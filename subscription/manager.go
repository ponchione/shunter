package subscription

import (
	"context"
	"errors"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// ErrQueryIDAlreadyLive is returned by RegisterSet when the given
// (ConnID, QueryID) pair already names a live set. Reference behavior:
// add_subscription_multi try_insert at
// reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1050.
var ErrQueryIDAlreadyLive = errors.New("subscription: query id already live on connection")

// SubscriptionSetRegisterRequest is the set-based register request.
// Predicates may have length >= 1; length 1 is the Single path.
type SubscriptionSetRegisterRequest struct {
	Context                 context.Context
	ConnID                  types.ConnectionID
	QueryID                 uint32
	Predicates              []Predicate
	PredicateHashIdentities []*types.Identity
	ClientIdentity          *types.Identity
	RequestID               uint32
	// SQLText is the original subscribe-query SQL string used for the
	// Single admission path. RegisterSet persists it on each newly
	// created queryState so UnregisterSet can surface it when wrapping a
	// final-eval failure with `ErrFinalQuery`. Empty on Multi paths and
	// callers that do not originate from a single SQL string; the Multi
	// UnsubscribeSingle WithSql wrap does not apply there
	// (`module_subscription_actor.rs:836` uses raw `return_on_err!`).
	SQLText string
}

// SubscriptionSetRegisterResult carries the merged initial snapshot.
// Update entries have Inserts populated and Deletes empty; one entry
// per (allocated internal SubscriptionID, table) pair, carrying the
// client QueryID for protocol projection.
type SubscriptionSetRegisterResult struct {
	QueryID                          uint32
	Update                           []SubscriptionUpdate
	TotalHostExecutionDurationMicros uint64
}

// SubscriptionSetUnregisterResult carries the final-delta rows that
// were still live at unsubscribe time. Update entries have Deletes
// populated and Inserts empty.
//
// SQLText is populated only when UnregisterSet returns an `ErrFinalQuery`
// wrap — it carries the stored queryState.sqlText of the first query
// whose final-delta evaluation failed, so the protocol-side adapter can
// apply the reference `DBError::WithSql` suffix on the UnsubscribeSingle
// path (`module_subscription_actor.rs:756`). On success or non-eval
// errors the field remains empty.
type SubscriptionSetUnregisterResult struct {
	QueryID                          uint32
	Update                           []SubscriptionUpdate
	TotalHostExecutionDurationMicros uint64
	SQLText                          string
}

// SubscriptionUpdate is the per-subscription component of a transaction
// delta. SubscriptionID is manager-internal; QueryID is the client-chosen
// subscription-set identifier projected onto the wire (SPEC-004 §10.2).
type SubscriptionUpdate struct {
	SubscriptionID types.SubscriptionID
	QueryID        uint32
	TableID        TableID
	TableName      string
	Inserts        []types.ProductValue
	Deletes        []types.ProductValue
}

// TransactionUpdate groups all subscription updates produced for one
// connection by one committed transaction (SPEC-004 §10.2).
type TransactionUpdate struct {
	TxID    types.TxID
	Updates []SubscriptionUpdate
}

// CommitFanout is the complete per-connection delta output from one
// EvalAndBroadcast call. Keyed by ConnectionID.
type CommitFanout map[types.ConnectionID][]SubscriptionUpdate

// SubscriptionManager is the contract consumed by the executor
// (SPEC-004 §10.1). All methods run on the executor goroutine.
type SubscriptionManager interface {
	RegisterSet(req SubscriptionSetRegisterRequest, view store.CommittedReadView) (SubscriptionSetRegisterResult, error)
	UnregisterSet(connID types.ConnectionID, queryID uint32, view store.CommittedReadView) (SubscriptionSetUnregisterResult, error)
	DisconnectClient(connID types.ConnectionID) error
	EvalAndBroadcast(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView, meta PostCommitMeta)
	DroppedClients() <-chan types.ConnectionID
}

// Manager is the default SubscriptionManager implementation.
// It is single-goroutine safe (the executor drives it).
type Manager struct {
	schema    SchemaLookup
	resolver  IndexResolver
	registry  *queryRegistry
	indexes   *PruningIndexes
	inbox     chan<- FanOutMessage
	dropped   chan types.ConnectionID
	querySets map[types.ConnectionID]map[uint32][]types.SubscriptionID
	nextSubID types.SubscriptionID

	// InitialRowLimit caps the initial-query row count returned to the
	// client. Zero means unlimited.
	InitialRowLimit int
}

// ManagerOption configures optional fields on the Manager.
type ManagerOption func(*Manager)

// WithInitialRowLimit sets the per-registration initial row cap.
func WithInitialRowLimit(n int) ManagerOption {
	return func(m *Manager) { m.InitialRowLimit = n }
}

// WithFanOutInbox wires the inbox channel used by EvalAndBroadcast to hand
// off computed fanout payloads. Nil inbox (the default) means evaluation
// still runs but no message is dispatched — useful for tests.
func WithFanOutInbox(inbox chan<- FanOutMessage) ManagerOption {
	return func(m *Manager) { m.inbox = inbox }
}

// NewManager constructs a Manager.
func NewManager(schema SchemaLookup, resolver IndexResolver, opts ...ManagerOption) *Manager {
	m := &Manager{
		schema:    schema,
		resolver:  resolver,
		registry:  newQueryRegistry(),
		indexes:   NewPruningIndexes(),
		dropped:   make(chan types.ConnectionID, 64),
		querySets: make(map[types.ConnectionID]map[uint32][]types.SubscriptionID),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// DroppedClients returns the channel the fan-out worker uses to signal
// disconnected ConnectionIDs. The executor drains it after each commit.
func (m *Manager) DroppedClients() <-chan types.ConnectionID { return m.dropped }

// DroppedChanSend returns the write end of the dropped-client channel.
// Used to wire the FanOutWorker to the same channel the Manager's
// eval-error path writes to, so the executor drains one channel.
func (m *Manager) DroppedChanSend() chan<- types.ConnectionID { return m.dropped }

// signalDropped is used by the fan-out worker (or equivalents in tests) to
// mark a connection as dropped. Non-blocking: if the channel is full the
// drop is discarded — the executor is responsible for draining frequently.
func (m *Manager) signalDropped(id types.ConnectionID) {
	select {
	case m.dropped <- id:
	default:
	}
}

// verify Manager satisfies SubscriptionManager at compile time.
var _ SubscriptionManager = (*Manager)(nil)

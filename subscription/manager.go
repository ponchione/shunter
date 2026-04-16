package subscription

import (
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// SubscriptionRegisterRequest carries the validated subscription parameters
// from the protocol layer to the executor and then to the subscription
// manager (SPEC-004 §4.1).
type SubscriptionRegisterRequest struct {
	ConnID         types.ConnectionID
	SubscriptionID types.SubscriptionID
	Predicate      Predicate        // validated and compiled by the protocol layer
	ClientIdentity *types.Identity  // nil for non-parameterized subscriptions
	RequestID      uint32           // echoed in SubscribeApplied
}

// SubscriptionRegisterResult is returned by Register after the initial query
// executes and the subscription is fully registered.
type SubscriptionRegisterResult struct {
	SubscriptionID types.SubscriptionID
	InitialRows    []types.ProductValue // all rows matching the predicate at registration time
}

// SubscriptionUpdate is the per-subscription component of a transaction
// delta. One per subscription affected by a commit (SPEC-004 §10.2).
type SubscriptionUpdate struct {
	SubscriptionID types.SubscriptionID
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
	Register(req SubscriptionRegisterRequest, view store.CommittedReadView) (SubscriptionRegisterResult, error)
	Unregister(connID types.ConnectionID, subscriptionID types.SubscriptionID) error
	DisconnectClient(connID types.ConnectionID) error
	EvalAndBroadcast(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView)
	DroppedClients() <-chan types.ConnectionID
}

// Manager is the default SubscriptionManager implementation.
// It is single-goroutine safe (the executor drives it).
type Manager struct {
	schema   SchemaLookup
	resolver IndexResolver
	registry *queryRegistry
	indexes  *PruningIndexes
	inbox    chan<- FanOutMessage
	dropped  chan types.ConnectionID

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
		schema:   schema,
		resolver: resolver,
		registry: newQueryRegistry(),
		indexes:  NewPruningIndexes(),
		dropped:  make(chan types.ConnectionID, 64),
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

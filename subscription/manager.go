package subscription

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// ErrQueryIDAlreadyLive is returned when (ConnID, QueryID) already names a set.
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
	// SQLText is the original Single-subscribe SQL used for final-eval errors.
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

// SubscriptionSetUnregisterResult carries final-delta rows still live at
// unsubscribe time. SQLText is populated only for final-eval failures.
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
	schema          SchemaLookup
	resolver        IndexResolver
	registry        *queryRegistry
	indexes         *PruningIndexes
	inbox           chan<- FanOutMessage
	dropped         chan types.ConnectionID
	droppedMu       sync.Mutex
	droppedPending  map[types.ConnectionID]struct{}
	droppedOrder    []types.ConnectionID
	activeColumns   map[TableID]map[ColID]int
	querySets       map[types.ConnectionID]map[uint32][]types.SubscriptionID
	nextSubID       types.SubscriptionID
	activeSets      atomic.Int64
	droppedTotal    atomic.Uint64
	observer        Observer
	fanoutMu        sync.Mutex
	fanoutClosed    bool
	fanoutClosedCh  chan struct{}
	fanoutCloseOnce sync.Once

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

// WithObserver wires runtime-scoped subscription observations.
func WithObserver(observer Observer) ManagerOption {
	return func(m *Manager) { m.observer = observer }
}

// NewManager constructs a Manager.
func NewManager(schema SchemaLookup, resolver IndexResolver, opts ...ManagerOption) *Manager {
	m := &Manager{
		schema:         schema,
		resolver:       resolver,
		registry:       newQueryRegistry(),
		indexes:        NewPruningIndexes(),
		dropped:        make(chan types.ConnectionID, 64),
		droppedPending: make(map[types.ConnectionID]struct{}),
		activeColumns:  make(map[TableID]map[ColID]int),
		querySets:      make(map[types.ConnectionID]map[uint32][]types.SubscriptionID),
		fanoutClosedCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// DroppedClients returns the channel the fan-out worker uses to signal
// disconnected ConnectionIDs. The executor drains it after each commit.
func (m *Manager) DroppedClients() <-chan types.ConnectionID { return m.dropped }

// CloseFanOut unblocks post-commit fan-out enqueue attempts during shutdown.
// It is idempotent; after it fires, EvalAndBroadcast skips handing new
// FanOutMessages to the worker while still completing reducer response paths.
func (m *Manager) CloseFanOut() {
	if m == nil {
		return
	}
	m.fanoutCloseOnce.Do(func() {
		m.fanoutMu.Lock()
		m.fanoutClosed = true
		close(m.fanoutClosedCh)
		m.fanoutMu.Unlock()
	})
}

// DroppedChanSend returns the write end of the dropped-client channel.
// Used to wire the FanOutWorker to the same channel the Manager's
// eval-error path writes to, so the executor drains one channel.
func (m *Manager) DroppedChanSend() chan<- types.ConnectionID { return m.dropped }

// ActiveSubscriptionSets returns the active client-visible subscription sets.
func (m *Manager) ActiveSubscriptionSets() int {
	if m == nil {
		return 0
	}
	n := m.activeSets.Load()
	if n < 0 {
		return 0
	}
	return int(n)
}

// DroppedClientCount returns the cumulative number of dropped clients signaled.
func (m *Manager) DroppedClientCount() uint64 {
	if m == nil {
		return 0
	}
	return m.droppedTotal.Load()
}

// RecordDroppedClient records one successfully signaled dropped client.
func (m *Manager) RecordDroppedClient() {
	if m != nil {
		m.droppedTotal.Add(1)
	}
}

// SignalDroppedClient records a dropped client for the next executor cleanup
// pass. Repeated signals for the same connection coalesce until drained.
func (m *Manager) SignalDroppedClient(id types.ConnectionID) {
	if m == nil {
		return
	}
	m.queueDroppedClient(id)
}

// DrainDroppedClients returns every dropped client signal retained since the
// previous drain, including legacy channel signals. IDs are coalesced so
// cleanup runs once per connection per drain window.
func (m *Manager) DrainDroppedClients() []types.ConnectionID {
	if m == nil {
		return nil
	}
	m.droppedMu.Lock()
	out := append([]types.ConnectionID(nil), m.droppedOrder...)
	clear(m.droppedPending)
	m.droppedOrder = nil
	m.droppedMu.Unlock()

	seen := make(map[types.ConnectionID]struct{}, len(out))
	for _, id := range out {
		seen[id] = struct{}{}
	}
	for {
		select {
		case id, ok := <-m.dropped:
			if !ok {
				return out
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		default:
			return out
		}
	}
}

func (m *Manager) queueDroppedClient(id types.ConnectionID) bool {
	m.droppedMu.Lock()
	defer m.droppedMu.Unlock()
	if _, exists := m.droppedPending[id]; exists {
		return false
	}
	m.droppedPending[id] = struct{}{}
	m.droppedOrder = append(m.droppedOrder, id)
	m.RecordDroppedClient()
	return true
}

// signalDropped is used by eval-error paths to mark a connection as dropped.
// It retains the signal losslessly and mirrors it to the legacy channel when
// possible so older tests and channel-based managers keep their behavior.
func (m *Manager) signalDropped(id types.ConnectionID) {
	if m == nil {
		return
	}
	m.queueDroppedClient(id)
	select {
	case m.dropped <- id:
		if m.observer != nil {
			m.observer.LogSubscriptionClientDropped("fanout_failed", &id)
		}
	default:
		if m.observer != nil {
			m.observer.LogSubscriptionClientDropped("buffer_full", &id)
		}
	}
}

// verify Manager satisfies SubscriptionManager at compile time.
var _ SubscriptionManager = (*Manager)(nil)

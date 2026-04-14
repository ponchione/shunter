package protocol

import (
	"errors"
	"fmt"
	"sync"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/types"
)

// SubscriptionState is the per-connection subscription-id state
// machine from SPEC-005 §9.1.
type SubscriptionState uint8

const (
	// SubPending: Subscribe accepted, initial evaluation not yet
	// complete.
	SubPending SubscriptionState = iota
	// SubActive: SubscribeApplied has been sent; subsequent
	// TransactionUpdate messages will reference this id.
	SubActive
)

// SubscriptionTracker enforces per-connection uniqueness of
// subscription_ids and their state machine. Reserved during
// Subscribe handling; activated when SubscribeApplied is delivered;
// removed on Unsubscribe / SubscriptionError / disconnect.
type SubscriptionTracker struct {
	mu   sync.Mutex
	subs map[uint32]SubscriptionState
}

// NewSubscriptionTracker returns an empty tracker.
func NewSubscriptionTracker() *SubscriptionTracker {
	return &SubscriptionTracker{subs: make(map[uint32]SubscriptionState)}
}

// ErrDuplicateSubscriptionID is returned when Reserve sees a
// subscription_id that is already pending or active on this
// connection (SPEC-005 §9.1 rule: ids cannot collide within an
// active connection).
var ErrDuplicateSubscriptionID = errors.New("protocol: duplicate subscription_id")

// ErrSubscriptionNotFound is returned when Remove or Activate is
// called with a subscription_id that is not tracked.
var ErrSubscriptionNotFound = errors.New("protocol: subscription_id not found")

// Reserve marks id as pending. Returns ErrDuplicateSubscriptionID if
// the id is already pending or active on this connection.
func (t *SubscriptionTracker) Reserve(id uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.subs[id]; ok {
		return fmt.Errorf("%w: id=%d", ErrDuplicateSubscriptionID, id)
	}
	t.subs[id] = SubPending
	return nil
}

// Activate transitions id from SubPending to SubActive. A no-op if
// already active; not an error.
func (t *SubscriptionTracker) Activate(id uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.subs[id]; ok {
		t.subs[id] = SubActive
	}
}

// Remove clears id from the tracker. Returns ErrSubscriptionNotFound
// if the id was not tracked.
func (t *SubscriptionTracker) Remove(id uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.subs[id]; !ok {
		return fmt.Errorf("%w: id=%d", ErrSubscriptionNotFound, id)
	}
	delete(t.subs, id)
	return nil
}

// IsActiveOrPending reports whether id is currently tracked.
func (t *SubscriptionTracker) IsActiveOrPending(id uint32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.subs[id]
	return ok
}

// RemoveAll clears the tracker and returns the list of ids that were
// tracked. Used on disconnect to hand off the full set to the
// executor's DisconnectClient cleanup path.
func (t *SubscriptionTracker) RemoveAll() []uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]uint32, 0, len(t.subs))
	for id := range t.subs {
		out = append(out, id)
	}
	t.subs = make(map[uint32]SubscriptionState)
	return out
}

// state is a test accessor for the raw state map. Kept internal to
// the package so callers go through the state-machine methods above.
func (t *SubscriptionTracker) state(id uint32) (SubscriptionState, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.subs[id]
	return st, ok
}

// Conn is per-connection server-side state for one WebSocket client
// (SPEC-005 §5.1). Subscription tracker, outbound queue, and
// transport references all live here; the read loop, write loop, and
// keep-alive goroutine share ownership.
type Conn struct {
	ID          types.ConnectionID
	Identity    types.Identity
	Token       string // validated or minted JWT for this connection
	Compression bool   // true when gzip was negotiated at upgrade

	Subscriptions *SubscriptionTracker
	// OutboundCh is the bounded per-connection outbound queue. The
	// backpressure design (SPEC-005 §10.1, Epic 6) uses the
	// fullness of this channel to decide between enqueue and close.
	OutboundCh chan []byte

	ws   *websocket.Conn
	opts *ProtocolOptions

	closeOnce sync.Once
	closed    chan struct{}
}

// NewConn constructs a per-connection state with its outbound queue
// sized from opts.OutgoingBufferMessages. The caller still owns
// lifecycle: adding to a ConnManager, spinning up read/write loops,
// calling Close.
func NewConn(
	id types.ConnectionID,
	identity types.Identity,
	token string,
	compression bool,
	ws *websocket.Conn,
	opts *ProtocolOptions,
) *Conn {
	return &Conn{
		ID:            id,
		Identity:      identity,
		Token:         token,
		Compression:   compression,
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, opts.OutgoingBufferMessages),
		ws:            ws,
		opts:          opts,
		closed:        make(chan struct{}),
	}
}

// ConnManager tracks all currently active connections by
// ConnectionID. Used by cross-connection operations such as the
// subscription fan-out delivery worker (Phase 8).
type ConnManager struct {
	mu    sync.RWMutex
	conns map[types.ConnectionID]*Conn
}

// NewConnManager returns an empty ConnManager.
func NewConnManager() *ConnManager {
	return &ConnManager{conns: make(map[types.ConnectionID]*Conn)}
}

// Add registers conn in the manager. Last-write-wins for duplicate
// IDs; upstream layers should not create colliding ConnectionIDs.
func (m *ConnManager) Add(conn *Conn) {
	m.mu.Lock()
	m.conns[conn.ID] = conn
	m.mu.Unlock()
}

// Remove drops the entry for id. Safe to call on a missing id.
func (m *ConnManager) Remove(id types.ConnectionID) {
	m.mu.Lock()
	delete(m.conns, id)
	m.mu.Unlock()
}

// Get returns the connection with the given id, or nil if absent.
func (m *ConnManager) Get(id types.ConnectionID) *Conn {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conns[id]
}

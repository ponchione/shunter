package protocol

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/types"
)

// Conn is per-connection server-side state for one WebSocket client
// (SPEC-005 §5.1). Outbound queue, keep-alive bookkeeping, and
// transport references all live here; the read loop, write loop, and
// keep-alive goroutine share ownership.
//
// Phase 2 Slice 2 admission-model slice (TD-140): per-connection
// subscription-id admission bookkeeping has been retired. The
// subscription.Manager's querySets map is the single source of truth
// for active query IDs, and SPEC-005 §9.4 in-flight ordering is
// preserved by the synchronous Reply closure invoked inside the
// executor main-loop goroutine plus per-connection OutboundCh FIFO.
// See docs/adr/2026-04-19-subscription-admission-model.md.
type Conn struct {
	ID          types.ConnectionID
	Identity    types.Identity
	Token       string // validated or minted JWT for this connection
	Compression bool   // true when gzip was negotiated at upgrade

	// OutboundCh is the bounded per-connection outbound queue. The
	// backpressure design (SPEC-005 §10.1, Epic 6) uses the
	// fullness of this channel to decide between enqueue and close.
	OutboundCh chan []byte

	// inflightSem limits concurrent in-flight inbound messages.
	// Capacity is IncomingQueueMessages. The dispatch loop acquires
	// before handler dispatch and the handler releases on completion.
	inflightSem chan struct{}

	ws   *websocket.Conn
	opts *ProtocolOptions

	readCtx    context.Context
	cancelRead context.CancelFunc

	closeOnce sync.Once
	closed    chan struct{}

	// lastActivity is the unix-nanos timestamp of the most recent
	// inbound signal observed on this connection: a Pong reply to
	// the keep-alive Ping (Story 3.5) or any application-level frame
	// received by the read loop (Epic 4). The keep-alive goroutine
	// samples this to decide if IdleTimeout has expired.
	lastActivity atomic.Int64
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
	readCtx, cancelRead := context.WithCancel(context.Background())
	c := &Conn{
		ID:          id,
		Identity:    identity,
		Token:       token,
		Compression: compression,
		OutboundCh:  make(chan []byte, opts.OutgoingBufferMessages),
		inflightSem: make(chan struct{}, opts.IncomingQueueMessages),
		ws:          ws,
		opts:        opts,
		readCtx:     readCtx,
		cancelRead:  cancelRead,
		closed:      make(chan struct{}),
	}
	c.MarkActivity()
	return c
}

// MarkActivity records that an inbound signal was observed on this
// connection. The Story 3.5 keep-alive loop calls it on every
// successful Ping-and-Pong round-trip; the Epic 4 read loop will call
// it on every inbound application frame. SPEC-005 §5.4: the idle
// timer resets on any received data, not only Pongs.
func (c *Conn) MarkActivity() {
	c.lastActivity.Store(time.Now().UnixNano())
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

// CloseAll sends a Close frame to every connected client and runs
// the disconnect sequence for each. Used for graceful server shutdown
// (SPEC-005 §11.1, close code 1000). Connections are closed
// concurrently with a bounded wait for all teardowns to complete.
//
// OI-004 sub-hazard pin
// (docs/hardening-oi-004-closeall-disconnect-context.md): the caller-
// supplied ctx is forwarded into each Conn.Disconnect, which threads it
// into inbox.DisconnectClientSubscriptions and inbox.OnDisconnect (steps
// 1-2 of the SPEC-005 §5.3 teardown). Every caller-supplied ctx is
// additionally wrapped per-conn by context.WithTimeout(ctx,
// DisconnectTimeout) so a single hung inbox call cannot pin a *Conn
// past the shutdown window, matching the bounded-ctx contract already
// enforced at the supervisor and sender overflow sites. The outer ctx
// is still honored — cancellation propagates through the per-conn
// derived ctx immediately — but a Background-rooted caller can no
// longer stall shutdown indefinitely. Pin tests:
// TestCloseAllBoundsDisconnectOnInboxHang,
// TestCloseAllDeliversOnInboxOK.
func (m *ConnManager) CloseAll(ctx context.Context, inbox ExecutorInbox) {
	m.mu.RLock()
	conns := make([]*Conn, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *Conn) {
			defer wg.Done()
			disconnectCtx, cancel := context.WithTimeout(ctx, c.opts.DisconnectTimeout)
			defer cancel()
			c.Disconnect(disconnectCtx, CloseNormal, "server shutdown", inbox, m)
		}(c)
	}
	wg.Wait()
}

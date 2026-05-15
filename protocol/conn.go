package protocol

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/types"
)

// Conn is server-side state for one WebSocket client.
// Subscription ownership lives in the subscription manager; Conn owns transport,
// keep-alive, and outbound queue state.
type Conn struct {
	ID              types.ConnectionID
	Identity        types.Identity
	Principal       types.AuthPrincipal
	Token           string // validated or minted JWT for this connection
	ProtocolVersion ProtocolVersion
	Compression     bool // true when gzip was negotiated at upgrade
	// Permissions are copied from authenticated upgrade claims and forwarded
	// to the executor on external reducer calls.
	Permissions         []string
	AllowAllPermissions bool
	Observer            Observer

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

	disconnectStarted atomic.Bool
	disconnectInbox   ExecutorInbox
	disconnectMgr     *ConnManager

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
	normalized := DefaultProtocolOptions()
	if opts != nil {
		var err error
		normalized, err = NormalizeProtocolOptions(*opts)
		if err != nil {
			panic("protocol: invalid options: " + err.Error())
		}
	}
	readCtx, cancelRead := context.WithCancel(context.Background())
	c := &Conn{
		ID:              id,
		Identity:        identity,
		Token:           token,
		ProtocolVersion: CurrentProtocolVersion,
		Compression:     compression,
		OutboundCh:      make(chan []byte, normalized.OutgoingBufferMessages),
		inflightSem:     make(chan struct{}, normalized.IncomingQueueMessages),
		ws:              ws,
		opts:            &normalized,
		readCtx:         readCtx,
		cancelRead:      cancelRead,
		closed:          make(chan struct{}),
	}
	c.MarkActivity()
	return c
}

func (c *Conn) bindDisconnect(inbox ExecutorInbox, mgr *ConnManager) {
	c.disconnectInbox = inbox
	c.disconnectMgr = mgr
}

func (c *Conn) startOutboundOverflowDisconnect(inbox ExecutorInbox, mgr *ConnManager) bool {
	if c == nil {
		return false
	}
	if inbox == nil {
		inbox = c.disconnectInbox
	}
	if mgr == nil {
		mgr = c.disconnectMgr
	}
	if inbox == nil || mgr == nil {
		return false
	}
	select {
	case <-c.closed:
		return true
	default:
	}
	if !c.disconnectStarted.CompareAndSwap(false, true) {
		return true
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), c.disconnectTimeout())
		defer cancel()
		c.Disconnect(ctx, websocket.StatusPolicyViolation, CloseReasonSendBufferFull, inbox, mgr)
	}()
	return true
}

func (c *Conn) disconnectTimeout() time.Duration {
	if c != nil && c.opts != nil && c.opts.DisconnectTimeout > 0 {
		return c.opts.DisconnectTimeout
	}
	return DefaultProtocolOptions().DisconnectTimeout
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
// subscription fan-out delivery worker (fan-out integration).
type ConnManager struct {
	mu       sync.RWMutex
	conns    map[types.ConnectionID]*Conn
	reserved map[types.ConnectionID]*Conn
	accepted atomic.Uint64
	rejected atomic.Uint64
}

// NewConnManager returns an empty ConnManager.
func NewConnManager() *ConnManager {
	return &ConnManager{
		conns:    make(map[types.ConnectionID]*Conn),
		reserved: make(map[types.ConnectionID]*Conn),
	}
}

func connIsClosed(conn *Conn) bool {
	if conn == nil || conn.closed == nil {
		return false
	}
	select {
	case <-conn.closed:
		return true
	default:
		return false
	}
}

// reserve claims conn.ID while the connection is passing admission. A reserved
// ID is not visible to delivery lookups, but it blocks duplicate live upgrades
// from running OnConnect side effects for the same ConnectionID.
func (m *ConnManager) reserve(conn *Conn) error {
	if m == nil || conn == nil {
		return nil
	}
	if conn.ID.IsZero() {
		return ErrZeroConnectionID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.conns[conn.ID]; existing != nil && existing != conn && !connIsClosed(existing) {
		return ErrConnectionIDInUse
	}
	if reserved := m.reserved[conn.ID]; reserved != nil && reserved != conn {
		return ErrConnectionIDInUse
	}
	m.reserved[conn.ID] = conn
	return nil
}

func (m *ConnManager) releaseReservation(conn *Conn) {
	if m == nil || conn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reserved[conn.ID] == conn {
		delete(m.reserved, conn.ID)
	}
}

// Add registers conn in the manager. A duplicate live or admitting
// ConnectionID is rejected so fan-out, subscription, and disconnect state remain
// bound to a single connection owner.
func (m *ConnManager) Add(conn *Conn) error {
	if conn == nil {
		return nil
	}
	if conn.ID.IsZero() {
		return ErrZeroConnectionID
	}
	m.mu.Lock()
	if existing := m.conns[conn.ID]; existing != nil && existing != conn && !connIsClosed(existing) {
		m.mu.Unlock()
		return ErrConnectionIDInUse
	}
	if reserved := m.reserved[conn.ID]; reserved != nil && reserved != conn {
		m.mu.Unlock()
		return ErrConnectionIDInUse
	}
	delete(m.reserved, conn.ID)
	m.conns[conn.ID] = conn
	m.mu.Unlock()
	m.accepted.Add(1)
	return nil
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

// ActiveCount returns the number of currently tracked connections.
func (m *ConnManager) ActiveCount() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

// AcceptedCount returns the cumulative accepted connection count.
func (m *ConnManager) AcceptedCount() uint64 {
	if m == nil {
		return 0
	}
	return m.accepted.Load()
}

// RejectedCount returns the cumulative rejected connection count.
func (m *ConnManager) RejectedCount() uint64 {
	if m == nil {
		return 0
	}
	return m.rejected.Load()
}

// RecordRejected records one rejected connection attempt.
func (m *ConnManager) RecordRejected() {
	if m != nil {
		m.rejected.Add(1)
	}
}

// CloseAll disconnects every connected client concurrently.
// Each teardown gets a bounded context so one hung inbox call cannot stall
// server shutdown indefinitely.
func (m *ConnManager) CloseAll(ctx context.Context, inbox ExecutorInbox) {
	if ctx == nil {
		ctx = context.Background()
	}
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
			disconnectCtx, cancel := context.WithTimeout(ctx, c.disconnectTimeout())
			defer cancel()
			c.Disconnect(disconnectCtx, CloseNormal, CloseReasonServerShutdown, inbox, m)
		}(c)
	}
	wg.Wait()
}

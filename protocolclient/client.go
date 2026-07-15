package protocolclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/protocol"
)

var (
	ErrURLRequired         = errors.New("protocol client URL is required")
	ErrTokenRequired       = errors.New("protocol client token is required")
	ErrTimeout             = errors.New("protocol client timeout")
	ErrUnexpectedMessage   = errors.New("protocol client unexpected message")
	ErrNonBinaryMessage    = errors.New("protocol client non-binary message")
	ErrResponseMismatch    = errors.New("protocol client response mismatch")
	ErrReducerFailed       = errors.New("protocol client reducer failed")
	ErrDeclaredQueryFailed = errors.New("protocol client declared query failed")
	ErrSQLQueryFailed      = errors.New("protocol client SQL query failed")
	ErrProcedureFailed     = errors.New("protocol client procedure failed")
	ErrProtocolVersion     = errors.New("protocol client unsupported protocol version")
	ErrPendingMessageLimit = errors.New("protocol client pending message limit exceeded")
)

const (
	DefaultMaxPendingMessages = 1024
	DefaultMaxPendingBytes    = 16 << 20
)

// Options configures a protocol WebSocket client connection.
type Options struct {
	URL            string
	Token          string
	AllowAnonymous bool
	// MaxPendingMessages and MaxPendingBytes bound decoded server messages
	// waiting for Read. Non-positive values select the safe defaults.
	MaxPendingMessages int
	MaxPendingBytes    int64
}

// Client is a small Shunter protocol client for admin and maintenance tooling.
// Typed request methods serialize their wire operations; asynchronous
// subscription messages encountered while awaiting a direct response are
// preserved for Read. Read may block concurrently with a typed request because
// one internal reader routes direct and asynchronous responses separately.
type Client struct {
	conn               *websocket.Conn
	nextID             atomic.Uint32
	identity           protocol.IdentityToken
	subproto           string
	closeDone          atomic.Bool
	operationMu        sync.Mutex
	writeMu            sync.Mutex
	pendingMu          sync.Mutex
	pending            []queuedServerMessage
	pendingBytes       int64
	maxPendingMessages int
	maxPendingBytes    int64
	pendingNotify      chan struct{}
	readerErr          error
	responseCh         chan queuedServerMessage
}

type queuedServerMessage struct {
	tag  uint8
	msg  any
	size int64
	err  error
}

// Dial connects to a Shunter protocol endpoint and reads the initial identity frame.
func Dial(ctx context.Context, opts Options) (*Client, protocol.IdentityToken, error) {
	ctx = contextOrBackground(ctx)
	target := strings.TrimSpace(opts.URL)
	if target == "" {
		return nil, protocol.IdentityToken{}, ErrURLRequired
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" && !opts.AllowAnonymous {
		return nil, protocol.IdentityToken{}, ErrTokenRequired
	}

	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	conn, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{
		HTTPHeader:   header,
		Subprotocols: protocol.SupportedSubprotocols(),
	})
	if err != nil {
		return nil, protocol.IdentityToken{}, classifyContextError(ctx, fmt.Errorf("dial protocol %q: %w", target, err))
	}

	maxPendingMessages := opts.MaxPendingMessages
	if maxPendingMessages <= 0 {
		maxPendingMessages = DefaultMaxPendingMessages
	}
	maxPendingBytes := opts.MaxPendingBytes
	if maxPendingBytes <= 0 {
		maxPendingBytes = DefaultMaxPendingBytes
	}
	client := &Client{
		conn:               conn,
		subproto:           conn.Subprotocol(),
		maxPendingMessages: maxPendingMessages,
		maxPendingBytes:    maxPendingBytes,
		pendingNotify:      make(chan struct{}),
	}
	if _, ok := protocol.ProtocolVersionForSubprotocol(client.subproto); !ok {
		conn.CloseNow()
		return nil, protocol.IdentityToken{}, fmt.Errorf("%w: negotiated subprotocol %q", ErrProtocolVersion, client.subproto)
	}
	tag, msg, _, err := client.readServerMessage(ctx)
	if err != nil {
		conn.CloseNow()
		return nil, protocol.IdentityToken{}, classifyContextError(ctx, fmt.Errorf("read identity token: %w", err))
	}
	if tag != protocol.TagIdentityToken {
		conn.CloseNow()
		return nil, protocol.IdentityToken{}, fmt.Errorf("%w: first server tag = %d, want identity token", ErrUnexpectedMessage, tag)
	}
	identity, ok := msg.(protocol.IdentityToken)
	if !ok {
		conn.CloseNow()
		return nil, protocol.IdentityToken{}, fmt.Errorf("%w: first server message = %T, want protocol.IdentityToken", ErrUnexpectedMessage, msg)
	}
	client.identity = identity
	go client.readLoop()
	return client, identity, nil
}

// Subprotocol returns the negotiated WebSocket subprotocol.
func (c *Client) Subprotocol() string {
	if c == nil {
		return ""
	}
	return c.subproto
}

// IdentityToken returns the initial identity frame received during Dial.
func (c *Client) IdentityToken() protocol.IdentityToken {
	if c == nil {
		return protocol.IdentityToken{}
	}
	return c.identity
}

// NextRequestID returns a monotonically increasing non-zero request ID.
func (c *Client) NextRequestID() uint32 {
	if c == nil {
		return 0
	}
	for {
		current := c.nextID.Load()
		next := current + 1
		if next == 0 {
			next = 1
		}
		if c.nextID.CompareAndSwap(current, next) {
			return next
		}
	}
}

// Send encodes and writes one client protocol message.
func (c *Client) Send(ctx context.Context, msg any) error {
	if c == nil || c.conn == nil {
		return errors.New("protocol client is closed")
	}
	ctx = contextOrBackground(ctx)
	frame, err := protocol.EncodeClientMessage(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		return classifyContextError(ctx, err)
	}
	return nil
}

// Read reads and decodes one server protocol message.
func (c *Client) Read(ctx context.Context) (uint8, any, error) {
	if c == nil || c.conn == nil {
		return 0, nil, errors.New("protocol client is closed")
	}
	ctx = contextOrBackground(ctx)
	for {
		c.pendingMu.Lock()
		if len(c.pending) > 0 {
			next := c.popPendingLocked()
			c.pendingMu.Unlock()
			return next.tag, next.msg, nil
		}
		if c.readerErr != nil {
			err := c.readerErr
			c.pendingMu.Unlock()
			return 0, nil, classifyContextError(ctx, err)
		}
		if c.pendingNotify == nil {
			c.pendingNotify = make(chan struct{})
		}
		notify := c.pendingNotify
		c.pendingMu.Unlock()

		select {
		case <-notify:
		case <-ctx.Done():
			return 0, nil, classifyContextError(ctx, ctx.Err())
		}
	}
}

func (c *Client) readServerMessage(ctx context.Context) (uint8, any, int64, error) {
	if c == nil || c.conn == nil {
		return 0, nil, 0, errors.New("protocol client is closed")
	}
	ctx = contextOrBackground(ctx)
	typ, frame, err := c.conn.Read(ctx)
	if err != nil {
		return 0, nil, 0, classifyContextError(ctx, err)
	}
	if typ != websocket.MessageBinary {
		return 0, nil, int64(len(frame)), fmt.Errorf("%w: message type %d", ErrNonBinaryMessage, typ)
	}
	tag, msg, err := protocol.DecodeServerMessage(frame)
	if err != nil {
		return 0, nil, int64(len(frame)), fmt.Errorf("%w: %w", ErrUnexpectedMessage, err)
	}
	return tag, msg, int64(len(frame)), nil
}

func (c *Client) readLoop() {
	for {
		tag, msg, size, err := c.readServerMessage(context.Background())
		if err != nil {
			c.finishReader(err, false)
			return
		}
		if !c.routeServerMessage(queuedServerMessage{tag: tag, msg: msg, size: size}) {
			_ = c.conn.CloseNow()
			return
		}
	}
}

func (c *Client) routeServerMessage(next queuedServerMessage) bool {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	if !isAsynchronousServerMessage(next.tag) && c.responseCh != nil {
		select {
		case c.responseCh <- next:
			return true
		default:
			err := fmt.Errorf("%w: typed response queue is full", ErrPendingMessageLimit)
			c.failReaderLocked(err, true)
			return false
		}
	}
	if len(c.pending)+1 > c.maxPendingMessages {
		err := fmt.Errorf("%w: messages=%d limit=%d", ErrPendingMessageLimit, len(c.pending)+1, c.maxPendingMessages)
		c.failReaderLocked(err, true)
		return false
	}
	if c.pendingBytes+next.size > c.maxPendingBytes {
		err := fmt.Errorf("%w: bytes=%d limit=%d", ErrPendingMessageLimit, c.pendingBytes+next.size, c.maxPendingBytes)
		c.failReaderLocked(err, true)
		return false
	}
	c.pending = append(c.pending, next)
	c.pendingBytes += next.size
	c.signalPendingLocked()
	return true
}

func isAsynchronousServerMessage(tag uint8) bool {
	return tag == protocol.TagTransactionUpdateLight || tag == protocol.TagSubscriptionError
}

func (c *Client) beginSynchronousResponse() (chan queuedServerMessage, error) {
	if c == nil || c.conn == nil {
		return nil, errors.New("protocol client is closed")
	}
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if c.readerErr != nil {
		return nil, c.readerErr
	}
	if c.responseCh != nil {
		return nil, errors.New("protocol client synchronous operation already active")
	}
	responseCh := make(chan queuedServerMessage, 1)
	c.responseCh = responseCh
	return responseCh, nil
}

func (c *Client) endSynchronousResponse(responseCh chan queuedServerMessage) {
	c.pendingMu.Lock()
	if c.responseCh == responseCh {
		c.responseCh = nil
	}
	c.pendingMu.Unlock()
}

func (c *Client) readSynchronousResponse(ctx context.Context, responseCh <-chan queuedServerMessage) (uint8, any, error) {
	ctx = contextOrBackground(ctx)
	select {
	case response := <-responseCh:
		if response.err != nil {
			return 0, nil, classifyContextError(ctx, response.err)
		}
		return response.tag, response.msg, nil
	case <-ctx.Done():
		return 0, nil, classifyContextError(ctx, ctx.Err())
	}
}

func (c *Client) popPendingLocked() queuedServerMessage {
	next := c.pending[0]
	c.pending[0] = queuedServerMessage{}
	c.pendingBytes -= next.size
	if len(c.pending) == 1 {
		c.pending = nil
		c.pendingBytes = 0
	} else {
		c.pending = c.pending[1:]
	}
	return next
}

func (c *Client) finishReader(err error, discardPending bool) {
	c.pendingMu.Lock()
	c.failReaderLocked(err, discardPending)
	c.pendingMu.Unlock()
}

func (c *Client) failReaderLocked(err error, discardPending bool) {
	if c.readerErr != nil {
		return
	}
	if discardPending {
		for i := range c.pending {
			c.pending[i] = queuedServerMessage{}
		}
		c.pending = nil
		c.pendingBytes = 0
	}
	c.readerErr = err
	if c.responseCh != nil {
		select {
		case c.responseCh <- queuedServerMessage{err: err}:
		default:
		}
	}
	c.signalPendingLocked()
}

func (c *Client) signalPendingLocked() {
	if c.pendingNotify == nil {
		c.pendingNotify = make(chan struct{})
	}
	close(c.pendingNotify)
	c.pendingNotify = make(chan struct{})
}

// Close gracefully closes the WebSocket connection once.
func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return nil
	}
	ctx = contextOrBackground(ctx)
	if !c.closeDone.CompareAndSwap(false, true) {
		return nil
	}
	err := c.conn.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	if err != nil {
		return classifyContextError(ctx, err)
	}
	return nil
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func classifyContextError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	return err
}

package protocolclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
)

// Options configures a protocol WebSocket client connection.
type Options struct {
	URL            string
	Token          string
	AllowAnonymous bool
}

// Client is a small Shunter protocol client for admin and maintenance tooling.
type Client struct {
	conn      *websocket.Conn
	nextID    atomic.Uint32
	identity  protocol.IdentityToken
	subproto  string
	closeDone atomic.Bool
}

// Dial connects to a Shunter protocol endpoint and reads the initial identity frame.
func Dial(ctx context.Context, opts Options) (*Client, protocol.IdentityToken, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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

	client := &Client{conn: conn, subproto: conn.Subprotocol()}
	if _, ok := protocol.ProtocolVersionForSubprotocol(client.subproto); !ok {
		conn.CloseNow()
		return nil, protocol.IdentityToken{}, fmt.Errorf("%w: negotiated subprotocol %q", ErrProtocolVersion, client.subproto)
	}
	tag, msg, err := client.Read(ctx)
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
	return c.nextID.Add(1)
}

// Send encodes and writes one client protocol message.
func (c *Client) Send(ctx context.Context, msg any) error {
	if c == nil || c.conn == nil {
		return errors.New("protocol client is closed")
	}
	frame, err := protocol.EncodeClientMessage(msg)
	if err != nil {
		return err
	}
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
	typ, frame, err := c.conn.Read(ctx)
	if err != nil {
		return 0, nil, classifyContextError(ctx, err)
	}
	if typ != websocket.MessageBinary {
		return 0, nil, fmt.Errorf("%w: message type %d", ErrNonBinaryMessage, typ)
	}
	tag, msg, err := protocol.DecodeServerMessage(frame)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %w", ErrUnexpectedMessage, err)
	}
	return tag, msg, nil
}

// Close gracefully closes the WebSocket connection once.
func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return nil
	}
	if !c.closeDone.CompareAndSwap(false, true) {
		return nil
	}
	err := c.conn.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		return classifyContextError(ctx, err)
	}
	return nil
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

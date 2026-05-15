package protocol

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponchione/websocket"
)

// MessageHandlers holds the per-message-type handler functions wired by
// the host. A nil field means the message type is not supported on this
// connection -- the dispatch loop closes with 1002 if it encounters one.
type MessageHandlers struct {
	OnSubscribeSingle                     func(ctx context.Context, conn *Conn, msg *SubscribeSingleMsg)
	OnSubscribeMulti                      func(ctx context.Context, conn *Conn, msg *SubscribeMultiMsg)
	OnUnsubscribeSingle                   func(ctx context.Context, conn *Conn, msg *UnsubscribeSingleMsg)
	OnUnsubscribeMulti                    func(ctx context.Context, conn *Conn, msg *UnsubscribeMultiMsg)
	OnCallReducer                         func(ctx context.Context, conn *Conn, msg *CallReducerMsg)
	OnOneOffQuery                         func(ctx context.Context, conn *Conn, msg *OneOffQueryMsg)
	OnDeclaredQuery                       func(ctx context.Context, conn *Conn, msg *DeclaredQueryMsg)
	OnDeclaredQueryWithParameters         func(ctx context.Context, conn *Conn, msg *DeclaredQueryWithParametersMsg)
	OnSubscribeDeclaredView               func(ctx context.Context, conn *Conn, msg *SubscribeDeclaredViewMsg)
	OnSubscribeDeclaredViewWithParameters func(ctx context.Context, conn *Conn, msg *SubscribeDeclaredViewWithParametersMsg)
}

// sendError encodes a server message, wraps it in the connection's
// compression envelope, and pushes it to the outbound queue. On outbound
// overflow it uses the same lifecycle-bound disconnect policy as the other
// local response paths.
func sendError(conn *Conn, msg any) {
	if err := (connOnlySender{conn: conn}).Send(conn.ID, msg); err != nil {
		if errors.Is(err, ErrClientBufferFull) || errors.Is(err, ErrConnNotFound) {
			return
		}
		logProtocolError(conn.Observer, "unknown", "encode_failed", err)
	}
}

// closeProtocolError sends a WebSocket close frame with status 1002
// (protocol error). Runs in a goroutine because coder/websocket.Close
// blocks on the close handshake.
func closeProtocolError(conn *Conn, reason string) {
	logProtocolError(conn.Observer, "unknown", protocolErrorReason(reason), errorFromText(reason))
	go closeWithHandshake(conn.ws, CloseProtocol, reason, conn.opts.CloseHandshakeTimeout)
}

// runDispatchLoop reads frames, decodes messages, and dispatches handlers.
// Handler contexts also cancel when the Conn closes so blocked executor sends
// cannot leak past disconnect.
func (c *Conn) runDispatchLoop(ctx context.Context, handlers *MessageHandlers) {
	if handlers == nil {
		handlers = &MessageHandlers{}
	}

	readCtx := ctx
	if c.readCtx != nil {
		combinedCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-c.readCtx.Done():
				cancel()
			case <-combinedCtx.Done():
			}
		}()
		readCtx = combinedCtx
	}
	handlerCtx, handlerCancel := context.WithCancel(ctx)
	defer handlerCancel()
	go func() {
		select {
		case <-c.closed:
			handlerCancel()
		case <-handlerCtx.Done():
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		default:
		}

		typ, frame, err := c.ws.Read(readCtx)
		if err != nil {
			return
		}

		// Reject text frames per SPEC-005 S3.2.
		if typ == websocket.MessageText {
			recordProtocolMessage(c.Observer, "unknown", "malformed")
			closeProtocolError(c, CloseReasonTextFrameUnsupported)
			return
		}
		c.MarkActivity()

		var (
			msg       any
			decodeErr error
			kind      = "unknown"
		)
		if len(frame) > 0 {
			kind = protocolKindFromTag(frame[0])
		}
		_, msg, decodeErr = DecodeClientMessageForVersion(c.ProtocolVersion, frame)
		if decodeErr != nil {
			reason := decodeErr.Error()
			recordProtocolMessage(c.Observer, kind, "malformed")
			closeProtocolError(c, reason)
			return
		}
		kind = protocolKindFromMessage(msg)

		var run func()
		switch m := msg.(type) {
		case SubscribeSingleMsg:
			if h := handlers.OnSubscribeSingle; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case SubscribeMultiMsg:
			if h := handlers.OnSubscribeMulti; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case UnsubscribeSingleMsg:
			if h := handlers.OnUnsubscribeSingle; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case UnsubscribeMultiMsg:
			if h := handlers.OnUnsubscribeMulti; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case CallReducerMsg:
			if h := handlers.OnCallReducer; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case OneOffQueryMsg:
			if h := handlers.OnOneOffQuery; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case DeclaredQueryMsg:
			if h := handlers.OnDeclaredQuery; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case DeclaredQueryWithParametersMsg:
			if h := handlers.OnDeclaredQueryWithParameters; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case SubscribeDeclaredViewMsg:
			if h := handlers.OnSubscribeDeclaredView; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		case SubscribeDeclaredViewWithParametersMsg:
			if h := handlers.OnSubscribeDeclaredViewWithParameters; h != nil {
				run = func() { h(handlerCtx, c, &m) }
			}
		}
		if run == nil {
			recordProtocolMessage(c.Observer, kind, "internal_error")
			closeProtocolError(c, CloseReasonUnsupportedMessage)
			return
		}

		// Incoming backpressure (SPEC-005 §10.2, Story 6.2):
		// non-blocking acquire on the inflight semaphore. If full,
		// the client is flooding faster than we can process —
		// close with 1008.
		select {
		case c.inflightSem <- struct{}{}:
		default:
			logProtocolBackpressure(c.Observer, "inbound", "buffer_full")
			go closeWithHandshake(c.ws, ClosePolicy, CloseReasonTooManyRequests, c.opts.CloseHandshakeTimeout)
			return
		}

		go c.runMessageHandler(kind, run)
	}
}

func (c *Conn) runMessageHandler(kind string, run func()) {
	defer func() { <-c.inflightSem }()
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("protocol handler panic: %v", recovered)
			recordProtocolMessage(c.Observer, kind, "internal_error")
			logProtocolError(c.Observer, kind, "handler_panic", err)
			if c.ws != nil {
				go closeWithHandshake(c.ws, CloseInternal, "internal error", c.closeHandshakeTimeout())
			}
		}
	}()
	run()
}

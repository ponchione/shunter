package protocol

import (
	"context"
	"errors"
	"log"

	"github.com/coder/websocket"
)

// MessageHandlers holds the per-message-type handler functions wired by
// the host. A nil field means the message type is not supported on this
// connection -- the dispatch loop closes with 1002 if it encounters one.
type MessageHandlers struct {
	OnSubscribeSingle   func(ctx context.Context, conn *Conn, msg *SubscribeSingleMsg)
	OnSubscribeMulti    func(ctx context.Context, conn *Conn, msg *SubscribeMultiMsg)
	OnUnsubscribeSingle func(ctx context.Context, conn *Conn, msg *UnsubscribeSingleMsg)
	OnUnsubscribeMulti  func(ctx context.Context, conn *Conn, msg *UnsubscribeMultiMsg)
	OnCallReducer       func(ctx context.Context, conn *Conn, msg *CallReducerMsg)
	OnOneOffQuery       func(ctx context.Context, conn *Conn, msg *OneOffQueryMsg)
}

// sendError encodes a server message, wraps it in the connection's
// compression envelope, and pushes it to the outbound queue. If encoding
// fails or the queue is full it logs and drops -- the caller is already
// in an error path and cannot retry.
func sendError(conn *Conn, msg any) {
	frame, err := EncodeServerMessage(msg)
	if err != nil {
		log.Printf("protocol: sendError encode failed: %v", err)
		return
	}
	wrapped := EncodeFrame(frame[0], frame[1:], conn.Compression, CompressionNone)
	select {
	case conn.OutboundCh <- wrapped:
	default:
		log.Printf("protocol: sendError dropped (outbound full) for conn %x", conn.ID[:])
	}
}

// closeProtocolError sends a WebSocket close frame with status 1002
// (protocol error). Runs in a goroutine because coder/websocket.Close
// blocks on the close handshake.
func closeProtocolError(conn *Conn, reason string) {
	log.Printf("protocol: closing conn %x with protocol error: %s", conn.ID[:], reason)
	go closeWithHandshake(conn.ws, CloseProtocol, reason, conn.opts.CloseHandshakeTimeout)
}

// runDispatchLoop replaces runReadPump (Story 3.5) with the full
// message-dispatching read loop (Epic 4, Story 4.1). Every successful
// read marks activity per SPEC-005 S5.4.
//
// Exit conditions: ctx cancelled, c.closed closed, or ws.Read error.
func (c *Conn) runDispatchLoop(ctx context.Context, handlers *MessageHandlers) {
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
			closeProtocolError(c, "text frames not supported")
			return
		}
		c.MarkActivity()

		var (
			msg       any
			decodeErr error
		)
		if c.Compression {
			var tag uint8
			var body []byte
			var unwrapErr error
			tag, body, unwrapErr = UnwrapCompressed(frame)
			if unwrapErr != nil {
				if errors.Is(unwrapErr, ErrBrotliUnsupported) {
					closeProtocolError(c, "brotli unsupported")
				} else {
					closeProtocolError(c, "malformed message")
				}
				return
			}
			msg, decodeErr = decodeClientMessageParts(tag, body)
		} else {
			_, msg, decodeErr = DecodeClientMessage(frame)
		}
		if decodeErr != nil {
			reason := decodeErr.Error()
			closeProtocolError(c, reason)
			return
		}

		var run func()
		switch m := msg.(type) {
		case SubscribeSingleMsg:
			if handlers.OnSubscribeSingle == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			run = func() { handlers.OnSubscribeSingle(ctx, c, &m) }
		case SubscribeMultiMsg:
			if handlers.OnSubscribeMulti == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			run = func() { handlers.OnSubscribeMulti(ctx, c, &m) }
		case UnsubscribeSingleMsg:
			if handlers.OnUnsubscribeSingle == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			run = func() { handlers.OnUnsubscribeSingle(ctx, c, &m) }
		case UnsubscribeMultiMsg:
			if handlers.OnUnsubscribeMulti == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			run = func() { handlers.OnUnsubscribeMulti(ctx, c, &m) }
		case CallReducerMsg:
			if handlers.OnCallReducer == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			run = func() { handlers.OnCallReducer(ctx, c, &m) }
		case OneOffQueryMsg:
			if handlers.OnOneOffQuery == nil {
				closeProtocolError(c, "unsupported message type")
				return
			}
			run = func() { handlers.OnOneOffQuery(ctx, c, &m) }
		}

		// Incoming backpressure (SPEC-005 §10.2, Story 6.2):
		// non-blocking acquire on the inflight semaphore. If full,
		// the client is flooding faster than we can process —
		// close with 1008.
		select {
		case c.inflightSem <- struct{}{}:
		default:
			go closeWithHandshake(c.ws, ClosePolicy, "too many requests", c.opts.CloseHandshakeTimeout)
			return
		}

		go func(run func()) {
			defer func() { <-c.inflightSem }()
			run()
		}(run)
	}
}

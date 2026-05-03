package protocol

import (
	"fmt"

	"github.com/ponchione/shunter/types"
)

// connOnlySender is a minimal ClientSender for accepted-command response
// watchers that already have the target *Conn in hand. It preserves the same
// non-blocking enqueue semantics as the protocol sender path without needing a
// ConnManager lookup.
type connOnlySender struct {
	conn *Conn
}

func (s connOnlySender) Send(connID types.ConnectionID, msg any) error {
	if s.conn == nil || connID != s.conn.ID {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}
	frame, err := EncodeServerMessage(msg)
	if err != nil {
		return fmt.Errorf("encode server message: %w", err)
	}
	wrapped := EncodeFrame(frame[0], frame[1:], s.conn.Compression, CompressionNone)

	select {
	case <-s.conn.closed:
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	default:
	}

	select {
	case <-s.conn.closed:
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	case s.conn.OutboundCh <- wrapped:
		return nil
	default:
		logProtocolBackpressure(s.conn.Observer, "outbound", "buffer_full")
		s.conn.startOutboundOverflowDisconnect(nil, nil)
		return fmt.Errorf("%w: %x", ErrClientBufferFull, connID[:])
	}
}

func (s connOnlySender) SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error {
	if update == nil {
		return nil
	}
	return s.Send(connID, *update)
}

func (s connOnlySender) SendTransactionUpdateLight(connID types.ConnectionID, update *TransactionUpdateLight) error {
	if update == nil {
		return nil
	}
	return s.Send(connID, *update)
}

// watchReducerResponse listens for the executor's heavy
// `TransactionUpdate` envelope and delivers it on the caller's outbound
// channel. outcome-model: the envelope is already fully populated by the
// executor / fan-out seam; this watcher only owns transport delivery.
//
// hardening (2026-04-20): the watcher goroutine is tied to the
// owning `Conn`'s lifecycle. Previously the body blocked unconditionally
// on `<-respCh`; if the executor accepted the CallReducer but never sent
// on (or closed) respCh — e.g. executor crash mid-commit, hung reducer
// on a shutting-down engine — the goroutine would leak forever, holding
// the `*Conn` alive past disconnect. The select now also watches
// `conn.closed`, which `Conn.Disconnect` closes as step 4 of the
// SPEC-005 §5.3 teardown, so the watcher exits promptly when the owning
// connection is torn down. A late send on respCh after `conn.closed`
// fires is benign: `respCh` is allocated with buffer 1 at the call site
// so a single post-close send completes without blocking, and the
// message is garbage-collected with the channel.
//
// Pinned by protocol/async_responses_test.go::
// TestWatchReducerResponseExitsOnConnClose.
func watchReducerResponse(conn *Conn, respCh <-chan TransactionUpdate) {
	go runReducerResponseWatcher(conn, respCh)
}

func runReducerResponseWatcher(conn *Conn, respCh <-chan TransactionUpdate) {
	select {
	case resp, ok := <-respCh:
		if !ok {
			return
		}
		sender := connOnlySender{conn: conn}
		if err := sender.SendTransactionUpdate(conn.ID, &resp); err != nil {
			logReducerDeliveryError(conn, resp.ReducerCall.RequestID, err)
		}
	case <-conn.closed:
		return
	}
}

func logReducerDeliveryError(conn *Conn, requestID uint32, err error) {
	logProtocolError(conn.Observer, "call_reducer", "send_failed", err)
}

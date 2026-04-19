package protocol

import (
	"fmt"
	"log"

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
// channel. Phase 1.5: the envelope is already fully populated by the
// executor / fan-out seam; this watcher only owns transport delivery.
func watchReducerResponse(conn *Conn, respCh <-chan TransactionUpdate) {
	go func() {
		resp, ok := <-respCh
		if !ok {
			return
		}
		sender := connOnlySender{conn: conn}
		if err := sender.SendTransactionUpdate(conn.ID, &resp); err != nil {
			logReducerDeliveryError(conn, resp.ReducerCall.RequestID, err)
		}
	}()
}

func logReducerDeliveryError(conn *Conn, requestID uint32, err error) {
	log.Printf("protocol: reducer-result delivery failed for conn %x request=%d: %v", conn.ID[:], requestID, err)
}

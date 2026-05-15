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
	return sendOnConn(s.conn, connID, msg, nil, nil)
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

// watchReducerResponse delivers the executor's TransactionUpdate to the caller.
// It exits when the owning Conn closes, even if the executor never responds.
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

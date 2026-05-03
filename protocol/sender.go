package protocol

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/types"
)

// ErrClientBufferFull is returned when a non-blocking send to a
// connection's OutboundCh finds the channel full. The caller should
// trigger a disconnect (Epic 6).
var ErrClientBufferFull = errors.New("protocol: client outbound buffer full")

// ErrConnNotFound is returned when the target ConnectionID is not in
// the ConnManager (client disconnected between evaluation and delivery).
var ErrConnNotFound = errors.New("protocol: connection not found")

// ClientSender delivers server messages to connected clients.
// Callers receive TransactionUpdate; non-callers receive TransactionUpdateLight.
type ClientSender interface {
	// Send encodes msg and enqueues the frame on the connection's
	// outbound channel. Used for direct response messages
	// (SubscribeSingleApplied, UnsubscribeSingleApplied, SubscriptionError,
	// OneOffQueryResponse).
	Send(connID types.ConnectionID, msg any) error
	// SendTransactionUpdate delivers the heavy caller-bound envelope.
	SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error
	// SendTransactionUpdateLight delivers the non-caller delta-only envelope.
	SendTransactionUpdateLight(connID types.ConnectionID, update *TransactionUpdateLight) error
}

// NewClientSender returns a ClientSender backed by mgr for connection
// lookup and frame delivery. The inbox is used to run the disconnect
// teardown sequence when a connection's outbound buffer overflows.
func NewClientSender(mgr *ConnManager, inbox ExecutorInbox) ClientSender {
	return &connManagerSender{mgr: mgr, inbox: inbox}
}

type connManagerSender struct {
	mgr   *ConnManager
	inbox ExecutorInbox
}

func (s *connManagerSender) Send(connID types.ConnectionID, msg any) error {
	return s.enqueue(connID, msg)
}

func (s *connManagerSender) SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error {
	return enqueueTransactionEnvelope(s, connID, update)
}

func (s *connManagerSender) SendTransactionUpdateLight(connID types.ConnectionID, update *TransactionUpdateLight) error {
	return enqueueTransactionEnvelope(s, connID, update)
}

func enqueueTransactionEnvelope[T TransactionUpdate | TransactionUpdateLight](s *connManagerSender, connID types.ConnectionID, update *T) error {
	conn := s.mgr.Get(connID)
	if conn == nil {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}
	return s.enqueueOnConn(conn, connID, *update)
}

// enqueue encodes msg, wraps it in the connection's compression
// envelope, and does a non-blocking send to OutboundCh.
func (s *connManagerSender) enqueue(connID types.ConnectionID, msg any) error {
	conn := s.mgr.Get(connID)
	if conn == nil {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}
	return s.enqueueOnConn(conn, connID, msg)
}

func (s *connManagerSender) enqueueOnConn(conn *Conn, connID types.ConnectionID, msg any) error {

	frame, err := EncodeServerMessage(msg)
	if err != nil {
		return fmt.Errorf("encode server message: %w", err)
	}

	wrapped := frame
	if conn.Compression {
		wrapped = EncodeFrame(frame[0], frame[1:], true, CompressionNone)
	}

	select {
	case <-conn.closed:
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	default:
	}

	select {
	case <-conn.closed:
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	case conn.OutboundCh <- wrapped:
		return nil
	default:
		logProtocolBackpressure(conn.Observer, "outbound", "buffer_full")
		conn.startOutboundOverflowDisconnect(s.inbox, s.mgr)
		return fmt.Errorf("%w: %x", ErrClientBufferFull, connID[:])
	}
}

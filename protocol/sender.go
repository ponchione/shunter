package protocol

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/types"
)

// ErrClientBufferFull is returned when a non-blocking send would exceed either
// outbound queue ceiling. The connection lifecycle is asked to disconnect the
// lagging client (Epic 6).
var ErrClientBufferFull = errors.New("protocol: client outbound buffer full")

// ErrConnNotFound is returned when the target ConnectionID is not in
// the ConnManager (client disconnected between evaluation and delivery).
var ErrConnNotFound = errors.New("protocol: connection not found")

const directResponseTooLargeText = "response exceeds configured outbound message limit"

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

// NewClientSender returns a ClientSender backed by mgr for connection lookup
// and frame delivery. The retained inbox parameter preserves the constructor
// surface; admitted connections route overflow teardown through their lifecycle
// supervisor.
func NewClientSender(mgr *ConnManager, _ ExecutorInbox) ClientSender {
	return &connManagerSender{mgr: mgr}
}

// SendDirectResponse sends a request-correlated response. If a successful
// response exceeds the connection's outbound message limit, it sends a small
// correlated failure instead. Failure to enqueue that fallback makes the
// connection terminal so a caller cannot wait indefinitely.
func SendDirectResponse(sender ClientSender, conn *Conn, msg any) error {
	if conn == nil {
		return ErrConnNotFound
	}
	if sender == nil {
		return fmt.Errorf("%w: client sender is nil", ErrConnNotFound)
	}
	err := sender.Send(conn.ID, msg)
	if err == nil || !errors.Is(err, ErrOutboundMessageLimit) {
		return err
	}

	fallback, ok := oversizedDirectResponse(msg)
	if !ok {
		conn.requestDisconnect(CloseInternal, CloseReasonResponseDeliveryFailed)
		return err
	}
	if fallbackErr := sender.Send(conn.ID, fallback); fallbackErr != nil {
		conn.requestDisconnect(CloseInternal, CloseReasonResponseDeliveryFailed)
		return errors.Join(err, fmt.Errorf("send correlated response-size error: %w", fallbackErr))
	}
	return err
}

func oversizedDirectResponse(msg any) (any, bool) {
	switch response := msg.(type) {
	case OneOffQueryResponse:
		errText := directResponseTooLargeText
		return OneOffQueryResponse{
			MessageID:                  append([]byte(nil), response.MessageID...),
			Error:                      &errText,
			TotalHostExecutionDuration: response.TotalHostExecutionDuration,
		}, true
	case ProcedureResponse:
		errText := directResponseTooLargeText
		return ProcedureResponse{
			MessageID:                  append([]byte(nil), response.MessageID...),
			Error:                      &errText,
			TotalHostExecutionDuration: response.TotalHostExecutionDuration,
		}, true
	case TransactionUpdate:
		return TransactionUpdate{
			Status:                     StatusFailed{Error: directResponseTooLargeText},
			Timestamp:                  response.Timestamp,
			CallerIdentity:             response.CallerIdentity,
			CallerConnectionID:         response.CallerConnectionID,
			TotalHostExecutionDuration: response.TotalHostExecutionDuration,
			ReducerCall: ReducerCallInfo{
				ReducerName: response.ReducerCall.ReducerName,
				ReducerID:   response.ReducerCall.ReducerID,
				RequestID:   response.ReducerCall.RequestID,
			},
		}, true
	default:
		return nil, false
	}
}

type connManagerSender struct {
	mgr *ConnManager
}

type outboundSendResult uint8

const (
	outboundSendSent outboundSendResult = iota
	outboundSendClosed
	outboundSendFull
	outboundSendBytesFull
)

func (c *Conn) trySendOutbound(frame []byte) (result outboundSendResult) {
	if c == nil {
		return outboundSendClosed
	}
	c.outboundMu.Lock()
	defer c.outboundMu.Unlock()
	defer func() {
		if recover() != nil {
			result = outboundSendClosed
		}
	}()
	if c.outboundStopped {
		return outboundSendClosed
	}
	select {
	case <-c.closed:
		return outboundSendClosed
	default:
	}
	frameBytes := int64(len(frame))
	maxQueuedBytes := DefaultMaxOutboundQueuedBytes
	if c.opts != nil && c.opts.MaxOutboundQueuedBytes > 0 {
		maxQueuedBytes = c.opts.MaxOutboundQueuedBytes
	}
	if frameBytes > maxQueuedBytes-c.outboundQueuedBytes {
		return outboundSendBytesFull
	}
	select {
	case <-c.closed:
		return outboundSendClosed
	case c.OutboundCh <- frame:
		c.outboundQueuedBytes += frameBytes
		return outboundSendSent
	default:
		return outboundSendFull
	}
}

func (c *Conn) releaseOutboundBytes(frameBytes int) {
	if c == nil || frameBytes <= 0 {
		return
	}
	c.outboundMu.Lock()
	defer c.outboundMu.Unlock()
	n := int64(frameBytes)
	if n >= c.outboundQueuedBytes {
		c.outboundQueuedBytes = 0
		return
	}
	c.outboundQueuedBytes -= n
}

func (c *Conn) abandonOutboundQueue() {
	if c == nil || c.OutboundCh == nil {
		return
	}
	for {
		select {
		case frame, ok := <-c.OutboundCh:
			if !ok {
				return
			}
			c.releaseOutboundBytes(len(frame))
		default:
			return
		}
	}
}

func (c *Conn) stopAndAbandonOutboundQueue() {
	if c == nil {
		return
	}
	c.outboundMu.Lock()
	c.outboundStopped = true
	c.outboundMu.Unlock()
	c.abandonOutboundQueue()
}

func (c *Conn) outboundQueuedByteCount() int64 {
	if c == nil {
		return 0
	}
	c.outboundMu.Lock()
	defer c.outboundMu.Unlock()
	return c.outboundQueuedBytes
}

func (s *connManagerSender) Send(connID types.ConnectionID, msg any) error {
	return s.enqueue(connID, msg)
}

func (s *connManagerSender) SendTransactionUpdate(connID types.ConnectionID, update *TransactionUpdate) error {
	if update == nil {
		return nil
	}
	return enqueueTransactionEnvelope(s, connID, update)
}

func (s *connManagerSender) SendTransactionUpdateLight(connID types.ConnectionID, update *TransactionUpdateLight) error {
	if update == nil {
		return nil
	}
	return enqueueTransactionEnvelope(s, connID, update)
}

func enqueueTransactionEnvelope[T TransactionUpdate | TransactionUpdateLight](s *connManagerSender, connID types.ConnectionID, update *T) error {
	if s == nil || s.mgr == nil {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}
	conn := s.mgr.Get(connID)
	if conn == nil {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}
	return s.enqueueOnConn(conn, connID, *update)
}

// enqueue encodes msg, wraps it in the connection's compression
// envelope, and does a non-blocking send to OutboundCh.
func (s *connManagerSender) enqueue(connID types.ConnectionID, msg any) error {
	if s == nil || s.mgr == nil {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}
	conn := s.mgr.Get(connID)
	if conn == nil {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}
	return s.enqueueOnConn(conn, connID, msg)
}

func (s *connManagerSender) enqueueOnConn(conn *Conn, connID types.ConnectionID, msg any) error {
	return sendOnConn(conn, connID, msg)
}

func sendOnConn(conn *Conn, connID types.ConnectionID, msg any) error {
	if conn == nil {
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	}
	maxBytes := 0
	if conn.opts != nil {
		maxBytes = conn.opts.MaxOutboundMessageSize
	}
	frame, err := EncodeServerMessageWithLimit(msg, maxBytes)
	if err != nil {
		return fmt.Errorf("encode server message: %w", err)
	}

	wrapped := EncodeFrame(frame[0], frame[1:], conn.Compression, outboundCompressionMode(conn))

	switch conn.trySendOutbound(wrapped) {
	case outboundSendSent:
		return nil
	case outboundSendClosed:
		return fmt.Errorf("%w: %x", ErrConnNotFound, connID[:])
	case outboundSendFull, outboundSendBytesFull:
		logProtocolBackpressure(conn.Observer, "outbound", "buffer_full")
		conn.requestDisconnect(ClosePolicy, CloseReasonSendBufferFull)
		return fmt.Errorf("%w: %x", ErrClientBufferFull, connID[:])
	default:
		panic("protocol: unknown outbound send result")
	}
}

func outboundCompressionMode(conn *Conn) uint8 {
	if conn != nil && conn.Compression {
		return CompressionGzip
	}
	return CompressionNone
}

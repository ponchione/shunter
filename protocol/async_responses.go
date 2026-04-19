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

// watchSubscribeSetResponse listens for the executor's
// SubscriptionSetCommandResponse. On success it emits the appropriate
// applied envelope (SingleApplied or MultiApplied); on error it emits
// a SubscriptionError. A malformed response (no arm populated) is
// logged but does not crash the connection.
func watchSubscribeSetResponse(
	conn *Conn,
	respCh <-chan SubscriptionSetCommandResponse,
	single bool,
	requestID uint32,
	queryID uint32,
) {
	go func() {
		resp, ok := <-respCh
		if !ok {
			return
		}
		sender := connOnlySender{conn: conn}
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: async SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case single && resp.SingleApplied != nil:
			if err := SendSubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				log.Printf("protocol: async SubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.SingleApplied.QueryID, err)
			}
		case !single && resp.MultiApplied != nil:
			if err := SendSubscribeMultiApplied(sender, conn, resp.MultiApplied); err != nil {
				log.Printf("protocol: async SubscribeMultiApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.MultiApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed SubscriptionSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}()
}

// watchUnsubscribeSetResponse mirrors watchSubscribeSetResponse for the
// unsubscribe path.
func watchUnsubscribeSetResponse(
	conn *Conn,
	respCh <-chan UnsubscribeSetCommandResponse,
	single bool,
	requestID uint32,
	queryID uint32,
) {
	go func() {
		resp, ok := <-respCh
		if !ok {
			return
		}
		sender := connOnlySender{conn: conn}
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: async unsubscribe SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case single && resp.SingleApplied != nil:
			if err := SendUnsubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				log.Printf("protocol: async UnsubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.SingleApplied.QueryID, err)
			}
		case !single && resp.MultiApplied != nil:
			if err := SendUnsubscribeMultiApplied(sender, conn, resp.MultiApplied); err != nil {
				log.Printf("protocol: async UnsubscribeMultiApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.MultiApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed UnsubscribeSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}()
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

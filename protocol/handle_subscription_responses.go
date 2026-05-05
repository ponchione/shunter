package protocol

import (
	"errors"
	"time"
)

func sendSubscribeCompileError(conn *Conn, receipt time.Time, requestID, queryID uint32, err error, sqlText string) {
	sendError(conn, SubscriptionError{
		TotalHostExecutionDurationMicros: elapsedMicros(receipt),
		RequestID:                        optionalUint32(requestID),
		QueryID:                          optionalUint32(queryID),
		Error:                            wrapSubscribeCompileErrorSQL(err, sqlText),
	})
}

func sendExecutorUnavailableError(conn *Conn, receipt time.Time, requestID, queryID uint32, err error) {
	sendError(conn, SubscriptionError{
		TotalHostExecutionDurationMicros: elapsedMicros(receipt),
		RequestID:                        optionalUint32(requestID),
		QueryID:                          optionalUint32(queryID),
		Error:                            "executor unavailable: " + err.Error(),
	})
}

func makeSubscribeSetReply(conn *Conn, requestID, queryID uint32, variant SubscriptionSetVariant) func(SubscriptionSetCommandResponse) {
	return makeSetReply(conn, requestID, queryID, variant, subscribeSetReplyDelivery)
}

func makeUnsubscribeSetReply(conn *Conn, requestID, queryID uint32, variant SubscriptionSetVariant) func(UnsubscribeSetCommandResponse) {
	return makeSetReply(conn, requestID, queryID, variant, unsubscribeSetReplyDelivery)
}

type setReplyDelivery[R any] struct {
	errorOf    func(R) *SubscriptionError
	sendSingle func(connOnlySender, *Conn, R) (bool, error)
	sendMulti  func(connOnlySender, *Conn, R) (bool, error)
}

var subscribeSetReplyDelivery = newSetReplyDelivery(
	func(resp SubscriptionSetCommandResponse) *SubscriptionError { return resp.Error },
	func(resp SubscriptionSetCommandResponse) *SubscribeSingleApplied { return resp.SingleApplied },
	func(resp SubscriptionSetCommandResponse) *SubscribeMultiApplied { return resp.MultiApplied },
	SendSubscribeSingleApplied,
	SendSubscribeMultiApplied,
)

var unsubscribeSetReplyDelivery = newSetReplyDelivery(
	func(resp UnsubscribeSetCommandResponse) *SubscriptionError { return resp.Error },
	func(resp UnsubscribeSetCommandResponse) *UnsubscribeSingleApplied { return resp.SingleApplied },
	func(resp UnsubscribeSetCommandResponse) *UnsubscribeMultiApplied { return resp.MultiApplied },
	SendUnsubscribeSingleApplied,
	SendUnsubscribeMultiApplied,
)

func newSetReplyDelivery[R any, S interface {
	*SubscribeSingleApplied | *UnsubscribeSingleApplied
}, M interface {
	*SubscribeMultiApplied | *UnsubscribeMultiApplied
}](
	errorOf func(R) *SubscriptionError,
	singleOf func(R) S,
	multiOf func(R) M,
	sendSingle func(ClientSender, *Conn, S) error,
	sendMulti func(ClientSender, *Conn, M) error,
) setReplyDelivery[R] {
	return setReplyDelivery[R]{
		errorOf:    errorOf,
		sendSingle: sendPresentApplied(singleOf, sendSingle),
		sendMulti:  sendPresentApplied(multiOf, sendMulti),
	}
}

func sendPresentApplied[R any, A comparable](
	appliedOf func(R) A,
	send func(ClientSender, *Conn, A) error,
) func(connOnlySender, *Conn, R) (bool, error) {
	return func(sender connOnlySender, conn *Conn, resp R) (bool, error) {
		applied := appliedOf(resp)
		var zero A
		if applied == zero {
			return false, nil
		}
		return true, send(sender, conn, applied)
	}
}

func makeSetReply[R any](
	conn *Conn,
	requestID, queryID uint32,
	variant SubscriptionSetVariant,
	delivery setReplyDelivery[R],
) func(R) {
	return func(resp R) {
		sender := connOnlySender{conn: conn}
		if respErr := delivery.errorOf(resp); respErr != nil {
			if err := SendSubscriptionError(sender, conn, respErr); err != nil {
				logSubscriptionDeliveryFailure(conn, err)
			}
			return
		}
		if variant == SubscriptionSetVariantSingle {
			if ok, err := delivery.sendSingle(sender, conn, resp); ok {
				if err != nil {
					logSubscriptionDeliveryFailure(conn, err)
				}
				return
			}
		}
		if variant == SubscriptionSetVariantMulti {
			if ok, err := delivery.sendMulti(sender, conn, resp); ok {
				if err != nil {
					logSubscriptionDeliveryFailure(conn, err)
				}
				return
			}
		}
		logProtocolError(conn.Observer, "unknown", "malformed", errors.New("malformed subscription response"))
	}
}

func logSubscriptionDeliveryFailure(conn *Conn, err error) {
	logProtocolError(conn.Observer, "unknown", "send_failed", err)
}

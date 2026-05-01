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
	errorLabel    string
	singleLabel   string
	multiLabel    string
	malformedType string
	errorOf       func(R) *SubscriptionError
	sendSingle    func(connOnlySender, *Conn, R) (uint32, bool, error)
	sendMulti     func(connOnlySender, *Conn, R) (uint32, bool, error)
}

var subscribeSetReplyDelivery = newSetReplyDelivery(
	"SubscriptionError",
	"SubscribeSingleApplied",
	"SubscribeMultiApplied",
	"SubscriptionSetCommandResponse",
	func(resp SubscriptionSetCommandResponse) *SubscriptionError { return resp.Error },
	func(resp SubscriptionSetCommandResponse) *SubscribeSingleApplied { return resp.SingleApplied },
	func(resp SubscriptionSetCommandResponse) *SubscribeMultiApplied { return resp.MultiApplied },
	SendSubscribeSingleApplied,
	SendSubscribeMultiApplied,
)

var unsubscribeSetReplyDelivery = newSetReplyDelivery(
	"unsubscribe SubscriptionError",
	"UnsubscribeSingleApplied",
	"UnsubscribeMultiApplied",
	"UnsubscribeSetCommandResponse",
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
	errorLabel, singleLabel, multiLabel, malformedType string,
	errorOf func(R) *SubscriptionError,
	singleOf func(R) S,
	multiOf func(R) M,
	sendSingle func(ClientSender, *Conn, S) error,
	sendMulti func(ClientSender, *Conn, M) error,
) setReplyDelivery[R] {
	return setReplyDelivery[R]{
		errorLabel:    errorLabel,
		singleLabel:   singleLabel,
		multiLabel:    multiLabel,
		malformedType: malformedType,
		errorOf:       errorOf,
		sendSingle:    sendPresentApplied(singleOf, appliedQueryID[S], sendSingle),
		sendMulti:     sendPresentApplied(multiOf, appliedQueryID[M], sendMulti),
	}
}

func appliedQueryID[A interface {
	*SubscribeSingleApplied | *UnsubscribeSingleApplied | *SubscribeMultiApplied | *UnsubscribeMultiApplied
}](msg A) uint32 {
	switch v := any(msg).(type) {
	case *SubscribeSingleApplied:
		return v.QueryID
	case *UnsubscribeSingleApplied:
		return v.QueryID
	case *SubscribeMultiApplied:
		return v.QueryID
	case *UnsubscribeMultiApplied:
		return v.QueryID
	default:
		return 0
	}
}

func sendPresentApplied[R any, A comparable](
	appliedOf func(R) A,
	queryIDOf func(A) uint32,
	send func(ClientSender, *Conn, A) error,
) func(connOnlySender, *Conn, R) (uint32, bool, error) {
	return func(sender connOnlySender, conn *Conn, resp R) (uint32, bool, error) {
		applied := appliedOf(resp)
		var zero A
		if applied == zero {
			return 0, false, nil
		}
		return queryIDOf(applied), true, send(sender, conn, applied)
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
				logSubscriptionErrorDeliveryFailure(conn, delivery.errorLabel, respErr, err)
			}
			return
		}
		if variant == SubscriptionSetVariantSingle {
			if deliveredQueryID, ok, err := delivery.sendSingle(sender, conn, resp); ok {
				if err != nil {
					logAppliedDeliveryFailure(conn, delivery.singleLabel, deliveredQueryID, err)
				}
				return
			}
		}
		if variant == SubscriptionSetVariantMulti {
			if deliveredQueryID, ok, err := delivery.sendMulti(sender, conn, resp); ok {
				if err != nil {
					logAppliedDeliveryFailure(conn, delivery.multiLabel, deliveredQueryID, err)
				}
				return
			}
		}
		logProtocolError(conn.Observer, "unknown", "malformed", errors.New("malformed subscription response"))
	}
}

func logSubscriptionErrorDeliveryFailure(conn *Conn, label string, resp *SubscriptionError, err error) {
	logProtocolError(conn.Observer, "unknown", "send_failed", err)
}

func logAppliedDeliveryFailure(conn *Conn, label string, queryID uint32, err error) {
	logProtocolError(conn.Observer, "unknown", "send_failed", err)
}

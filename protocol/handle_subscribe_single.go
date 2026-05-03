package protocol

import (
	"context"
	"time"

	"github.com/ponchione/shunter/types"
)

// handleSubscribeSingle validates one SQL query and submits it to the executor.
// The receipt timestamp covers local validation and executor admission.
func handleSubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeSingleMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	handleSubscribeSingleWithVisibility(ctx, conn, msg, executor, sl, nil)
}

func handleSubscribeSingleWithVisibility(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeSingleMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
) {
	receipt := time.Now()
	readSL := authorizedSchemaLookupForConn(sl, conn)
	caller := readCallerContext(conn)
	compiled, err := CompileSQLQueryStringWithVisibility(msg.QueryString, readSL, &caller.Identity, SQLQueryValidationOptions{
		AllowLimit:      false,
		AllowProjection: false,
	}, visibilityFilters, caller.AllowAllPermissions)
	if err != nil {
		sendSubscribeCompileError(conn, receipt, msg.RequestID, msg.QueryID, err, msg.QueryString)
		recordProtocolMessage(conn.Observer, "subscribe_single", "validation_error")
		return
	}
	pred := compiled.Predicate()

	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:                  conn.ID,
		QueryID:                 msg.QueryID,
		RequestID:               msg.RequestID,
		Variant:                 SubscriptionSetVariantSingle,
		Predicates:              []any{pred},
		PredicateHashIdentities: []*types.Identity{compiled.PredicateHashIdentity(caller.Identity)},
		Reply:                   makeSubscribeSetReply(conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantSingle),
		Receipt:                 receipt,
		SQLText:                 msg.QueryString,
	}); submitErr != nil {
		sendExecutorUnavailableError(conn, receipt, msg.RequestID, msg.QueryID, submitErr)
		recordProtocolMessage(conn.Observer, "subscribe_single", "executor_rejected")
		return
	}
	recordProtocolMessage(conn.Observer, "subscribe_single", "ok")
}

// elapsedMicros reports the non-zero microsecond delta since receipt.
// A zero measurement is bumped to 1 so downstream consumers can
// distinguish "measured" from the deferred-measurement sentinel (0)
// that error-origin SubscriptionError emitters used before the seam.
func elapsedMicros(receipt time.Time) uint64 {
	us := uint64(time.Since(receipt).Microseconds())
	if us == 0 {
		return 1
	}
	return us
}

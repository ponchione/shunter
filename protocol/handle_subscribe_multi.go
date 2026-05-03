package protocol

import (
	"context"
	"time"

	"github.com/ponchione/shunter/types"
)

// handleSubscribeMulti validates all SQL queries and submits them atomically.
// The receipt timestamp covers local validation and executor admission.
func handleSubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	handleSubscribeMultiWithVisibility(ctx, conn, msg, executor, sl, nil)
}

func handleSubscribeMultiWithVisibility(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
) {
	receipt := time.Now()
	readSL := authorizedSchemaLookupForConn(sl, conn)
	caller := readCallerContext(conn)
	preds := make([]any, 0, len(msg.QueryStrings))
	hashIdentities := make([]*types.Identity, 0, len(msg.QueryStrings))
	for _, qs := range msg.QueryStrings {
		compiled, err := CompileSQLQueryStringWithVisibility(qs, readSL, &caller.Identity, SQLQueryValidationOptions{
			AllowLimit:      false,
			AllowProjection: false,
		}, visibilityFilters, caller.AllowAllPermissions)
		if err != nil {
			sendSubscribeCompileError(conn, receipt, msg.RequestID, msg.QueryID, err, qs)
			recordProtocolMessage(conn.Observer, "subscribe_multi", "validation_error")
			return
		}
		preds = append(preds, compiled.Predicate())
		hashIdentities = append(hashIdentities, compiled.PredicateHashIdentity(caller.Identity))
	}

	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:                  conn.ID,
		QueryID:                 msg.QueryID,
		RequestID:               msg.RequestID,
		Variant:                 SubscriptionSetVariantMulti,
		Predicates:              preds,
		PredicateHashIdentities: hashIdentities,
		Reply:                   makeSubscribeSetReply(conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantMulti),
		Receipt:                 receipt,
	}); submitErr != nil {
		sendExecutorUnavailableError(conn, receipt, msg.RequestID, msg.QueryID, submitErr)
		recordProtocolMessage(conn.Observer, "subscribe_multi", "executor_rejected")
		return
	}
	recordProtocolMessage(conn.Observer, "subscribe_multi", "ok")
}

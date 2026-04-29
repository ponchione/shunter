package protocol

import (
	"context"
	"time"

	"github.com/ponchione/shunter/types"
)

// handleSubscribeMulti processes an incoming SubscribeMultiMsg. Every
// wire query is compiled against the schema; the first compile error
// aborts the entire batch and emits a SubscriptionError (atomic
// admission per SPEC-005 §7.1b). On success the N predicates are
// forwarded to the executor under a single QueryID via the set-based
// seam. The executor invokes the Reply closure synchronously on its
// own goroutine; the closure enqueues either a SubscribeMultiApplied
// or a SubscriptionError onto the connection's outbound channel.
// Synchronous dispatch here is what enforces ADR §9.4 FIFO between
// Applied and any subsequent fan-out.
//
// Receipt-timestamp seam: see handleSubscribeSingle.
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
		return
	}
}

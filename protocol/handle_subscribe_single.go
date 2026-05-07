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
	handleSubscribeSetWithVisibility(ctx, conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantSingle, []string{msg.QueryString}, msg.QueryString, executor, sl, visibilityFilters)
}

type rawSubscribeAdmissionPlan struct {
	predicates              []any
	predicateHashIdentities []*types.Identity
}

func compileRawSubscribeAdmissionPlan(
	queryStrings []string,
	sl SchemaLookup,
	caller types.CallerContext,
	visibilityFilters []VisibilityFilter,
) (rawSubscribeAdmissionPlan, string, error) {
	plan := rawSubscribeAdmissionPlan{
		predicates:              make([]any, 0, len(queryStrings)),
		predicateHashIdentities: make([]*types.Identity, 0, len(queryStrings)),
	}
	for _, qs := range queryStrings {
		compiled, err := CompileSQLQueryStringWithVisibility(qs, sl, &caller.Identity, SQLQueryValidationOptions{
			AllowLimit:      false,
			AllowProjection: false,
		}, visibilityFilters, caller.AllowAllPermissions)
		if err != nil {
			return rawSubscribeAdmissionPlan{}, qs, err
		}
		plan.predicates = append(plan.predicates, compiled.Predicate())
		plan.predicateHashIdentities = append(plan.predicateHashIdentities, compiled.PredicateHashIdentity(caller.Identity))
	}
	return plan, "", nil
}

func handleSubscribeSetWithVisibility(
	ctx context.Context,
	conn *Conn,
	requestID, queryID uint32,
	variant SubscriptionSetVariant,
	queryStrings []string,
	sqlText string,
	executor ExecutorInbox,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
) {
	receipt := time.Now()
	readSL := authorizedSchemaLookupForConn(sl, conn)
	caller := readCallerContext(conn)
	plan, failedSQL, err := compileRawSubscribeAdmissionPlan(queryStrings, readSL, caller, visibilityFilters)
	if err != nil {
		sendSubscribeCompileError(conn, receipt, requestID, queryID, err, failedSQL)
		recordProtocolMessage(conn.Observer, protocolSubscribeMetricKind(variant), "validation_error")
		return
	}

	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:                  conn.ID,
		QueryID:                 queryID,
		RequestID:               requestID,
		Variant:                 variant,
		Predicates:              plan.predicates,
		PredicateHashIdentities: plan.predicateHashIdentities,
		Reply:                   makeSubscribeSetReply(conn, requestID, queryID, variant),
		Receipt:                 receipt,
		SQLText:                 sqlText,
	}); submitErr != nil {
		sendExecutorUnavailableError(conn, receipt, requestID, queryID, submitErr)
		recordProtocolMessage(conn.Observer, protocolSubscribeMetricKind(variant), "executor_rejected")
		return
	}
	recordProtocolMessage(conn.Observer, protocolSubscribeMetricKind(variant), "ok")
}

func protocolSubscribeMetricKind(variant SubscriptionSetVariant) string {
	if variant == SubscriptionSetVariantMulti {
		return "subscribe_multi"
	}
	return "subscribe_single"
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

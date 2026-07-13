package protocol

import (
	"context"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
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
	handleSubscribeSingleWithVisibilityAndLimit(ctx, conn, msg, executor, sl, visibilityFilters, DefaultSubscriptionMaxQueriesPerSet)
}

func handleSubscribeSingleWithVisibilityAndLimit(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeSingleMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
	maxQueriesPerSet int,
) {
	handleSubscribeSetWithVisibilityAndLimit(ctx, conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantSingle, []string{msg.QueryString}, msg.QueryString, executor, sl, visibilityFilters, maxQueriesPerSet)
}

type rawSubscribeAdmissionPlan struct {
	queries []rawSubscribeAdmissionPlanQuery
}

type rawSubscribeAdmissionPlanQuery struct {
	sqlText               string
	predicate             subscription.Predicate
	predicateHashIdentity *types.Identity
	usesCallerIdentity    bool
	referencedTables      []schema.TableID
	relations             []AdmissionRelation
	joinConditions        []AdmissionJoinCondition
	projectedRelation     int
	resultShape           rawSubscribeAdmissionResultShape
}

type rawSubscribeAdmissionResultShape struct {
	tableName  string
	projection []subscription.ProjectionColumn
	aggregate  *subscription.Aggregate
	orderBy    []subscription.OrderByColumn
	hasOrderBy bool
	limit      *uint64
	offset     *uint64
}

func (s rawSubscribeAdmissionResultShape) tableShaped() bool {
	return len(s.projection) == 0 &&
		s.aggregate == nil &&
		len(s.orderBy) == 0 &&
		!s.hasOrderBy &&
		s.limit == nil &&
		s.offset == nil
}

func (p rawSubscribeAdmissionPlan) predicates() []any {
	out := make([]any, len(p.queries))
	for i, query := range p.queries {
		out[i] = query.predicate
	}
	return out
}

func (p rawSubscribeAdmissionPlan) predicateHashIdentities() []*types.Identity {
	out := make([]*types.Identity, len(p.queries))
	for i, query := range p.queries {
		out[i] = query.predicateHashIdentity
	}
	return out
}

func compileRawSubscribeAdmissionPlan(
	queryStrings []string,
	sl SchemaLookup,
	caller types.CallerContext,
	visibilityFilters []VisibilityFilter,
) (rawSubscribeAdmissionPlan, string, error) {
	plan := rawSubscribeAdmissionPlan{
		queries: make([]rawSubscribeAdmissionPlanQuery, 0, len(queryStrings)),
	}
	for _, qs := range queryStrings {
		compiled, err := CompileSQLQueryStringWithVisibility(qs, sl, &caller.Identity, SQLQueryValidationOptions{
			AllowLimit:      false,
			AllowProjection: false,
		}, visibilityFilters, caller.AllowAllPermissions)
		if err != nil {
			return rawSubscribeAdmissionPlan{}, qs, err
		}
		relations, joinConditions := AdmissionJoinGraph(compiled.Predicate(), sl)
		plan.queries = append(plan.queries, rawSubscribeAdmissionPlanQuery{
			sqlText:               qs,
			predicate:             compiled.Predicate(),
			predicateHashIdentity: compiled.PredicateHashIdentity(caller.Identity),
			usesCallerIdentity:    compiled.UsesCallerIdentity(),
			referencedTables:      compiled.ReferencedTables(),
			relations:             relations,
			joinConditions:        joinConditions,
			projectedRelation:     AdmissionProjectedRelation(compiled.Predicate()),
			resultShape: rawSubscribeAdmissionResultShape{
				tableName:  compiled.TableName(),
				projection: compiled.SubscriptionProjection(),
				aggregate:  compiled.SubscriptionAggregate(),
				orderBy:    compiled.SubscriptionOrderBy(),
				hasOrderBy: compiled.HasOrderBy(),
				limit:      compiled.SubscriptionLimit(),
				offset:     compiled.SubscriptionOffset(),
			},
		})
	}
	return plan, "", nil
}

func handleSubscribeSetWithVisibilityAndLimit(
	ctx context.Context,
	conn *Conn,
	requestID, queryID uint32,
	variant SubscriptionSetVariant,
	queryStrings []string,
	sqlText string,
	executor ExecutorInbox,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
	maxQueriesPerSet int,
) {
	receipt := time.Now()
	if maxQueriesPerSet > 0 && len(queryStrings) > maxQueriesPerSet {
		err := subscription.NewQuotaError(subscription.ErrSubscriptionQueryLimit, "queries_per_set", len(queryStrings), maxQueriesPerSet)
		sendSubscribeCompileError(conn, receipt, requestID, queryID, err, sqlText)
		recordProtocolMessage(conn.Observer, protocolSubscribeMetricKind(variant), "quota_rejected")
		return
	}
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
		Predicates:              plan.predicates(),
		PredicateHashIdentities: plan.predicateHashIdentities(),
		Reply:                   makeSubscribeSetReply(conn, requestID, queryID, variant),
		Receipt:                 receipt,
		SQLText:                 sqlText,
		MaxResponseBytes:        subscriptionResponseByteLimit(conn),
	}); submitErr != nil {
		sendExecutorUnavailableError(conn, receipt, requestID, queryID, submitErr)
		recordProtocolMessage(conn.Observer, protocolSubscribeMetricKind(variant), "executor_rejected")
		return
	}
	recordProtocolMessage(conn.Observer, protocolSubscribeMetricKind(variant), "ok")
}

func subscriptionResponseByteLimit(conn *Conn) int {
	if conn == nil || conn.opts == nil {
		return 0
	}
	return conn.opts.MaxOutboundMessageSize
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
	return uint64(elapsedMicrosI64(receipt))
}

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
	handleSubscribeSetWithVisibility(ctx, conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantSingle, []string{msg.QueryString}, msg.QueryString, executor, sl, visibilityFilters)
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
	relations             []rawSubscribeAdmissionRelation
	joinConditions        []rawSubscribeAdmissionJoinCondition
	projectedRelation     int
	resultShape           rawSubscribeAdmissionResultShape
}

type rawSubscribeAdmissionRelation struct {
	relation int
	table    schema.TableID
	alias    uint8
}

type rawSubscribeAdmissionColumnRef struct {
	relation int
	table    schema.TableID
	column   types.ColID
	alias    uint8
	indexed  bool
}

type rawSubscribeAdmissionJoinCondition struct {
	left  rawSubscribeAdmissionColumnRef
	right rawSubscribeAdmissionColumnRef
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
		relations, joinConditions := rawSubscribeAdmissionJoinGraph(compiled.Predicate(), sl)
		plan.queries = append(plan.queries, rawSubscribeAdmissionPlanQuery{
			sqlText:               qs,
			predicate:             compiled.Predicate(),
			predicateHashIdentity: compiled.PredicateHashIdentity(caller.Identity),
			usesCallerIdentity:    compiled.UsesCallerIdentity(),
			referencedTables:      compiled.ReferencedTables(),
			relations:             relations,
			joinConditions:        joinConditions,
			projectedRelation:     rawSubscribeAdmissionProjectedRelation(compiled.Predicate()),
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

func rawSubscribeAdmissionProjectedRelation(pred subscription.Predicate) int {
	switch p := pred.(type) {
	case subscription.Join:
		if p.ProjectRight {
			return 1
		}
		return 0
	case subscription.CrossJoin:
		if p.ProjectRight {
			return 1
		}
		return 0
	case subscription.MultiJoin:
		return p.ProjectedRelation
	default:
		if pred == nil || len(pred.Tables()) == 0 {
			return -1
		}
		return 0
	}
}

func rawSubscribeAdmissionJoinGraph(pred subscription.Predicate, sl SchemaLookup) ([]rawSubscribeAdmissionRelation, []rawSubscribeAdmissionJoinCondition) {
	if pred == nil {
		return nil, nil
	}
	switch p := pred.(type) {
	case subscription.Join:
		relations := []rawSubscribeAdmissionRelation{
			{relation: 0, table: p.Left, alias: p.LeftAlias},
			{relation: 1, table: p.Right, alias: p.RightAlias},
		}
		return relations, []rawSubscribeAdmissionJoinCondition{
			{
				left: rawSubscribeAdmissionColumnRef{
					relation: 0,
					table:    p.Left,
					column:   p.LeftCol,
					alias:    p.LeftAlias,
					indexed:  sl != nil && sl.HasIndex(p.Left, p.LeftCol),
				},
				right: rawSubscribeAdmissionColumnRef{
					relation: 1,
					table:    p.Right,
					column:   p.RightCol,
					alias:    p.RightAlias,
					indexed:  sl != nil && sl.HasIndex(p.Right, p.RightCol),
				},
			},
		}
	case subscription.CrossJoin:
		return []rawSubscribeAdmissionRelation{
			{relation: 0, table: p.Left, alias: p.LeftAlias},
			{relation: 1, table: p.Right, alias: p.RightAlias},
		}, nil
	case subscription.MultiJoin:
		relations := make([]rawSubscribeAdmissionRelation, len(p.Relations))
		for i, relation := range p.Relations {
			relations[i] = rawSubscribeAdmissionRelation{
				relation: i,
				table:    relation.Table,
				alias:    relation.Alias,
			}
		}
		conditions := make([]rawSubscribeAdmissionJoinCondition, len(p.Conditions))
		for i, condition := range p.Conditions {
			conditions[i] = rawSubscribeAdmissionJoinCondition{
				left:  rawSubscribeAdmissionColumnRefFromMultiJoin(condition.Left, sl),
				right: rawSubscribeAdmissionColumnRefFromMultiJoin(condition.Right, sl),
			}
		}
		return relations, conditions
	default:
		tables := pred.Tables()
		relations := make([]rawSubscribeAdmissionRelation, len(tables))
		for i, table := range tables {
			relations[i] = rawSubscribeAdmissionRelation{
				relation: i,
				table:    table,
			}
		}
		return relations, nil
	}
}

func rawSubscribeAdmissionColumnRefFromMultiJoin(ref subscription.MultiJoinColumnRef, sl SchemaLookup) rawSubscribeAdmissionColumnRef {
	return rawSubscribeAdmissionColumnRef{
		relation: ref.Relation,
		table:    ref.Table,
		column:   ref.Column,
		alias:    ref.Alias,
		indexed:  sl != nil && sl.HasIndex(ref.Table, ref.Column),
	}
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
		Predicates:              plan.predicates(),
		PredicateHashIdentities: plan.predicateHashIdentities(),
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

package shunter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

var (
	// ErrUnknownDeclaredRead reports that no executable declared read exists
	// for the supplied declaration name and API kind.
	ErrUnknownDeclaredRead = errors.New("shunter: unknown declared read")
	// ErrDeclaredReadNotExecutable reports a metadata-only query/view
	// declaration used through an execution API.
	ErrDeclaredReadNotExecutable = errors.New("shunter: declared read is metadata-only")

	errDeclaredReadParameter = errors.New("shunter: declared read parameter validation")
)

type declaredReadParameterError struct {
	message string
}

func (e declaredReadParameterError) Error() string {
	return e.message
}

func (e declaredReadParameterError) Is(target error) bool {
	return target == errDeclaredReadParameter
}

func declaredReadParameterErrorf(format string, args ...any) error {
	return declaredReadParameterError{message: fmt.Sprintf(format, args...)}
}

// DeclaredQueryResult is the detached row result returned by CallQuery.
type DeclaredQueryResult struct {
	Name      string
	TableName string
	Columns   []schema.ColumnSchema
	Rows      []types.ProductValue
}

// DeclaredViewSubscription is the local admission result returned by
// SubscribeView.
type DeclaredViewSubscription struct {
	Name        string
	QueryID     uint32
	RequestID   uint32
	TableName   string
	Columns     []schema.ColumnSchema
	InitialRows []types.ProductValue
}

// DeclaredReadOption configures local named query/view calls.
type DeclaredReadOption func(*declaredReadOptions)

type declaredReadOptions struct {
	caller         types.CallerContext
	requestID      uint32
	parameters     *types.ProductValue
	permissionsSet bool
}

// WithDeclaredReadIdentity sets the local caller identity for named reads.
func WithDeclaredReadIdentity(identity types.Identity) DeclaredReadOption {
	return func(opts *declaredReadOptions) {
		opts.caller.Identity = identity
	}
}

// WithDeclaredReadConnectionID sets the local caller connection ID for named
// view subscriptions.
func WithDeclaredReadConnectionID(connID types.ConnectionID) DeclaredReadOption {
	return func(opts *declaredReadOptions) {
		opts.caller.ConnectionID = connID
	}
}

// WithDeclaredReadAuthPrincipal sets generic external-auth principal data for
// a local named read without requiring a raw JWT.
func WithDeclaredReadAuthPrincipal(principal types.AuthPrincipal) DeclaredReadOption {
	return func(opts *declaredReadOptions) {
		opts.caller.Principal = principal.Copy()
	}
}

// WithDeclaredReadPermissions sets the caller permission tags for named reads.
func WithDeclaredReadPermissions(permissions ...string) DeclaredReadOption {
	return func(opts *declaredReadOptions) {
		opts.caller.Permissions = append([]string(nil), permissions...)
		opts.caller.AllowAllPermissions = false
		opts.permissionsSet = true
	}
}

// WithDeclaredReadAllowAllPermissions enables the admin/dev permission bypass
// for a named read call.
func WithDeclaredReadAllowAllPermissions() DeclaredReadOption {
	return func(opts *declaredReadOptions) {
		opts.caller.Permissions = nil
		opts.caller.AllowAllPermissions = true
		opts.permissionsSet = true
	}
}

// WithDeclaredReadRequestID attaches a caller-chosen request ID to a local
// named view subscription.
func WithDeclaredReadRequestID(requestID uint32) DeclaredReadOption {
	return func(opts *declaredReadOptions) {
		opts.requestID = requestID
	}
}

// WithDeclaredReadParameters supplies ordered declared-read parameter values.
func WithDeclaredReadParameters(values types.ProductValue) DeclaredReadOption {
	return func(opts *declaredReadOptions) {
		copied := values.Copy()
		opts.parameters = &copied
	}
}

// CallQuery executes an executable QueryDeclaration by declaration name.
func (r *Runtime) CallQuery(ctx context.Context, name string, opts ...DeclaredReadOption) (DeclaredQueryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return DeclaredQueryResult{}, err
	}
	state, err := r.readyState()
	if err != nil {
		return DeclaredQueryResult{}, err
	}
	callOpts := r.applyDeclaredReadOptions(opts)
	entry, err := r.authorizedDeclaredRead(name, declaredReadKindQuery, callOpts.caller)
	if err != nil {
		return DeclaredQueryResult{}, err
	}
	compiled, err := r.executableDeclaredRead(entry, callOpts.caller, callOpts.parameters)
	if err != nil {
		return DeclaredQueryResult{}, err
	}
	compiled, err = r.applyDeclaredReadVisibility(compiled, callOpts.caller)
	if err != nil {
		return DeclaredQueryResult{}, err
	}
	result, err := protocol.ExecuteCompiledSQLQuery(ctx, compiled, committedStateAccess{state: state}, r.registry)
	if err != nil {
		return DeclaredQueryResult{}, err
	}
	return DeclaredQueryResult{
		Name:      entry.Name,
		TableName: result.TableName,
		Columns:   copyColumnSchemas(result.Columns),
		Rows:      types.CopyProductValues(result.Rows),
	}, nil
}

// SubscribeView admits an executable ViewDeclaration subscription by
// declaration name and returns its initial snapshot rows.
func (r *Runtime) SubscribeView(ctx context.Context, name string, queryID uint32, opts ...DeclaredReadOption) (DeclaredViewSubscription, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return DeclaredViewSubscription{}, err
	}
	exec, err := r.readyExecutor()
	if err != nil {
		return DeclaredViewSubscription{}, err
	}
	callOpts := r.applyDeclaredReadOptions(opts)
	entry, err := r.authorizedDeclaredRead(name, declaredReadKindView, callOpts.caller)
	if err != nil {
		return DeclaredViewSubscription{}, err
	}
	compiled, err := r.executableDeclaredRead(entry, callOpts.caller, callOpts.parameters)
	if err != nil {
		return DeclaredViewSubscription{}, err
	}
	compiled, err = r.applyDeclaredReadVisibility(compiled, callOpts.caller)
	if err != nil {
		return DeclaredViewSubscription{}, err
	}
	plan := newDeclaredViewAdmissionPlan(entry, compiled, callOpts.caller, r.registry)
	responseCh := make(chan declaredViewRegisterResponse, 1)
	cmd := executor.RegisterSubscriptionSetCmd{
		Request: plan.subscriptionSetRegisterRequest(ctx, callOpts.caller.ConnectionID, queryID, callOpts.requestID),
		Reply: func(result subscription.SubscriptionSetRegisterResult, err error) {
			responseCh <- declaredViewRegisterResponse{result: result, err: err}
		},
	}
	if err := exec.SubmitWithContext(ctx, cmd); err != nil {
		return DeclaredViewSubscription{}, err
	}
	select {
	case response := <-responseCh:
		if response.err != nil {
			return DeclaredViewSubscription{}, response.err
		}
		return DeclaredViewSubscription{
			Name:        entry.Name,
			QueryID:     queryID,
			RequestID:   callOpts.requestID,
			TableName:   declaredViewTableName(plan, response.result.Update),
			Columns:     declaredViewColumns(plan, response.result.Update, r.registry),
			InitialRows: collectDeclaredInitialRows(response.result.Update),
		}, nil
	case <-ctx.Done():
		return DeclaredViewSubscription{}, ctx.Err()
	}
}

type declaredViewRegisterResponse struct {
	result subscription.SubscriptionSetRegisterResult
	err    error
}

type declaredViewAdmissionPlan struct {
	name                  string
	sqlText               string
	tableName             string
	predicate             subscription.Predicate
	projection            []subscription.ProjectionColumn
	aggregate             *subscription.Aggregate
	orderBy               []subscription.OrderByColumn
	limit                 *uint64
	offset                *uint64
	predicateHashIdentity *types.Identity
	referencedTables      []schema.TableID
	usesCallerIdentity    bool
	relations             []declaredViewAdmissionRelation
	joinConditions        []declaredViewAdmissionJoinCondition
	projectedRelation     int
}

type declaredViewAdmissionRelation struct {
	relation int
	table    schema.TableID
	alias    uint8
}

type declaredViewAdmissionColumnRef struct {
	relation int
	table    schema.TableID
	column   types.ColID
	alias    uint8
	indexed  bool
}

type declaredViewAdmissionJoinCondition struct {
	left  declaredViewAdmissionColumnRef
	right declaredViewAdmissionColumnRef
}

func newDeclaredViewAdmissionPlan(entry declaredReadEntry, compiled protocol.CompiledSQLQuery, caller types.CallerContext, sl schema.SchemaLookup) declaredViewAdmissionPlan {
	relations, joinConditions := declaredViewAdmissionJoinGraph(compiled.Predicate(), sl)
	return declaredViewAdmissionPlan{
		name:                  entry.Name,
		sqlText:               entry.SQL,
		tableName:             compiled.TableName(),
		predicate:             compiled.Predicate(),
		projection:            compiled.SubscriptionProjection(),
		aggregate:             compiled.SubscriptionAggregate(),
		orderBy:               compiled.SubscriptionOrderBy(),
		limit:                 compiled.SubscriptionLimit(),
		offset:                compiled.SubscriptionOffset(),
		predicateHashIdentity: compiled.PredicateHashIdentity(caller.Identity),
		referencedTables:      compiled.ReferencedTables(),
		usesCallerIdentity:    compiled.UsesCallerIdentity(),
		relations:             relations,
		joinConditions:        joinConditions,
		projectedRelation:     declaredViewAdmissionProjectedRelation(compiled.Predicate()),
	}
}

func declaredViewAdmissionProjectedRelation(pred subscription.Predicate) int {
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

func declaredViewAdmissionJoinGraph(pred subscription.Predicate, sl schema.SchemaLookup) ([]declaredViewAdmissionRelation, []declaredViewAdmissionJoinCondition) {
	if pred == nil {
		return nil, nil
	}
	switch p := pred.(type) {
	case subscription.Join:
		return []declaredViewAdmissionRelation{
				{relation: 0, table: p.Left, alias: p.LeftAlias},
				{relation: 1, table: p.Right, alias: p.RightAlias},
			}, []declaredViewAdmissionJoinCondition{{
				left: declaredViewAdmissionColumnRef{
					relation: 0,
					table:    p.Left,
					column:   p.LeftCol,
					alias:    p.LeftAlias,
					indexed:  sl != nil && sl.HasIndex(p.Left, p.LeftCol),
				},
				right: declaredViewAdmissionColumnRef{
					relation: 1,
					table:    p.Right,
					column:   p.RightCol,
					alias:    p.RightAlias,
					indexed:  sl != nil && sl.HasIndex(p.Right, p.RightCol),
				},
			}}
	case subscription.CrossJoin:
		return []declaredViewAdmissionRelation{
			{relation: 0, table: p.Left, alias: p.LeftAlias},
			{relation: 1, table: p.Right, alias: p.RightAlias},
		}, nil
	case subscription.MultiJoin:
		relations := make([]declaredViewAdmissionRelation, len(p.Relations))
		for i, relation := range p.Relations {
			relations[i] = declaredViewAdmissionRelation{
				relation: i,
				table:    relation.Table,
				alias:    relation.Alias,
			}
		}
		conditions := make([]declaredViewAdmissionJoinCondition, len(p.Conditions))
		for i, condition := range p.Conditions {
			conditions[i] = declaredViewAdmissionJoinCondition{
				left:  declaredViewAdmissionColumnRefFromMultiJoin(condition.Left, sl),
				right: declaredViewAdmissionColumnRefFromMultiJoin(condition.Right, sl),
			}
		}
		return relations, conditions
	default:
		tables := pred.Tables()
		relations := make([]declaredViewAdmissionRelation, len(tables))
		for i, table := range tables {
			relations[i] = declaredViewAdmissionRelation{
				relation: i,
				table:    table,
			}
		}
		return relations, nil
	}
}

func declaredViewAdmissionColumnRefFromMultiJoin(ref subscription.MultiJoinColumnRef, sl schema.SchemaLookup) declaredViewAdmissionColumnRef {
	return declaredViewAdmissionColumnRef{
		relation: ref.Relation,
		table:    ref.Table,
		column:   ref.Column,
		alias:    ref.Alias,
		indexed:  sl != nil && sl.HasIndex(ref.Table, ref.Column),
	}
}

func (p declaredViewAdmissionPlan) subscriptionSetRegisterRequest(ctx context.Context, connID types.ConnectionID, queryID, requestID uint32) subscription.SubscriptionSetRegisterRequest {
	return subscription.SubscriptionSetRegisterRequest{
		Context:                 ctx,
		ConnID:                  connID,
		QueryID:                 queryID,
		RequestID:               requestID,
		Predicates:              []subscription.Predicate{p.predicate},
		ProjectionColumns:       [][]subscription.ProjectionColumn{p.projection},
		Aggregates:              []*subscription.Aggregate{p.aggregate},
		OrderByColumns:          [][]subscription.OrderByColumn{p.orderBy},
		Limits:                  []*uint64{p.limit},
		Offsets:                 []*uint64{p.offset},
		PredicateHashIdentities: []*types.Identity{p.predicateHashIdentity},
		SQLText:                 p.sqlText,
	}
}

func (r *Runtime) applyDeclaredReadOptions(opts []DeclaredReadOption) declaredReadOptions {
	out := declaredReadOptions{
		caller: types.CallerContext{Identity: defaultLocalIdentity},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.caller.Identity.IsZero() {
		out.caller.Identity = defaultLocalIdentity
	}
	if !out.permissionsSet && r.buildConfig.AuthMode == AuthModeDev {
		out.caller.AllowAllPermissions = true
	}
	return out
}

func (r *Runtime) authorizedDeclaredRead(name string, kind declaredReadKind, caller types.CallerContext) (declaredReadEntry, error) {
	entry, ok := r.readCatalog.lookup(name)
	if !ok || entry.Kind != kind {
		return declaredReadEntry{}, fmt.Errorf("%w: %s %q", ErrUnknownDeclaredRead, kind, name)
	}
	if missing, ok := types.MissingRequiredPermission(caller, entry.Permissions.Required); ok {
		return declaredReadEntry{}, fmt.Errorf("%w: declared %s %q requires %q", ErrPermissionDenied, kind, name, missing)
	}
	return entry, nil
}

func (r *Runtime) executableDeclaredRead(entry declaredReadEntry, caller types.CallerContext, parameters *types.ProductValue) (protocol.CompiledSQLQuery, error) {
	if entry.compiled == nil && entry.template == nil {
		return protocol.CompiledSQLQuery{}, fmt.Errorf("%w: %s %q", ErrDeclaredReadNotExecutable, entry.Kind, entry.Name)
	}
	bindings, err := declaredReadParameterBindings(entry, parameters)
	if err != nil {
		return protocol.CompiledSQLQuery{}, err
	}
	if len(bindings) != 0 {
		return protocol.CompileSQLQueryStringWithParameters(entry.SQL, r.registry, &caller.Identity, validationOptionsForDeclaredRead(entry.Kind), bindings)
	}
	if entry.UsesCallerIdentity {
		return protocol.CompileSQLQueryString(entry.SQL, r.registry, &caller.Identity, validationOptionsForDeclaredRead(entry.Kind))
	}
	return entry.compiled.Copy(), nil
}

func declaredReadParameterBindings(entry declaredReadEntry, supplied *types.ProductValue) ([]protocol.SQLQueryParameterValue, error) {
	if !declaredReadHasAppParameters(entry.Parameters) {
		if supplied != nil {
			return nil, declaredReadParameterErrorf("shunter: declared %s %q does not accept parameters", entry.Kind, entry.Name)
		}
		return nil, nil
	}
	if supplied == nil {
		return nil, declaredReadParameterErrorf("shunter: declared %s %q requires %d parameter(s)", entry.Kind, entry.Name, len(entry.Parameters.Columns))
	}
	values := *supplied
	if len(values) != len(entry.Parameters.Columns) {
		return nil, declaredReadParameterErrorf("shunter: declared %s %q parameter arity = %d, want %d", entry.Kind, entry.Name, len(values), len(entry.Parameters.Columns))
	}
	bindings := make([]protocol.SQLQueryParameterValue, len(entry.Parameters.Columns))
	for i, parameter := range entry.Parameters.Columns {
		kind, ok := valueKindFromExportString(parameter.Type)
		if !ok {
			return nil, fmt.Errorf("shunter: declared %s %q parameter %d %q has invalid type %q", entry.Kind, entry.Name, i, parameter.Name, parameter.Type)
		}
		value := values[i]
		if value.Kind() != kind {
			return nil, declaredReadParameterErrorf("shunter: declared %s %q parameter %d %q type = %s, want %s", entry.Kind, entry.Name, i, parameter.Name, value.Kind(), kind)
		}
		if value.IsNull() && !parameter.Nullable {
			return nil, declaredReadParameterErrorf("shunter: declared %s %q parameter %d %q is null but not nullable", entry.Kind, entry.Name, i, parameter.Name)
		}
		bindings[i] = protocol.SQLQueryParameterValue{Name: parameter.Name, Value: value}
	}
	return bindings, nil
}

func (r *Runtime) applyDeclaredReadVisibility(compiled protocol.CompiledSQLQuery, caller types.CallerContext) (protocol.CompiledSQLQuery, error) {
	return protocol.ApplyVisibilityFilters(
		compiled,
		r.registry,
		&caller.Identity,
		runtimeProtocolVisibilityFilters(r.module.visibilityFilters),
		caller.AllowAllPermissions,
	)
}

func validationOptionsForDeclaredRead(kind declaredReadKind) protocol.SQLQueryValidationOptions {
	if kind == declaredReadKindView {
		return protocol.SQLQueryValidationOptions{AllowLimit: true, AllowProjection: true, AllowOrderBy: true, AllowOffset: true}
	}
	return protocol.SQLQueryValidationOptions{AllowLimit: true, AllowProjection: true, AllowOrderBy: true, AllowOffset: true}
}

func declaredViewTableName(plan declaredViewAdmissionPlan, updates []subscription.SubscriptionUpdate) string {
	for _, update := range updates {
		if update.TableName != "" {
			return update.TableName
		}
	}
	return plan.tableName
}

func collectDeclaredInitialRows(updates []subscription.SubscriptionUpdate) []types.ProductValue {
	var rows []types.ProductValue
	for _, update := range updates {
		rows = append(rows, update.Inserts...)
	}
	return types.CopyProductValues(rows)
}

func declaredViewColumns(plan declaredViewAdmissionPlan, updates []subscription.SubscriptionUpdate, sl schema.SchemaLookup) []schema.ColumnSchema {
	for _, update := range updates {
		if len(update.Columns) != 0 {
			return copyColumnSchemas(update.Columns)
		}
	}
	if len(plan.projection) != 0 {
		columns := make([]schema.ColumnSchema, len(plan.projection))
		for i, col := range plan.projection {
			columns[i] = col.Schema
		}
		return columns
	}
	if plan.aggregate != nil {
		return []schema.ColumnSchema{plan.aggregate.ResultColumn}
	}
	if sl != nil {
		if _, ts, ok := sl.TableByName(plan.tableName); ok && ts != nil {
			return copyColumnSchemas(ts.Columns)
		}
	}
	return nil
}

// HandleDeclaredQuery handles the protocol named-query message by delegating to
// the runtime-owned declared read API.
func (r *Runtime) HandleDeclaredQuery(ctx context.Context, conn *protocol.Conn, msg *protocol.DeclaredQueryMsg) {
	r.handleProtocolDeclaredQuery(ctx, conn, msg.MessageID, msg.Name, nil)
}

// HandleDeclaredQueryWithParameters handles the v2 protocol named-query message
// with BSATN-encoded ordered declared-read parameters.
func (r *Runtime) HandleDeclaredQueryWithParameters(ctx context.Context, conn *protocol.Conn, msg *protocol.DeclaredQueryWithParametersMsg) {
	receipt := time.Now()
	parameters, err := r.decodeProtocolDeclaredReadParameters(msg.Name, declaredReadKindQuery, conn, msg.Params)
	if err != nil {
		r.sendProtocolDeclaredQueryError(conn, msg.MessageID, err.Error(), receipt)
		r.observability.RecordProtocolMessage("declared_query", protocolDeclaredReadMetricResult(err))
		return
	}
	r.handleProtocolDeclaredQuery(ctx, conn, msg.MessageID, msg.Name, &parameters)
}

func (r *Runtime) handleProtocolDeclaredQuery(ctx context.Context, conn *protocol.Conn, messageID []byte, name string, parameters *types.ProductValue) {
	receipt := time.Now()
	opts := protocolDeclaredReadOptions(conn, nil)
	if parameters != nil {
		opts = append(opts, WithDeclaredReadParameters(*parameters))
	}
	result, err := r.CallQuery(ctx, name, opts...)
	if err != nil {
		r.sendProtocolDeclaredQueryError(conn, messageID, err.Error(), receipt)
		r.observability.RecordProtocolMessage("declared_query", protocolDeclaredReadMetricResult(err))
		return
	}
	rows, err := encodeDeclaredReadRows(result.Rows, result.Columns)
	if err != nil {
		r.sendProtocolDeclaredQueryError(conn, messageID, "encode error: "+err.Error(), receipt)
		r.observability.RecordProtocolMessage("declared_query", "internal_error")
		return
	}
	if err := r.sendProtocolDeclaredReadMessage(conn, protocol.OneOffQueryResponse{
		MessageID: messageID,
		Tables: []protocol.OneOffTable{{
			TableName: result.TableName,
			Rows:      rows,
		}},
		TotalHostExecutionDuration: declaredReadElapsedMicrosI64(receipt),
	}); err != nil {
		r.logProtocolDeclaredReadSendError("declared_query", err)
		r.observability.RecordProtocolMessage("declared_query", "connection_closed")
		return
	}
	r.observability.RecordProtocolMessage("declared_query", "ok")
}

// HandleSubscribeDeclaredView handles the protocol named-view subscription
// message by delegating to the runtime-owned declared read API.
func (r *Runtime) HandleSubscribeDeclaredView(ctx context.Context, conn *protocol.Conn, msg *protocol.SubscribeDeclaredViewMsg) {
	r.handleProtocolSubscribeDeclaredView(ctx, conn, msg.RequestID, msg.QueryID, msg.Name, nil)
}

// HandleSubscribeDeclaredViewWithParameters handles the v2 protocol named-view
// subscription message with BSATN-encoded ordered declared-read parameters.
func (r *Runtime) HandleSubscribeDeclaredViewWithParameters(ctx context.Context, conn *protocol.Conn, msg *protocol.SubscribeDeclaredViewWithParametersMsg) {
	receipt := time.Now()
	parameters, err := r.decodeProtocolDeclaredReadParameters(msg.Name, declaredReadKindView, conn, msg.Params)
	if err != nil {
		r.sendProtocolDeclaredViewError(conn, msg.RequestID, msg.QueryID, err.Error(), receipt)
		r.observability.RecordProtocolMessage("subscribe_declared_view", protocolDeclaredReadMetricResult(err))
		return
	}
	r.handleProtocolSubscribeDeclaredView(ctx, conn, msg.RequestID, msg.QueryID, msg.Name, &parameters)
}

func (r *Runtime) handleProtocolSubscribeDeclaredView(ctx context.Context, conn *protocol.Conn, requestID, queryID uint32, name string, parameters *types.ProductValue) {
	receipt := time.Now()
	opts := protocolDeclaredReadOptions(conn, &requestID)
	if parameters != nil {
		opts = append(opts, WithDeclaredReadParameters(*parameters))
	}
	sub, err := r.SubscribeView(ctx, name, queryID, opts...)
	if err != nil {
		r.sendProtocolDeclaredViewError(conn, requestID, queryID, err.Error(), receipt)
		r.observability.RecordProtocolMessage("subscribe_declared_view", protocolDeclaredReadMetricResult(err))
		return
	}
	rows, err := encodeDeclaredReadRows(sub.InitialRows, sub.Columns)
	if err != nil {
		r.sendProtocolDeclaredViewError(conn, requestID, queryID, "encode error: "+err.Error(), receipt)
		r.observability.RecordProtocolMessage("subscribe_declared_view", "internal_error")
		return
	}
	if err := r.sendProtocolDeclaredReadMessage(conn, protocol.SubscribeSingleApplied{
		RequestID:                        requestID,
		TotalHostExecutionDurationMicros: declaredReadElapsedMicrosU64(receipt),
		QueryID:                          queryID,
		TableName:                        sub.TableName,
		Rows:                             rows,
	}); err != nil {
		r.logProtocolDeclaredReadSendError("subscribe_declared_view", err)
		r.observability.RecordProtocolMessage("subscribe_declared_view", "connection_closed")
		return
	}
	r.observability.RecordProtocolMessage("subscribe_declared_view", "ok")
}

func (r *Runtime) decodeProtocolDeclaredReadParameters(name string, kind declaredReadKind, conn *protocol.Conn, data []byte) (types.ProductValue, error) {
	opts := r.applyDeclaredReadOptions(protocolDeclaredReadOptions(conn, nil))
	entry, err := r.authorizedDeclaredRead(name, kind, opts.caller)
	if err != nil {
		return nil, err
	}
	if !declaredReadHasAppParameters(entry.Parameters) {
		return nil, declaredReadParameterErrorf("shunter: declared %s %q does not accept parameters", entry.Kind, entry.Name)
	}
	columns, err := declaredReadParameterColumnSchemas(entry.Parameters)
	if err != nil {
		return nil, err
	}
	values, err := bsatn.DecodeProductValueFromBytes(data, &schema.TableSchema{Name: entry.Name, Columns: columns})
	if err != nil {
		if nullableValues, ok := decodeDeclaredReadProtocolNullableFallback(data, entry.Name, columns); ok {
			if _, validationErr := declaredReadParameterBindings(entry, &nullableValues); validationErr != nil {
				return nil, validationErr
			}
			return nullableValues, nil
		}
		return nil, declaredReadParameterErrorf("shunter: declared %s %q parameter decode failed: %v", entry.Kind, entry.Name, err)
	}
	if _, err := declaredReadParameterBindings(entry, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func decodeDeclaredReadProtocolNullableFallback(data []byte, name string, columns []schema.ColumnSchema) (types.ProductValue, bool) {
	if len(columns) == 0 {
		return nil, false
	}
	nullableColumns := make([]schema.ColumnSchema, len(columns))
	copy(nullableColumns, columns)
	for i := range nullableColumns {
		nullableColumns[i].Nullable = true
	}
	values, err := bsatn.DecodeProductValueFromBytes(data, &schema.TableSchema{Name: name, Columns: nullableColumns})
	if err != nil {
		return nil, false
	}
	return values, true
}

func declaredReadParameterColumnSchemas(parameters *ProductSchema) ([]schema.ColumnSchema, error) {
	if !declaredReadHasAppParameters(parameters) {
		return nil, nil
	}
	columns := make([]schema.ColumnSchema, len(parameters.Columns))
	for i, parameter := range parameters.Columns {
		kind, ok := valueKindFromExportString(parameter.Type)
		if !ok {
			return nil, fmt.Errorf("shunter: declared read parameter %d %q has invalid type %q", i, parameter.Name, parameter.Type)
		}
		columns[i] = schema.ColumnSchema{
			Index:    i,
			Name:     parameter.Name,
			Type:     kind,
			Nullable: parameter.Nullable,
		}
	}
	return columns, nil
}

func protocolDeclaredReadOptions(conn *protocol.Conn, requestID *uint32) []DeclaredReadOption {
	opts := []DeclaredReadOption{}
	if conn != nil {
		opts = append(opts,
			WithDeclaredReadIdentity(conn.Identity),
			WithDeclaredReadConnectionID(conn.ID),
			WithDeclaredReadAuthPrincipal(conn.Principal),
		)
		if conn.AllowAllPermissions {
			opts = append(opts, WithDeclaredReadAllowAllPermissions())
		} else {
			opts = append(opts, WithDeclaredReadPermissions(conn.Permissions...))
		}
	} else {
		opts = append(opts, WithDeclaredReadPermissions())
	}
	if requestID != nil {
		opts = append(opts, WithDeclaredReadRequestID(*requestID))
	}
	return opts
}

func (r *Runtime) sendProtocolDeclaredQueryError(conn *protocol.Conn, messageID []byte, errText string, receipt time.Time) {
	if err := r.sendProtocolDeclaredReadMessage(conn, protocol.OneOffQueryResponse{
		MessageID:                  messageID,
		Error:                      &errText,
		TotalHostExecutionDuration: declaredReadElapsedMicrosI64(receipt),
	}); err != nil {
		r.logProtocolDeclaredReadSendError("declared_query", err)
	}
}

func (r *Runtime) sendProtocolDeclaredViewError(conn *protocol.Conn, requestID, queryID uint32, errText string, receipt time.Time) {
	if err := r.sendProtocolDeclaredReadMessage(conn, protocol.SubscriptionError{
		TotalHostExecutionDurationMicros: declaredReadElapsedMicrosU64(receipt),
		RequestID:                        declaredReadOptionalUint32(requestID),
		QueryID:                          declaredReadOptionalUint32(queryID),
		Error:                            errText,
	}); err != nil {
		r.logProtocolDeclaredReadSendError("subscribe_declared_view", err)
	}
}

func (r *Runtime) sendProtocolDeclaredReadMessage(conn *protocol.Conn, msg any) error {
	if conn == nil {
		return nil
	}
	sender, err := r.readyProtocolSender()
	if err != nil {
		return err
	}
	return sender.Send(conn.ID, msg)
}

func (r *Runtime) readyProtocolSender() (protocol.ClientSender, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.readyLocked(); err != nil {
		return nil, err
	}
	if r.protocolSender == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.protocolSender, nil
}

func (r *Runtime) logProtocolDeclaredReadSendError(kind string, err error) {
	r.observability.LogProtocolProtocolError(kind, "send_failed", err)
}

func encodeDeclaredReadRows(rows []types.ProductValue, columns []schema.ColumnSchema) ([]byte, error) {
	if len(columns) != 0 {
		return protocol.EncodeProductRowsForColumns(rows, columns)
	}
	return protocol.EncodeProductRows(rows)
}

func copyColumnSchemas(in []schema.ColumnSchema) []schema.ColumnSchema {
	if len(in) == 0 {
		return nil
	}
	out := make([]schema.ColumnSchema, len(in))
	copy(out, in)
	return out
}

func declaredReadElapsedMicrosI64(receipt time.Time) int64 {
	us := time.Since(receipt).Microseconds()
	if us <= 0 {
		return 1
	}
	return us
}

func declaredReadElapsedMicrosU64(receipt time.Time) uint64 {
	return uint64(declaredReadElapsedMicrosI64(receipt))
}

func declaredReadOptionalUint32(v uint32) *uint32 {
	return &v
}

func protocolDeclaredReadMetricResult(err error) string {
	if errors.Is(err, ErrPermissionDenied) {
		return "permission_denied"
	}
	if errors.Is(err, ErrUnknownDeclaredRead) || errors.Is(err, ErrDeclaredReadNotExecutable) {
		return "validation_error"
	}
	if errors.Is(err, errDeclaredReadParameter) {
		return "validation_error"
	}
	return "internal_error"
}

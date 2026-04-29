package shunter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
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
)

// DeclaredQueryResult is the detached row result returned by CallQuery.
type DeclaredQueryResult struct {
	Name      string
	TableName string
	Rows      []types.ProductValue
}

// DeclaredViewSubscription is the local admission result returned by
// SubscribeView.
type DeclaredViewSubscription struct {
	Name        string
	QueryID     uint32
	RequestID   uint32
	TableName   string
	InitialRows []types.ProductValue
}

// DeclaredReadOption configures local named query/view calls.
type DeclaredReadOption func(*declaredReadOptions)

type declaredReadOptions struct {
	caller         types.CallerContext
	requestID      uint32
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
	compiled, err := r.executableDeclaredRead(entry, callOpts.caller)
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
		Rows:      copyRuntimeProductRows(result.Rows),
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
	compiled, err := r.executableDeclaredRead(entry, callOpts.caller)
	if err != nil {
		return DeclaredViewSubscription{}, err
	}
	compiled, err = r.applyDeclaredReadVisibility(compiled, callOpts.caller)
	if err != nil {
		return DeclaredViewSubscription{}, err
	}
	responseCh := make(chan declaredViewRegisterResponse, 1)
	cmd := executor.RegisterSubscriptionSetCmd{
		Request: subscription.SubscriptionSetRegisterRequest{
			Context:                 ctx,
			ConnID:                  callOpts.caller.ConnectionID,
			QueryID:                 queryID,
			RequestID:               callOpts.requestID,
			Predicates:              []subscription.Predicate{compiled.Predicate()},
			PredicateHashIdentities: []*types.Identity{compiled.PredicateHashIdentity(callOpts.caller.Identity)},
			SQLText:                 entry.SQL,
		},
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
			TableName:   declaredViewTableName(compiled, response.result.Update),
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

func (r *Runtime) executableDeclaredRead(entry declaredReadEntry, caller types.CallerContext) (protocol.CompiledSQLQuery, error) {
	if entry.compiled == nil {
		return protocol.CompiledSQLQuery{}, fmt.Errorf("%w: %s %q", ErrDeclaredReadNotExecutable, entry.Kind, entry.Name)
	}
	if entry.UsesCallerIdentity {
		return protocol.CompileSQLQueryString(entry.SQL, r.registry, &caller.Identity, validationOptionsForDeclaredRead(entry.Kind))
	}
	return entry.compiled.Copy(), nil
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
		return protocol.SQLQueryValidationOptions{AllowLimit: false, AllowProjection: false}
	}
	return protocol.SQLQueryValidationOptions{AllowLimit: true, AllowProjection: true}
}

func declaredViewTableName(compiled protocol.CompiledSQLQuery, updates []subscription.SubscriptionUpdate) string {
	for _, update := range updates {
		if update.TableName != "" {
			return update.TableName
		}
	}
	return compiled.TableName()
}

func collectDeclaredInitialRows(updates []subscription.SubscriptionUpdate) []types.ProductValue {
	var rows []types.ProductValue
	for _, update := range updates {
		rows = append(rows, update.Inserts...)
	}
	return copyRuntimeProductRows(rows)
}

// HandleDeclaredQuery handles the protocol named-query message by delegating to
// the runtime-owned declared read API.
func (r *Runtime) HandleDeclaredQuery(ctx context.Context, conn *protocol.Conn, msg *protocol.DeclaredQueryMsg) {
	receipt := time.Now()
	result, err := r.CallQuery(ctx, msg.Name, protocolDeclaredReadOptions(conn, nil)...)
	if err != nil {
		sendProtocolDeclaredQueryError(conn, msg.MessageID, err.Error(), receipt)
		return
	}
	rows, err := encodeDeclaredReadRows(result.Rows)
	if err != nil {
		sendProtocolDeclaredQueryError(conn, msg.MessageID, "encode error: "+err.Error(), receipt)
		return
	}
	sendProtocolDeclaredReadMessage(conn, protocol.OneOffQueryResponse{
		MessageID: msg.MessageID,
		Tables: []protocol.OneOffTable{{
			TableName: result.TableName,
			Rows:      rows,
		}},
		TotalHostExecutionDuration: declaredReadElapsedMicrosI64(receipt),
	})
}

// HandleSubscribeDeclaredView handles the protocol named-view subscription
// message by delegating to the runtime-owned declared read API.
func (r *Runtime) HandleSubscribeDeclaredView(ctx context.Context, conn *protocol.Conn, msg *protocol.SubscribeDeclaredViewMsg) {
	receipt := time.Now()
	sub, err := r.SubscribeView(ctx, msg.Name, msg.QueryID, protocolDeclaredReadOptions(conn, &msg.RequestID)...)
	if err != nil {
		sendProtocolDeclaredViewError(conn, msg.RequestID, msg.QueryID, err.Error(), receipt)
		return
	}
	rows, err := encodeDeclaredReadRows(sub.InitialRows)
	if err != nil {
		sendProtocolDeclaredViewError(conn, msg.RequestID, msg.QueryID, "encode error: "+err.Error(), receipt)
		return
	}
	sendProtocolDeclaredReadMessage(conn, protocol.SubscribeSingleApplied{
		RequestID:                        msg.RequestID,
		TotalHostExecutionDurationMicros: declaredReadElapsedMicrosU64(receipt),
		QueryID:                          msg.QueryID,
		TableName:                        sub.TableName,
		Rows:                             rows,
	})
}

func protocolDeclaredReadOptions(conn *protocol.Conn, requestID *uint32) []DeclaredReadOption {
	opts := []DeclaredReadOption{}
	if conn != nil {
		opts = append(opts,
			WithDeclaredReadIdentity(conn.Identity),
			WithDeclaredReadConnectionID(conn.ID),
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

func sendProtocolDeclaredQueryError(conn *protocol.Conn, messageID []byte, errText string, receipt time.Time) {
	sendProtocolDeclaredReadMessage(conn, protocol.OneOffQueryResponse{
		MessageID:                  messageID,
		Error:                      &errText,
		TotalHostExecutionDuration: declaredReadElapsedMicrosI64(receipt),
	})
}

func sendProtocolDeclaredViewError(conn *protocol.Conn, requestID, queryID uint32, errText string, receipt time.Time) {
	sendProtocolDeclaredReadMessage(conn, protocol.SubscriptionError{
		TotalHostExecutionDurationMicros: declaredReadElapsedMicrosU64(receipt),
		RequestID:                        declaredReadOptionalUint32(requestID),
		QueryID:                          declaredReadOptionalUint32(queryID),
		Error:                            errText,
	})
}

func sendProtocolDeclaredReadMessage(conn *protocol.Conn, msg any) {
	if conn == nil {
		return
	}
	frame, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		return
	}
	wrapped := protocol.EncodeFrame(frame[0], frame[1:], conn.Compression, protocol.CompressionNone)
	select {
	case conn.OutboundCh <- wrapped:
	default:
	}
}

func encodeDeclaredReadRows(rows []types.ProductValue) ([]byte, error) {
	encodedRows := make([][]byte, len(rows))
	for i, row := range rows {
		var buf bytes.Buffer
		if err := bsatn.EncodeProductValue(&buf, row); err != nil {
			return nil, err
		}
		encodedRows[i] = buf.Bytes()
	}
	return protocol.EncodeRowList(encodedRows), nil
}

func declaredReadElapsedMicrosI64(receipt time.Time) int64 {
	us := time.Since(receipt).Microseconds()
	if us <= 0 {
		return 1
	}
	return us
}

func declaredReadElapsedMicrosU64(receipt time.Time) uint64 {
	us := uint64(time.Since(receipt).Microseconds())
	if us == 0 {
		return 1
	}
	return us
}

func declaredReadOptionalUint32(v uint32) *uint32 {
	return &v
}

func copyRuntimeProductRows(rows []types.ProductValue) []types.ProductValue {
	if len(rows) == 0 {
		return nil
	}
	out := make([]types.ProductValue, len(rows))
	for i, row := range rows {
		out[i] = row.Copy()
	}
	return out
}

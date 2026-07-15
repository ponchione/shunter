package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type protocolCommandSubmitter interface {
	SubmitWithContext(ctx context.Context, cmd ExecutorCommand) error
}

type protocolTableLookup interface {
	Table(schema.TableID) (*schema.TableSchema, bool)
}

// ProtocolInboxAdapter is the concrete production bridge that satisfies
// protocol.ExecutorInbox by translating protocol-layer requests into executor
// commands.
type ProtocolInboxAdapter struct {
	submitter protocolCommandSubmitter
	schemaReg protocolTableLookup
}

var _ protocol.ExecutorInbox = (*ProtocolInboxAdapter)(nil)

func NewProtocolInboxAdapter(exec *Executor) *ProtocolInboxAdapter {
	return newProtocolInboxAdapter(exec, exec.schemaReg)
}

func newProtocolInboxAdapter(submitter protocolCommandSubmitter, schemaReg protocolTableLookup) *ProtocolInboxAdapter {
	return &ProtocolInboxAdapter{submitter: submitter, schemaReg: schemaReg}
}

func (a *ProtocolInboxAdapter) OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity, principal types.AuthPrincipal) error {
	respCh := make(chan ReducerResponse, 1)
	if err := a.submitter.SubmitWithContext(ctx, OnConnectCmd{ConnID: connID, Identity: identity, Principal: principal.Copy(), ResponseCh: respCh}); err != nil {
		return err
	}
	return awaitReducerStatus(ctx, respCh, "OnConnect")
}

func (a *ProtocolInboxAdapter) OnDisconnect(ctx context.Context, connID types.ConnectionID, identity types.Identity, principal types.AuthPrincipal) error {
	respCh := make(chan ReducerResponse, 1)
	if err := a.submitter.SubmitWithContext(ctx, OnDisconnectCmd{ConnID: connID, Identity: identity, Principal: principal.Copy(), ResponseCh: respCh}); err != nil {
		return err
	}
	return awaitReducerStatus(ctx, respCh, "OnDisconnect")
}

func (a *ProtocolInboxAdapter) DisconnectClientSubscriptions(ctx context.Context, connID types.ConnectionID) error {
	respCh := make(chan error, 1)
	if err := a.submitter.SubmitWithContext(ctx, DisconnectClientSubscriptionsCmd{ConnID: connID, ResponseCh: respCh}); err != nil {
		return err
	}
	select {
	case err := <-respCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *ProtocolInboxAdapter) RegisterSubscriptionSet(ctx context.Context, req protocol.RegisterSubscriptionSetRequest) error {
	preds, err := castPredicates(req.Predicates)
	if err != nil {
		return err
	}
	if err := validateVariant(req.Variant); err != nil {
		return err
	}
	var prepared *protocol.SubscriptionSetCommandResponse
	cmd := RegisterSubscriptionSetCmd{
		Request: subscription.SubscriptionSetRegisterRequest{
			Context:                 ctx,
			ConnID:                  req.ConnID,
			QueryID:                 req.QueryID,
			RequestID:               req.RequestID,
			Predicates:              preds,
			PredicateHashIdentities: req.PredicateHashIdentities,
			SQLText:                 req.SQLText,
			PrepareSnapshot: func(updates []subscription.SubscriptionUpdate) error {
				resp, err := a.prepareRegisterResponse(req, preds, updates)
				if err != nil {
					return err
				}
				prepared = &resp
				return nil
			},
		},
		Reply: func(result subscription.SubscriptionSetRegisterResult, replyErr error) {
			if req.Reply == nil {
				return
			}
			if replyErr == nil && prepared != nil {
				setRegisterResponseDuration(prepared, result.TotalHostExecutionDurationMicros)
				req.Reply(*prepared)
				return
			}
			req.Reply(a.buildRegisterResponse(req, preds, result, replyErr))
		},
		Receipt: req.Receipt,
	}
	return a.submitter.SubmitWithContext(ctx, cmd)
}

func (a *ProtocolInboxAdapter) UnregisterSubscriptionSet(ctx context.Context, req protocol.UnregisterSubscriptionSetRequest) error {
	if err := validateVariant(req.Variant); err != nil {
		return err
	}
	cmd := UnregisterSubscriptionSetCmd{
		ConnID:  req.ConnID,
		QueryID: req.QueryID,
		Reply: func(result subscription.SubscriptionSetUnregisterResult, replyErr error) {
			if req.Reply == nil {
				return
			}
			req.Reply(a.buildUnregisterResponse(req, result, replyErr))
		},
		Context: ctx,
		Receipt: req.Receipt,
	}
	return a.submitter.SubmitWithContext(ctx, cmd)
}

func (a *ProtocolInboxAdapter) CallReducer(ctx context.Context, req protocol.CallReducerRequest) error {
	respCh := make(chan ProtocolCallReducerResponse, 1)
	cmd := CallReducerCmd{
		Request: ReducerRequest{
			ReducerName: req.ReducerName,
			Args:        req.Args,
			Caller: types.CallerContext{
				Identity:            req.Identity,
				ConnectionID:        req.ConnID,
				Principal:           req.Principal.Copy(),
				Permissions:         append([]string(nil), req.Permissions...),
				AllowAllPermissions: req.AllowAllPermissions,
			},
			RequestID: req.RequestID,
			Source:    CallSourceExternal,
			Flags:     req.Flags,
		},
		ProtocolResponseCh: respCh,
	}
	if err := a.submitter.SubmitWithContext(ctx, cmd); err != nil {
		return err
	}
	if req.ResponseCh != nil {
		// Once admitted, keep the protocol handler in flight until the executor
		// has completed the command. Conn disconnect drains these handlers before
		// subscription cleanup and OnDisconnect, so an accepted reducer cannot
		// commit after the client lifecycle has already been torn down.
		resp, ok := <-respCh
		a.deliverReducerResponse(ctx, req, resp, ok)
	}
	return nil
}

func awaitReducerStatus(ctx context.Context, respCh <-chan ReducerResponse, op string) error {
	select {
	case resp := <-respCh:
		if resp.Status == StatusCommitted {
			return nil
		}
		if resp.Error != nil {
			return resp.Error
		}
		return fmt.Errorf("executor: %s returned status %d", op, resp.Status)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validateVariant(variant protocol.SubscriptionSetVariant) error {
	if variant == protocol.SubscriptionSetVariantSingle || variant == protocol.SubscriptionSetVariantMulti {
		return nil
	}
	return fmt.Errorf("protocol: unknown subscription-set variant %d", variant)
}

func castPredicates(raw []any) ([]subscription.Predicate, error) {
	preds := make([]subscription.Predicate, 0, len(raw))
	for i, item := range raw {
		pred, ok := item.(subscription.Predicate)
		if !ok {
			return nil, fmt.Errorf("protocol: predicate[%d] has type %T, want subscription.Predicate", i, item)
		}
		preds = append(preds, pred)
	}
	return preds, nil
}

func optionalUint32(v uint32) *uint32 {
	return &v
}

// Request-origin SubscriptionError always leaves table_id unset.
func (a *ProtocolInboxAdapter) buildRegisterResponse(
	req protocol.RegisterSubscriptionSetRequest,
	preds []subscription.Predicate,
	result subscription.SubscriptionSetRegisterResult,
	replyErr error,
) protocol.SubscriptionSetCommandResponse {
	if replyErr != nil {
		errText := replyErr.Error()
		switch {
		case errors.Is(replyErr, subscription.ErrSubscriptionQuota):
			// Quota rejections remain classifiable at the protocol boundary.
		case errors.Is(replyErr, subscription.ErrInitialQuery) && req.Variant == protocol.SubscriptionSetVariantMulti:
			// Reference `module_subscription_actor.rs:1383` substitutes
			// the underlying initial-eval error with a canned
			// "Internal error evaluating queries" message on the
			// SubscribeMulti path, discarding per-query detail. Match
			// that text exactly.
			errText = "Internal error evaluating queries"
		case errors.Is(replyErr, subscription.ErrInitialQuery) && req.SQLText != "":
			// Reference `DBError::WithSql` suffix for SubscribeSingle
			// initial-eval errors (`error.rs:140`,
			// `module_subscription_actor.rs:672` via
			// `return_on_err_with_sql_bool!`).
			errText = fmt.Sprintf("%s, executing: `%s`", errText, req.SQLText)
		}
		return protocol.SubscriptionSetCommandResponse{
			Error: newProtocolSubscriptionError(result.TotalHostExecutionDurationMicros, req.RequestID, req.QueryID, errText),
		}
	}
	resp, err := a.prepareRegisterResponse(req, preds, result.Update)
	if err != nil {
		return protocol.SubscriptionSetCommandResponse{
			Error: newProtocolSubscriptionError(result.TotalHostExecutionDurationMicros, req.RequestID, req.QueryID, err.Error()),
		}
	}
	setRegisterResponseDuration(&resp, result.TotalHostExecutionDurationMicros)
	return resp
}

func (a *ProtocolInboxAdapter) prepareRegisterResponse(
	req protocol.RegisterSubscriptionSetRequest,
	preds []subscription.Predicate,
	updates []subscription.SubscriptionUpdate,
) (protocol.SubscriptionSetCommandResponse, error) {
	if req.Variant == protocol.SubscriptionSetVariantMulti {
		wireUpdates, err := encodeProtocolSubscriptionUpdates(updates)
		if err != nil {
			return protocol.SubscriptionSetCommandResponse{}, err
		}
		applied := &protocol.SubscribeMultiApplied{RequestID: req.RequestID, QueryID: req.QueryID, Update: wireUpdates}
		if err := validatePreparedSubscriptionResponse(*applied, req.MaxResponseBytes); err != nil {
			return protocol.SubscriptionSetCommandResponse{}, err
		}
		return protocol.SubscriptionSetCommandResponse{MultiApplied: applied}, nil
	}
	rows, err := protocol.EncodeProductRows(collectInsertRows(updates))
	if err != nil {
		return protocol.SubscriptionSetCommandResponse{}, err
	}
	applied := &protocol.SubscribeSingleApplied{
		RequestID: req.RequestID,
		QueryID:   req.QueryID,
		TableName: a.singleTableName(preds, updates),
		Rows:      rows,
	}
	if err := validatePreparedSubscriptionResponse(*applied, req.MaxResponseBytes); err != nil {
		return protocol.SubscriptionSetCommandResponse{}, err
	}
	return protocol.SubscriptionSetCommandResponse{SingleApplied: applied}, nil
}

func validatePreparedSubscriptionResponse(msg any, maxBytes int) error {
	size, err := protocol.ValidateServerMessageSize(msg, maxBytes)
	if errors.Is(err, protocol.ErrOutboundMessageLimit) {
		return subscription.NewQuotaError(subscription.ErrSnapshotByteLimit, "snapshot_response_bytes", size, maxBytes)
	}
	return err
}

func setRegisterResponseDuration(resp *protocol.SubscriptionSetCommandResponse, duration uint64) {
	if resp == nil {
		return
	}
	if resp.SingleApplied != nil {
		resp.SingleApplied.TotalHostExecutionDurationMicros = duration
	}
	if resp.MultiApplied != nil {
		resp.MultiApplied.TotalHostExecutionDurationMicros = duration
	}
}

func (a *ProtocolInboxAdapter) buildUnregisterResponse(
	req protocol.UnregisterSubscriptionSetRequest,
	result subscription.SubscriptionSetUnregisterResult,
	replyErr error,
) protocol.UnsubscribeSetCommandResponse {
	if replyErr != nil {
		errText := replyErr.Error()
		// Reference `module_subscription_actor.rs:756` wraps the
		// UnsubscribeSingle final-eval error via
		// `return_on_err_with_sql!` (DBError::WithSql suffix). The
		// UnsubscribeMulti path at `:836` uses plain `return_on_err!`
		// and emits raw err text. Non-ErrFinalQuery errors (e.g.
		// admission-time `ErrSubscriptionNotFound`) are never wrapped.
		if errors.Is(replyErr, subscription.ErrFinalQuery) &&
			req.Variant == protocol.SubscriptionSetVariantSingle &&
			result.SQLText != "" {
			errText = fmt.Sprintf("%s, executing: `%s`", errText, result.SQLText)
		}
		return protocol.UnsubscribeSetCommandResponse{
			Error: newProtocolSubscriptionError(result.TotalHostExecutionDurationMicros, req.RequestID, req.QueryID, errText),
		}
	}
	updates, err := encodeProtocolSubscriptionUpdates(result.Update)
	if err != nil {
		return protocol.UnsubscribeSetCommandResponse{
			Error: newProtocolSubscriptionError(result.TotalHostExecutionDurationMicros, req.RequestID, req.QueryID, err.Error()),
		}
	}
	if req.Variant == protocol.SubscriptionSetVariantMulti {
		return protocol.UnsubscribeSetCommandResponse{
			MultiApplied: &protocol.UnsubscribeMultiApplied{RequestID: req.RequestID, QueryID: req.QueryID, Update: updates, TotalHostExecutionDurationMicros: result.TotalHostExecutionDurationMicros},
		}
	}
	rows, err := protocol.EncodeProductRows(collectDeleteRows(result.Update))
	if err != nil {
		return protocol.UnsubscribeSetCommandResponse{
			Error: newProtocolSubscriptionError(result.TotalHostExecutionDurationMicros, req.RequestID, req.QueryID, err.Error()),
		}
	}
	return protocol.UnsubscribeSetCommandResponse{
		SingleApplied: &protocol.UnsubscribeSingleApplied{
			RequestID:                        req.RequestID,
			QueryID:                          req.QueryID,
			HasRows:                          len(result.Update) > 0,
			Rows:                             rows,
			TotalHostExecutionDurationMicros: result.TotalHostExecutionDurationMicros,
		},
	}
}

func (a *ProtocolInboxAdapter) singleTableName(preds []subscription.Predicate, updates []subscription.SubscriptionUpdate) string {
	for _, update := range updates {
		if update.TableName != "" {
			return update.TableName
		}
	}
	if len(preds) == 0 {
		return ""
	}
	tableID, ok := singleEmittedTableID(preds[0])
	if !ok {
		return ""
	}
	if ts, ok := a.schemaReg.Table(tableID); ok {
		return ts.Name
	}
	return ""
}

func singleEmittedTableID(pred subscription.Predicate) (schema.TableID, bool) {
	switch p := pred.(type) {
	case subscription.Join:
		return schema.TableID(p.ProjectedTable()), true
	case subscription.CrossJoin:
		return schema.TableID(p.ProjectedTable()), true
	}
	tables := pred.Tables()
	if len(tables) == 0 {
		return 0, false
	}
	return schema.TableID(tables[0]), true
}

func newProtocolSubscriptionError(durationMicros uint64, requestID, queryID uint32, errText string) *protocol.SubscriptionError {
	return &protocol.SubscriptionError{
		TotalHostExecutionDurationMicros: durationMicros,
		RequestID:                        optionalUint32(requestID),
		QueryID:                          optionalUint32(queryID),
		Error:                            errText,
	}
}

func collectInsertRows(updates []subscription.SubscriptionUpdate) []types.ProductValue {
	return collectSubscriptionRows(updates, func(update subscription.SubscriptionUpdate) []types.ProductValue {
		return update.Inserts
	})
}

func collectDeleteRows(updates []subscription.SubscriptionUpdate) []types.ProductValue {
	return collectSubscriptionRows(updates, func(update subscription.SubscriptionUpdate) []types.ProductValue {
		return update.Deletes
	})
}

func collectSubscriptionRows(updates []subscription.SubscriptionUpdate, rowsFor func(subscription.SubscriptionUpdate) []types.ProductValue) []types.ProductValue {
	var rows []types.ProductValue
	for _, update := range updates {
		rows = append(rows, rowsFor(update)...)
	}
	return rows
}

func encodeProtocolSubscriptionUpdates(updates []subscription.SubscriptionUpdate) ([]protocol.SubscriptionUpdate, error) {
	encoded := make([]protocol.SubscriptionUpdate, 0, len(updates))
	for _, update := range updates {
		wireUpdate, err := encodeProtocolSubscriptionUpdate(update)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, wireUpdate)
	}
	return encoded, nil
}

func encodeProtocolSubscriptionUpdate(update subscription.SubscriptionUpdate) (protocol.SubscriptionUpdate, error) {
	inserts, err := encodeProtocolProductRows(update.Inserts, update.Columns)
	if err != nil {
		return protocol.SubscriptionUpdate{}, fmt.Errorf("encode inserts: %w", err)
	}
	deletes, err := encodeProtocolProductRows(update.Deletes, update.Columns)
	if err != nil {
		return protocol.SubscriptionUpdate{}, fmt.Errorf("encode deletes: %w", err)
	}
	return protocol.SubscriptionUpdate{
		QueryID:   update.QueryID,
		TableName: update.TableName,
		Inserts:   inserts,
		Deletes:   deletes,
	}, nil
}

func encodeProtocolProductRows(rows []types.ProductValue, columns []schema.ColumnSchema) ([]byte, error) {
	if len(columns) == 0 {
		return protocol.EncodeProductRows(rows)
	}
	return protocol.EncodeProductRowsForColumns(rows, columns)
}

// forwardReducerResponse bridges the executor response onto the protocol
// TransactionUpdate channel and exits when the owning request is done.
func (a *ProtocolInboxAdapter) forwardReducerResponse(ctx context.Context, req protocol.CallReducerRequest, respCh <-chan ProtocolCallReducerResponse) {
	select {
	case resp, ok := <-respCh:
		a.deliverReducerResponse(ctx, req, resp, ok)
	case <-ctx.Done():
	case <-req.Done:
	}
}

func (a *ProtocolInboxAdapter) deliverReducerResponse(ctx context.Context, req protocol.CallReducerRequest, resp ProtocolCallReducerResponse, ok bool) {
	if !ok {
		sendTransactionUpdateWithContext(ctx, req.Done, req.ResponseCh, buildProtocolReducerEnvelope(req, reducerStatusToProtocol(ReducerResponse{
			Status: StatusFailedInternal,
			Error:  errors.New("executor reducer response channel closed"),
		})))
		return
	}
	if resp.Committed != nil {
		if resp.Committed.Outcome.Kind == subscription.CallerOutcomeCommitted &&
			resp.Committed.Outcome.Flags == subscription.CallerOutcomeFlagNoSuccessNotify {
			close(req.ResponseCh)
			return
		}
		update, err := protocol.BuildTransactionUpdateHeavy(req.ConnID, resp.Committed.Outcome, resp.Committed.Updates, nil)
		if err != nil {
			sendTransactionUpdateWithContext(ctx, req.Done, req.ResponseCh, buildProtocolReducerEnvelope(req, reducerStatusToProtocol(ReducerResponse{
				Status: StatusFailedInternal,
				Error:  fmt.Errorf("encode caller outcome: %w", err),
			})))
			return
		}
		sendTransactionUpdateWithContext(ctx, req.Done, req.ResponseCh, update)
		return
	}
	sendTransactionUpdateWithContext(ctx, req.Done, req.ResponseCh, buildProtocolReducerEnvelope(req, reducerStatusToProtocol(resp.Reducer)))
}

func sendTransactionUpdateWithContext(ctx context.Context, done <-chan struct{}, ch chan<- protocol.TransactionUpdate, update protocol.TransactionUpdate) bool {
	if ch == nil {
		return true
	}
	select {
	case ch <- update:
		return true
	case <-ctx.Done():
		return false
	case <-done:
		return false
	}
}

func buildProtocolReducerEnvelope(req protocol.CallReducerRequest, status protocol.UpdateStatus) protocol.TransactionUpdate {
	return protocol.TransactionUpdate{
		Status:             status,
		CallerIdentity:     req.Identity,
		CallerConnectionID: req.ConnID,
		ReducerCall: protocol.ReducerCallInfo{
			ReducerName: req.ReducerName,
			Args:        req.Args,
			RequestID:   req.RequestID,
		},
	}
}

func reducerStatusToProtocol(resp ReducerResponse) protocol.UpdateStatus {
	if resp.Status == StatusCommitted {
		return protocol.StatusCommitted{}
	}
	return protocol.StatusFailed{Error: reducerFailureText(resp)}
}

func reducerFailureText(resp ReducerResponse) string {
	detail := fmt.Sprintf("executor reducer status %d", resp.Status)
	if resp.Error != nil {
		detail = resp.Error.Error()
	}
	switch resp.Status {
	case StatusFailedUser:
		return "app reducer error: " + detail
	case StatusFailedPanic:
		return "app reducer panic: " + detail
	case StatusFailedPermission:
		return "permission denied: " + detail
	default:
		return "shunter runtime error: " + detail
	}
}

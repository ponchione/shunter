package executor

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type protocolCommandSubmitter interface {
	SubmitWithContext(ctx context.Context, cmd ExecutorCommand) error
}

// ProtocolInboxAdapter is the concrete production bridge that satisfies
// protocol.ExecutorInbox by translating protocol-layer requests into executor
// commands.
type ProtocolInboxAdapter struct {
	submitter protocolCommandSubmitter
	schemaReg schema.SchemaRegistry
}

var _ protocol.ExecutorInbox = (*ProtocolInboxAdapter)(nil)

func NewProtocolInboxAdapter(exec *Executor) *ProtocolInboxAdapter {
	return newProtocolInboxAdapter(exec, exec.schemaReg)
}

func newProtocolInboxAdapter(submitter protocolCommandSubmitter, schemaReg schema.SchemaRegistry) *ProtocolInboxAdapter {
	return &ProtocolInboxAdapter{submitter: submitter, schemaReg: schemaReg}
}

func (a *ProtocolInboxAdapter) OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error {
	respCh := make(chan ReducerResponse, 1)
	if err := a.submitter.SubmitWithContext(ctx, OnConnectCmd{ConnID: connID, Identity: identity, ResponseCh: respCh}); err != nil {
		return err
	}
	return awaitReducerStatus(ctx, respCh, "OnConnect")
}

func (a *ProtocolInboxAdapter) OnDisconnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error {
	respCh := make(chan ReducerResponse, 1)
	if err := a.submitter.SubmitWithContext(ctx, OnDisconnectCmd{ConnID: connID, Identity: identity, ResponseCh: respCh}); err != nil {
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
	cmd := RegisterSubscriptionSetCmd{
		Request: subscription.SubscriptionSetRegisterRequest{
			ConnID:     req.ConnID,
			QueryID:    req.QueryID,
			RequestID:  req.RequestID,
			Predicates: preds,
		},
		Reply: func(result subscription.SubscriptionSetRegisterResult, replyErr error) {
			if req.Reply == nil {
				return
			}
			req.Reply(a.buildRegisterResponse(req, preds, result, replyErr))
		},
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
				Identity:     req.Identity,
				ConnectionID: req.ConnID,
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
		go a.forwardReducerResponse(ctx, req, respCh)
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

func (a *ProtocolInboxAdapter) buildRegisterResponse(
	req protocol.RegisterSubscriptionSetRequest,
	preds []subscription.Predicate,
	result subscription.SubscriptionSetRegisterResult,
	replyErr error,
) protocol.SubscriptionSetCommandResponse {
	if replyErr != nil {
		return protocol.SubscriptionSetCommandResponse{
			Error: &protocol.SubscriptionError{RequestID: req.RequestID, QueryID: req.QueryID, Error: replyErr.Error()},
		}
	}
	updates := make([]protocol.SubscriptionUpdate, 0, len(result.Update))
	for _, update := range result.Update {
		encoded, err := encodeProtocolSubscriptionUpdate(update)
		if err != nil {
			return protocol.SubscriptionSetCommandResponse{
				Error: &protocol.SubscriptionError{RequestID: req.RequestID, QueryID: req.QueryID, Error: err.Error()},
			}
		}
		updates = append(updates, encoded)
	}
	if req.Variant == protocol.SubscriptionSetVariantMulti {
		return protocol.SubscriptionSetCommandResponse{
			MultiApplied: &protocol.SubscribeMultiApplied{RequestID: req.RequestID, QueryID: req.QueryID, Update: updates},
		}
	}
	rows, err := encodeProductRows(collectInsertRows(result.Update))
	if err != nil {
		return protocol.SubscriptionSetCommandResponse{
			Error: &protocol.SubscriptionError{RequestID: req.RequestID, QueryID: req.QueryID, Error: err.Error()},
		}
	}
	return protocol.SubscriptionSetCommandResponse{
		SingleApplied: &protocol.SubscribeSingleApplied{
			RequestID: req.RequestID,
			QueryID:   req.QueryID,
			TableName: a.singleTableName(preds, result.Update),
			Rows:      rows,
		},
	}
}

func (a *ProtocolInboxAdapter) buildUnregisterResponse(
	req protocol.UnregisterSubscriptionSetRequest,
	result subscription.SubscriptionSetUnregisterResult,
	replyErr error,
) protocol.UnsubscribeSetCommandResponse {
	if replyErr != nil {
		return protocol.UnsubscribeSetCommandResponse{
			Error: &protocol.SubscriptionError{RequestID: req.RequestID, QueryID: req.QueryID, Error: replyErr.Error()},
		}
	}
	updates := make([]protocol.SubscriptionUpdate, 0, len(result.Update))
	for _, update := range result.Update {
		encoded, err := encodeProtocolSubscriptionUpdate(update)
		if err != nil {
			return protocol.UnsubscribeSetCommandResponse{
				Error: &protocol.SubscriptionError{RequestID: req.RequestID, QueryID: req.QueryID, Error: err.Error()},
			}
		}
		updates = append(updates, encoded)
	}
	if req.Variant == protocol.SubscriptionSetVariantMulti {
		return protocol.UnsubscribeSetCommandResponse{
			MultiApplied: &protocol.UnsubscribeMultiApplied{RequestID: req.RequestID, QueryID: req.QueryID, Update: updates},
		}
	}
	rows, err := encodeProductRows(collectDeleteRows(result.Update))
	if err != nil {
		return protocol.UnsubscribeSetCommandResponse{
			Error: &protocol.SubscriptionError{RequestID: req.RequestID, QueryID: req.QueryID, Error: err.Error()},
		}
	}
	return protocol.UnsubscribeSetCommandResponse{
		SingleApplied: &protocol.UnsubscribeSingleApplied{
			RequestID: req.RequestID,
			QueryID:   req.QueryID,
			HasRows:   len(result.Update) > 0,
			Rows:      rows,
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
	tables := preds[0].Tables()
	if len(tables) == 0 {
		return ""
	}
	if ts, ok := a.schemaReg.Table(schema.TableID(tables[0])); ok {
		return ts.Name
	}
	return ""
}

func collectInsertRows(updates []subscription.SubscriptionUpdate) []types.ProductValue {
	var rows []types.ProductValue
	for _, update := range updates {
		rows = append(rows, update.Inserts...)
	}
	return rows
}

func collectDeleteRows(updates []subscription.SubscriptionUpdate) []types.ProductValue {
	var rows []types.ProductValue
	for _, update := range updates {
		rows = append(rows, update.Deletes...)
	}
	return rows
}

func encodeProtocolSubscriptionUpdate(update subscription.SubscriptionUpdate) (protocol.SubscriptionUpdate, error) {
	inserts, err := encodeProductRows(update.Inserts)
	if err != nil {
		return protocol.SubscriptionUpdate{}, fmt.Errorf("encode inserts: %w", err)
	}
	deletes, err := encodeProductRows(update.Deletes)
	if err != nil {
		return protocol.SubscriptionUpdate{}, fmt.Errorf("encode deletes: %w", err)
	}
	return protocol.SubscriptionUpdate{
		SubscriptionID: uint32(update.SubscriptionID),
		TableName:      update.TableName,
		Inserts:        inserts,
		Deletes:        deletes,
	}, nil
}

func encodeProductRows(rows []types.ProductValue) ([]byte, error) {
	encoded := make([][]byte, len(rows))
	for i, row := range rows {
		var buf bytes.Buffer
		if err := bsatn.EncodeProductValue(&buf, row); err != nil {
			return nil, err
		}
		encoded[i] = buf.Bytes()
	}
	return protocol.EncodeRowList(encoded), nil
}

func (a *ProtocolInboxAdapter) forwardReducerResponse(ctx context.Context, req protocol.CallReducerRequest, respCh <-chan ProtocolCallReducerResponse) {
	select {
	case resp := <-respCh:
		if resp.Committed != nil {
			if resp.Committed.Outcome.Kind == subscription.CallerOutcomeCommitted &&
				resp.Committed.Outcome.Flags == subscription.CallerOutcomeFlagNoSuccessNotify {
				close(req.ResponseCh)
				return
			}
			update, err := protocol.BuildTransactionUpdateHeavy(req.ConnID, resp.Committed.Outcome, resp.Committed.Updates, nil)
			if err != nil {
				sendTransactionUpdateWithContext(ctx, req.ResponseCh, buildProtocolReducerEnvelope(req, protocol.StatusFailed{Error: fmt.Sprintf("encode caller outcome: %v", err)}))
				return
			}
			sendTransactionUpdateWithContext(ctx, req.ResponseCh, update)
			return
		}
		sendTransactionUpdateWithContext(ctx, req.ResponseCh, buildProtocolReducerEnvelope(req, reducerStatusToProtocol(resp.Reducer)))
	case <-ctx.Done():
	}
}

func sendTransactionUpdateWithContext(ctx context.Context, ch chan<- protocol.TransactionUpdate, update protocol.TransactionUpdate) bool {
	if ch == nil {
		return true
	}
	select {
	case ch <- update:
		return true
	case <-ctx.Done():
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
	if resp.Error != nil {
		return protocol.StatusFailed{Error: resp.Error.Error()}
	}
	return protocol.StatusFailed{Error: fmt.Sprintf("executor reducer status %d", resp.Status)}
}

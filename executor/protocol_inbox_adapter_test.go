package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type stubProtocolSubmitter struct {
	submit func(context.Context, ExecutorCommand) error
}

func (s stubProtocolSubmitter) SubmitWithContext(ctx context.Context, cmd ExecutorCommand) error {
	if s.submit == nil {
		return nil
	}
	return s.submit(ctx, cmd)
}

func requireOptionalUint32(t *testing.T, got *uint32, want uint32, field string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %d", field, got, want)
	}
}

type stubProtocolSchemaRegistry struct {
	tables map[schema.TableID]string
}

func (s stubProtocolSchemaRegistry) Table(id schema.TableID) (*schema.TableSchema, bool) {
	name, ok := s.tables[id]
	if !ok {
		return nil, false
	}
	return &schema.TableSchema{ID: id, Name: name}, true
}

func newAdapterTestConn(t *testing.T) (*protocol.Conn, *protocol.ConnManager, protocol.ClientSender) {
	t.Helper()
	opts := protocol.DefaultProtocolOptions()
	conn := protocol.NewConn(types.ConnectionID{1}, types.Identity{2}, "", false, nil, &opts)
	mgr := protocol.NewConnManager()
	mgr.Add(conn)
	return conn, mgr, protocol.NewClientSender(mgr, nil)
}

func readServerMessage(t *testing.T, conn *protocol.Conn) (uint8, any) {
	t.Helper()
	select {
	case frame := <-conn.OutboundCh:
		tag, msg, err := protocol.DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("DecodeServerMessage: %v", err)
		}
		return tag, msg
	default:
		t.Fatal("expected frame on OutboundCh")
		return 0, nil
	}
}

func assertNoServerMessage(t *testing.T, conn *protocol.Conn) {
	t.Helper()
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected frame on OutboundCh: %x", frame)
	default:
	}
}

func TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleSuccessReply(t *testing.T) {
	conn, _, sender := newAdapterTestConn(t)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg, ok := cmd.(RegisterSubscriptionSetCmd)
			if !ok {
				t.Fatalf("command type = %T, want RegisterSubscriptionSetCmd", cmd)
			}
			if reg.Request.ConnID != conn.ID || reg.Request.QueryID != 7 || reg.Request.RequestID != 10 {
				t.Fatalf("register request = %+v", reg.Request)
			}
			if len(reg.Request.Predicates) != 1 {
				t.Fatalf("len(Predicates) = %d, want 1", len(reg.Request.Predicates))
			}
			reg.Reply(subscription.SubscriptionSetRegisterResult{
				QueryID:                          7,
				TotalHostExecutionDurationMicros: 111,
				Update: []subscription.SubscriptionUpdate{{
					SubscriptionID: 11,
					TableID:        1,
					TableName:      "users",
					Inserts: []types.ProductValue{{
						types.NewUint32(42),
					}},
				}},
			}, nil)
			return nil
		}},
		stubProtocolSchemaRegistry{tables: map[schema.TableID]string{1: "users"}},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   7,
		RequestID: 10,
		Variant:   protocol.SubscriptionSetVariantSingle,
		Predicates: []any{
			subscription.AllRows{Table: 1},
		},
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			if resp.SingleApplied == nil {
				t.Fatalf("response = %+v, want SingleApplied", resp)
			}
			if err := protocol.SendSubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				t.Fatalf("SendSubscribeSingleApplied: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}

	tag, decoded := readServerMessage(t, conn)
	if tag != protocol.TagSubscribeSingleApplied {
		t.Fatalf("tag = %d, want %d", tag, protocol.TagSubscribeSingleApplied)
	}
	applied := decoded.(protocol.SubscribeSingleApplied)
	if applied.RequestID != 10 || applied.QueryID != 7 || applied.TableName != "users" {
		t.Fatalf("SubscribeSingleApplied = %+v", applied)
	}
	if applied.TotalHostExecutionDurationMicros != 111 {
		t.Fatalf("TotalHostExecutionDurationMicros = %d, want 111", applied.TotalHostExecutionDurationMicros)
	}
	rows, err := protocol.DecodeRowList(applied.Rows)
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
}

func TestProtocolInboxAdapter_RegisterSubscriptionSet_EmptyProjectedJoinUsesProjectedTableName(t *testing.T) {
	tests := []struct {
		name string
		pred subscription.Predicate
	}{
		{
			name: "join",
			pred: subscription.Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, ProjectRight: true},
		},
		{
			name: "cross join",
			pred: subscription.CrossJoin{Left: 1, Right: 2, ProjectRight: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, _, _ := newAdapterTestConn(t)
			var captured *protocol.SubscribeSingleApplied
			adapter := newProtocolInboxAdapter(
				stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
					reg := cmd.(RegisterSubscriptionSetCmd)
					reg.Reply(subscription.SubscriptionSetRegisterResult{
						QueryID:                          7,
						TotalHostExecutionDurationMicros: 111,
					}, nil)
					return nil
				}},
				stubProtocolSchemaRegistry{tables: map[schema.TableID]string{1: "lhs", 2: "rhs"}},
			)

			err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
				ConnID:     conn.ID,
				QueryID:    7,
				RequestID:  10,
				Variant:    protocol.SubscriptionSetVariantSingle,
				Predicates: []any{tt.pred},
				Reply: func(resp protocol.SubscriptionSetCommandResponse) {
					captured = resp.SingleApplied
				},
			})
			if err != nil {
				t.Fatalf("RegisterSubscriptionSet: %v", err)
			}
			if captured == nil {
				t.Fatalf("response missing SingleApplied")
			}
			if captured.TableName != "rhs" {
				t.Fatalf("TableName = %q, want projected table rhs", captured.TableName)
			}
		})
	}
}

func TestProtocolInboxAdapter_RegisterSubscriptionSet_DuplicateErrorReply(t *testing.T) {
	conn, _, sender := newAdapterTestConn(t)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg := cmd.(RegisterSubscriptionSetCmd)
			reg.Reply(subscription.SubscriptionSetRegisterResult{}, subscription.ErrQueryIDAlreadyLive)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    9,
		RequestID:  4,
		Variant:    protocol.SubscriptionSetVariantMulti,
		Predicates: []any{subscription.AllRows{Table: 1}, subscription.AllRows{Table: 2}},
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			if resp.Error == nil {
				t.Fatalf("response = %+v, want Error", resp)
			}
			if err := protocol.SendSubscriptionError(sender, conn, resp.Error); err != nil {
				t.Fatalf("SendSubscriptionError: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}

	tag, decoded := readServerMessage(t, conn)
	if tag != protocol.TagSubscriptionError {
		t.Fatalf("tag = %d, want %d", tag, protocol.TagSubscriptionError)
	}
	resp := decoded.(protocol.SubscriptionError)
	requireOptionalUint32(t, resp.RequestID, 4, "SubscriptionError.RequestID")
	requireOptionalUint32(t, resp.QueryID, 9, "SubscriptionError.QueryID")
	if resp.TableID != nil {
		t.Fatalf("SubscriptionError.TableID = %v, want nil for multi-table duplicate-register error", *resp.TableID)
	}
	if !errors.Is(subscription.ErrQueryIDAlreadyLive, subscription.ErrQueryIDAlreadyLive) {
		t.Fatal("sanity")
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty error text")
	}
}

// TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleTableErrorEmitsNilTableID
// pins that request-origin SubscriptionError leaves table_id unset.
func TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleTableErrorEmitsNilTableID(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	var captured *protocol.SubscriptionError
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg := cmd.(RegisterSubscriptionSetCmd)
			reg.Reply(subscription.SubscriptionSetRegisterResult{}, subscription.ErrQueryIDAlreadyLive)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    9,
		RequestID:  4,
		Variant:    protocol.SubscriptionSetVariantSingle,
		Predicates: []any{subscription.AllRows{Table: 1}},
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			captured = resp.Error
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}
	if captured == nil {
		t.Fatal("expected SubscriptionError captured, got nil")
	}
	if captured.TableID != nil {
		t.Fatalf("SubscriptionError.TableID = %v, want nil (reference v1 always None on request-origin error paths)", *captured.TableID)
	}
}

// TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleInitialEvalErrorWrapsWithSql
// pins the reference `DBError::WithSql` shape on the SubscribeSingle
// initial-snapshot evaluation error path (reference
// `error.rs:140` = `"{error}, executing: \`{sql}\`"`;
// `module_subscription_actor.rs:672` wraps
// `evaluate_initial_subscription` via `return_on_err_with_sql_bool!`).
// Admission-time errors that are not initial-eval (duplicate QID,
// validation) stay unwrapped, matching reference where only compile
// and initial-eval errors flow through the WithSql macro.
func TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleInitialEvalErrorWrapsWithSql(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	const sqlText = "SELECT * FROM users WHERE id = 42"
	initialEvalErr := fmt.Errorf("%w: %w", subscription.ErrInitialQuery, subscription.ErrInitialRowLimit)
	var captured *protocol.SubscriptionError
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg := cmd.(RegisterSubscriptionSetCmd)
			reg.Reply(subscription.SubscriptionSetRegisterResult{}, initialEvalErr)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    9,
		RequestID:  4,
		Variant:    protocol.SubscriptionSetVariantSingle,
		Predicates: []any{subscription.AllRows{Table: 1}},
		SQLText:    sqlText,
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			captured = resp.Error
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}
	if captured == nil {
		t.Fatal("expected SubscriptionError captured, got nil")
	}
	wantSuffix := ", executing: `" + sqlText + "`"
	if !strings.HasSuffix(captured.Error, wantSuffix) {
		t.Fatalf("SubscriptionError.Error = %q, want suffix %q (reference DBError::WithSql)", captured.Error, wantSuffix)
	}
}

// TestProtocolInboxAdapter_RegisterSubscriptionSet_DuplicateErrorIsNotWrappedWithSql
// pins the negative complement of the WithSql wrap: a non-initial-eval
// admission error (here `ErrQueryIDAlreadyLive`) must not gain the
// `", executing: \`<sql>\`"` suffix even when SQLText is populated.
// Reference wraps only compile + initial-eval errors
// (`module_subscription_actor.rs:643,:672,:756`); `add_subscription`
// duplicate-QID errors propagate unwrapped.
func TestProtocolInboxAdapter_RegisterSubscriptionSet_DuplicateErrorIsNotWrappedWithSql(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	const sqlText = "SELECT * FROM users"
	var captured *protocol.SubscriptionError
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg := cmd.(RegisterSubscriptionSetCmd)
			reg.Reply(subscription.SubscriptionSetRegisterResult{}, subscription.ErrQueryIDAlreadyLive)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    9,
		RequestID:  4,
		Variant:    protocol.SubscriptionSetVariantSingle,
		Predicates: []any{subscription.AllRows{Table: 1}},
		SQLText:    sqlText,
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			captured = resp.Error
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}
	if captured == nil {
		t.Fatal("expected SubscriptionError captured, got nil")
	}
	if strings.Contains(captured.Error, "executing: `") {
		t.Fatalf("SubscriptionError.Error = %q, must not carry executing-SQL suffix for non-initial-eval admission error", captured.Error)
	}
}

// TestProtocolInboxAdapter_RegisterSubscriptionSet_MultiInitialEvalErrorEmitsCannedMessage
// pins the canned SubscribeMulti initial-eval error text.
func TestProtocolInboxAdapter_RegisterSubscriptionSet_MultiInitialEvalErrorEmitsCannedMessage(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	initialEvalErr := fmt.Errorf("%w: %w", subscription.ErrInitialQuery, subscription.ErrInitialRowLimit)
	var captured *protocol.SubscriptionError
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg := cmd.(RegisterSubscriptionSetCmd)
			reg.Reply(subscription.SubscriptionSetRegisterResult{}, initialEvalErr)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    22,
		RequestID:  23,
		Variant:    protocol.SubscriptionSetVariantMulti,
		Predicates: []any{subscription.AllRows{Table: 1}, subscription.AllRows{Table: 2}},
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			captured = resp.Error
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}
	if captured == nil {
		t.Fatal("expected SubscriptionError captured, got nil")
	}
	if captured.Error != "Internal error evaluating queries" {
		t.Fatalf("SubscriptionError.Error = %q, want exactly %q (reference canned text)", captured.Error, "Internal error evaluating queries")
	}
	if strings.Contains(captured.Error, "executing: `") {
		t.Fatalf("SubscriptionError.Error = %q, must not carry Single-path WithSql suffix on Multi initial-eval", captured.Error)
	}
}

// TestProtocolInboxAdapter_RegisterSubscriptionSet_MultiDuplicateErrorNotCanned
// pins the negative complement: non-initial-eval admission errors on
// the Multi path (here `ErrQueryIDAlreadyLive`) are not substituted with
// the canned text — only initial-eval failures funnel through the
// reference `evaluate_queries` canned-message shape.
func TestProtocolInboxAdapter_RegisterSubscriptionSet_MultiDuplicateErrorNotCanned(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	var captured *protocol.SubscriptionError
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg := cmd.(RegisterSubscriptionSetCmd)
			reg.Reply(subscription.SubscriptionSetRegisterResult{}, subscription.ErrQueryIDAlreadyLive)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    24,
		RequestID:  25,
		Variant:    protocol.SubscriptionSetVariantMulti,
		Predicates: []any{subscription.AllRows{Table: 1}, subscription.AllRows{Table: 2}},
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			captured = resp.Error
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}
	if captured == nil {
		t.Fatal("expected SubscriptionError captured, got nil")
	}
	if captured.Error == "Internal error evaluating queries" {
		t.Fatalf("SubscriptionError.Error = %q, must not use the canned initial-eval text for admission errors", captured.Error)
	}
	if captured.Error == "" {
		t.Fatal("SubscriptionError.Error is empty, want ErrQueryIDAlreadyLive text")
	}
}

func TestProtocolInboxAdapter_RegisterSubscriptionSet_ForwardsPerPredicateHashIdentity(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	id := types.Identity{0xAA, 0xBB}
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg, ok := cmd.(RegisterSubscriptionSetCmd)
			if !ok {
				t.Fatalf("command type = %T, want RegisterSubscriptionSetCmd", cmd)
			}
			if len(reg.Request.PredicateHashIdentities) != 1 {
				t.Fatalf("len(PredicateHashIdentities) = %d, want 1", len(reg.Request.PredicateHashIdentities))
			}
			if reg.Request.PredicateHashIdentities[0] == nil {
				t.Fatal("PredicateHashIdentities[0] = nil, want forwarded identity")
			}
			if *reg.Request.PredicateHashIdentities[0] != id {
				t.Fatalf("PredicateHashIdentities[0] = %x, want %x", *reg.Request.PredicateHashIdentities[0], id)
			}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:                  conn.ID,
		QueryID:                 71,
		RequestID:               72,
		Variant:                 protocol.SubscriptionSetVariantSingle,
		Predicates:              []any{subscription.AllRows{Table: 1}},
		PredicateHashIdentities: []*types.Identity{&id},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}
}

func TestProtocolInboxAdapter_RegisterSubscriptionSet_ForwardsMixedPerPredicateHashIdentity(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	id := types.Identity{0x11, 0x22}
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg, ok := cmd.(RegisterSubscriptionSetCmd)
			if !ok {
				t.Fatalf("command type = %T, want RegisterSubscriptionSetCmd", cmd)
			}
			if len(reg.Request.PredicateHashIdentities) != 2 {
				t.Fatalf("len(PredicateHashIdentities) = %d, want 2", len(reg.Request.PredicateHashIdentities))
			}
			if reg.Request.PredicateHashIdentities[0] != nil {
				t.Fatalf("PredicateHashIdentities[0] = %x, want nil", *reg.Request.PredicateHashIdentities[0])
			}
			if reg.Request.PredicateHashIdentities[1] == nil {
				t.Fatal("PredicateHashIdentities[1] = nil, want forwarded identity")
			}
			if *reg.Request.PredicateHashIdentities[1] != id {
				t.Fatalf("PredicateHashIdentities[1] = %x, want %x", *reg.Request.PredicateHashIdentities[1], id)
			}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:                  conn.ID,
		QueryID:                 81,
		RequestID:               82,
		Variant:                 protocol.SubscriptionSetVariantMulti,
		Predicates:              []any{subscription.AllRows{Table: 1}, subscription.AllRows{Table: 2}},
		PredicateHashIdentities: []*types.Identity{nil, &id},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}
}

func TestProtocolInboxAdapter_UnregisterSubscriptionSet_SingleSuccessReply(t *testing.T) {
	conn, _, sender := newAdapterTestConn(t)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			unreg, ok := cmd.(UnregisterSubscriptionSetCmd)
			if !ok {
				t.Fatalf("command type = %T, want UnregisterSubscriptionSetCmd", cmd)
			}
			if unreg.ConnID != conn.ID || unreg.QueryID != 42 {
				t.Fatalf("unregister cmd = %+v", unreg)
			}
			unreg.Reply(subscription.SubscriptionSetUnregisterResult{
				QueryID:                          42,
				TotalHostExecutionDurationMicros: 222,
				Update: []subscription.SubscriptionUpdate{{
					SubscriptionID: 7,
					Deletes: []types.ProductValue{{
						types.NewUint32(99),
					}},
				}},
			}, nil)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.UnregisterSubscriptionSet(context.Background(), protocol.UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   42,
		RequestID: 3,
		Variant:   protocol.SubscriptionSetVariantSingle,
		Reply: func(resp protocol.UnsubscribeSetCommandResponse) {
			if resp.SingleApplied == nil {
				t.Fatalf("response = %+v, want SingleApplied", resp)
			}
			if err := protocol.SendUnsubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				t.Fatalf("SendUnsubscribeSingleApplied: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatalf("UnregisterSubscriptionSet: %v", err)
	}

	tag, decoded := readServerMessage(t, conn)
	if tag != protocol.TagUnsubscribeSingleApplied {
		t.Fatalf("tag = %d, want %d", tag, protocol.TagUnsubscribeSingleApplied)
	}
	applied := decoded.(protocol.UnsubscribeSingleApplied)
	if applied.RequestID != 3 || applied.QueryID != 42 || !applied.HasRows {
		t.Fatalf("UnsubscribeSingleApplied = %+v", applied)
	}
	if applied.TotalHostExecutionDurationMicros != 222 {
		t.Fatalf("TotalHostExecutionDurationMicros = %d, want 222", applied.TotalHostExecutionDurationMicros)
	}
	rows, err := protocol.DecodeRowList(applied.Rows)
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
}

func TestProtocolInboxAdapter_UnregisterSubscriptionSet_NotFoundErrorReply(t *testing.T) {
	conn, _, sender := newAdapterTestConn(t)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			unreg := cmd.(UnregisterSubscriptionSetCmd)
			unreg.Reply(subscription.SubscriptionSetUnregisterResult{}, subscription.ErrSubscriptionNotFound)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.UnregisterSubscriptionSet(context.Background(), protocol.UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   77,
		RequestID: 12,
		Variant:   protocol.SubscriptionSetVariantMulti,
		Reply: func(resp protocol.UnsubscribeSetCommandResponse) {
			if resp.Error == nil {
				t.Fatalf("response = %+v, want Error", resp)
			}
			if err := protocol.SendSubscriptionError(sender, conn, resp.Error); err != nil {
				t.Fatalf("SendSubscriptionError: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatalf("UnregisterSubscriptionSet: %v", err)
	}

	tag, decoded := readServerMessage(t, conn)
	if tag != protocol.TagSubscriptionError {
		t.Fatalf("tag = %d, want %d", tag, protocol.TagSubscriptionError)
	}
	resp := decoded.(protocol.SubscriptionError)
	requireOptionalUint32(t, resp.RequestID, 12, "SubscriptionError.RequestID")
	requireOptionalUint32(t, resp.QueryID, 77, "SubscriptionError.QueryID")
	if resp.Error == "" {
		t.Fatal("expected non-empty error text")
	}
}

// TestProtocolInboxAdapter_UnregisterSubscriptionSet_SingleFinalEvalErrorWrapsWithSql
// pins the reference UnsubscribeSingle final-eval error shape
// (`module_subscription_actor.rs:756` via `return_on_err_with_sql!`):
// an ErrFinalQuery-wrapped reply with a non-empty result SQLText
// gets the `DBError::WithSql` suffix `", executing: \`<sql>\`"` on
// the Single Variant only. The WithSql suffix format is defined at
// `reference/.../error.rs:140`.
func TestProtocolInboxAdapter_UnregisterSubscriptionSet_SingleFinalEvalErrorWrapsWithSql(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	const sqlText = "SELECT * FROM users WHERE id = 42"
	finalEvalErr := fmt.Errorf("%w: %w", subscription.ErrFinalQuery, subscription.ErrInitialRowLimit)
	var captured *protocol.SubscriptionError
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			unreg := cmd.(UnregisterSubscriptionSetCmd)
			unreg.Reply(subscription.SubscriptionSetUnregisterResult{SQLText: sqlText}, finalEvalErr)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.UnregisterSubscriptionSet(context.Background(), protocol.UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   31,
		RequestID: 32,
		Variant:   protocol.SubscriptionSetVariantSingle,
		Reply: func(resp protocol.UnsubscribeSetCommandResponse) {
			captured = resp.Error
		},
	})
	if err != nil {
		t.Fatalf("UnregisterSubscriptionSet: %v", err)
	}
	if captured == nil {
		t.Fatal("expected SubscriptionError captured, got nil")
	}
	wantSuffix := ", executing: `" + sqlText + "`"
	if !strings.HasSuffix(captured.Error, wantSuffix) {
		t.Fatalf("SubscriptionError.Error = %q, want suffix %q (reference DBError::WithSql)", captured.Error, wantSuffix)
	}
}

// TestProtocolInboxAdapter_UnregisterSubscriptionSet_MultiFinalEvalErrorRawText
// pins the reference UnsubscribeMulti final-eval shape
// (`module_subscription_actor.rs:836` via plain `return_on_err!`): an
// ErrFinalQuery-wrapped reply on the Multi Variant emits raw error text
// — no `DBError::WithSql` suffix and no canned SubscribeMulti text. The
// SubscribeMulti canned-message branch is admission-only (reference
// `:1383`) and does not apply to the unsubscribe path.
func TestProtocolInboxAdapter_UnregisterSubscriptionSet_MultiFinalEvalErrorRawText(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	finalEvalErr := fmt.Errorf("%w: %w", subscription.ErrFinalQuery, subscription.ErrInitialRowLimit)
	var captured *protocol.SubscriptionError
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			unreg := cmd.(UnregisterSubscriptionSetCmd)
			// Multi path never populates SQLText on queryState, so the
			// result SQLText is empty; the adapter would not apply the
			// Single suffix even if Variant were Single in this case.
			unreg.Reply(subscription.SubscriptionSetUnregisterResult{}, finalEvalErr)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.UnregisterSubscriptionSet(context.Background(), protocol.UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   41,
		RequestID: 42,
		Variant:   protocol.SubscriptionSetVariantMulti,
		Reply: func(resp protocol.UnsubscribeSetCommandResponse) {
			captured = resp.Error
		},
	})
	if err != nil {
		t.Fatalf("UnregisterSubscriptionSet: %v", err)
	}
	if captured == nil {
		t.Fatal("expected SubscriptionError captured, got nil")
	}
	if strings.Contains(captured.Error, "executing: `") {
		t.Fatalf("SubscriptionError.Error = %q, must not carry Single-path WithSql suffix on Multi unsubscribe", captured.Error)
	}
	if captured.Error == "Internal error evaluating queries" {
		t.Fatalf("SubscriptionError.Error = %q, must not use the admission-path canned text on the unsubscribe path", captured.Error)
	}
	if captured.Error == "" {
		t.Fatal("SubscriptionError.Error is empty, want raw ErrFinalQuery-wrapped text")
	}
}

// TestProtocolInboxAdapter_UnregisterSubscriptionSet_NotFoundErrorIsNotWrappedWithSql
// pins the negative complement of the Single WithSql wrap on the
// unsubscribe path: admission-time errors that are not final-eval (here
// `ErrSubscriptionNotFound`) must not gain the `", executing: \`<sql>\`"`
// suffix even if Variant is Single. Reference only WithSql-wraps the
// evaluate_initial_subscription result (`module_subscription_actor.rs:756`);
// `remove_subscription` failures (`:741`) propagate raw.
func TestProtocolInboxAdapter_UnregisterSubscriptionSet_NotFoundErrorIsNotWrappedWithSql(t *testing.T) {
	conn, _, _ := newAdapterTestConn(t)
	var captured *protocol.SubscriptionError
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			unreg := cmd.(UnregisterSubscriptionSetCmd)
			// SQLText empty because remove_subscription-equivalent failures
			// never observe a queryState on the erroring path.
			unreg.Reply(subscription.SubscriptionSetUnregisterResult{}, subscription.ErrSubscriptionNotFound)
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.UnregisterSubscriptionSet(context.Background(), protocol.UnregisterSubscriptionSetRequest{
		ConnID:    conn.ID,
		QueryID:   51,
		RequestID: 52,
		Variant:   protocol.SubscriptionSetVariantSingle,
		Reply: func(resp protocol.UnsubscribeSetCommandResponse) {
			captured = resp.Error
		},
	})
	if err != nil {
		t.Fatalf("UnregisterSubscriptionSet: %v", err)
	}
	if captured == nil {
		t.Fatal("expected SubscriptionError captured, got nil")
	}
	if strings.Contains(captured.Error, "executing: `") {
		t.Fatalf("SubscriptionError.Error = %q, must not carry WithSql suffix on non-final-eval unsubscribe error", captured.Error)
	}
}

func TestProtocolInboxAdapter_RegisterSubscriptionSet_ClosedConnectionReplyDiscarded(t *testing.T) {
	conn, mgr, sender := newAdapterTestConn(t)
	var deliveryErr error
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			mgr.Remove(conn.ID)
			reg := cmd.(RegisterSubscriptionSetCmd)
			reg.Reply(subscription.SubscriptionSetRegisterResult{
				QueryID: 5,
				Update: []subscription.SubscriptionUpdate{{
					SubscriptionID: 1,
					TableID:        1,
					TableName:      "users",
				}},
			}, nil)
			return nil
		}},
		stubProtocolSchemaRegistry{tables: map[schema.TableID]string{1: "users"}},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    5,
		RequestID:  6,
		Variant:    protocol.SubscriptionSetVariantSingle,
		Predicates: []any{subscription.AllRows{Table: 1}},
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			deliveryErr = protocol.SendSubscribeSingleApplied(sender, conn, resp.SingleApplied)
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}
	if !errors.Is(deliveryErr, protocol.ErrConnNotFound) {
		t.Fatalf("deliveryErr = %v, want ErrConnNotFound", deliveryErr)
	}
	assertNoServerMessage(t, conn)
}

func TestProtocolInboxAdapter_RegisterSubscriptionSet_ReplyPrecedesLaterSameGoroutineSend(t *testing.T) {
	conn, _, sender := newAdapterTestConn(t)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			reg := cmd.(RegisterSubscriptionSetCmd)
			reg.Reply(subscription.SubscriptionSetRegisterResult{
				QueryID: 13,
				Update: []subscription.SubscriptionUpdate{{
					SubscriptionID: 1,
					TableID:        1,
					TableName:      "users",
				}},
			}, nil)
			if err := protocol.SendSubscriptionError(sender, conn, &protocol.SubscriptionError{
				RequestID: optionalUint32(99),
				QueryID:   optionalUint32(13),
				Error:     "later",
			}); err != nil {
				t.Fatalf("SendSubscriptionError: %v", err)
			}
			return nil
		}},
		stubProtocolSchemaRegistry{tables: map[schema.TableID]string{1: "users"}},
	)

	err := adapter.RegisterSubscriptionSet(context.Background(), protocol.RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    13,
		RequestID:  8,
		Variant:    protocol.SubscriptionSetVariantSingle,
		Predicates: []any{subscription.AllRows{Table: 1}},
		Reply: func(resp protocol.SubscriptionSetCommandResponse) {
			if err := protocol.SendSubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				t.Fatalf("SendSubscribeSingleApplied: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatalf("RegisterSubscriptionSet: %v", err)
	}

	tag1, _ := readServerMessage(t, conn)
	if tag1 != protocol.TagSubscribeSingleApplied {
		t.Fatalf("first tag = %d, want %d", tag1, protocol.TagSubscribeSingleApplied)
	}
	tag2, decoded2 := readServerMessage(t, conn)
	if tag2 != protocol.TagSubscriptionError {
		t.Fatalf("second tag = %d, want %d", tag2, protocol.TagSubscriptionError)
	}
	if got := decoded2.(protocol.SubscriptionError); got.RequestID == nil || *got.RequestID != 99 {
		t.Fatalf("second message = %+v", got)
	}
}

func TestProtocolInboxAdapter_OnConnect_BridgesLifecycleResponse(t *testing.T) {
	connID := types.ConnectionID{7}
	identity := types.Identity{8}
	principal := types.AuthPrincipal{Issuer: "issuer", Subject: "alice", Audience: []string{"api"}}
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			onConnect, ok := cmd.(OnConnectCmd)
			if !ok {
				t.Fatalf("command type = %T, want OnConnectCmd", cmd)
			}
			if onConnect.ConnID != connID || onConnect.Identity != identity {
				t.Fatalf("OnConnectCmd = %+v", onConnect)
			}
			if got := onConnect.Principal; got.Issuer != "issuer" || got.Subject != "alice" ||
				len(got.Audience) != 1 || got.Audience[0] != "api" {
				t.Fatalf("OnConnectCmd.Principal = %+v, want copied issuer/subject/audience", got)
			}
			onConnect.ResponseCh <- ReducerResponse{Status: StatusCommitted}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	if err := adapter.OnConnect(context.Background(), connID, identity, principal); err != nil {
		t.Fatalf("OnConnect: %v", err)
	}
}

func TestProtocolInboxAdapter_OnConnect_UsesBufferedResponseChannel(t *testing.T) {
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			onConnect, ok := cmd.(OnConnectCmd)
			if !ok {
				t.Fatalf("command type = %T, want OnConnectCmd", cmd)
			}
			if cap(onConnect.ResponseCh) != 1 {
				t.Fatalf("cap(ResponseCh) = %d, want 1", cap(onConnect.ResponseCh))
			}
			onConnect.ResponseCh <- ReducerResponse{Status: StatusCommitted}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	if err := adapter.OnConnect(context.Background(), types.ConnectionID{1}, types.Identity{2}, types.AuthPrincipal{}); err != nil {
		t.Fatalf("OnConnect: %v", err)
	}
}

func TestProtocolInboxAdapter_OnDisconnect_UsesBufferedResponseChannel(t *testing.T) {
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			onDisconnect, ok := cmd.(OnDisconnectCmd)
			if !ok {
				t.Fatalf("command type = %T, want OnDisconnectCmd", cmd)
			}
			if cap(onDisconnect.ResponseCh) != 1 {
				t.Fatalf("cap(ResponseCh) = %d, want 1", cap(onDisconnect.ResponseCh))
			}
			onDisconnect.ResponseCh <- ReducerResponse{Status: StatusCommitted}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	if err := adapter.OnDisconnect(context.Background(), types.ConnectionID{1}, types.Identity{2}, types.AuthPrincipal{}); err != nil {
		t.Fatalf("OnDisconnect: %v", err)
	}
}

func TestProtocolInboxAdapter_CallReducer_TranslatesFailedReducerResponse(t *testing.T) {
	connID := types.ConnectionID{3}
	identity := types.Identity{4}
	respCh := make(chan protocol.TransactionUpdate, 1)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			call, ok := cmd.(CallReducerCmd)
			if !ok {
				t.Fatalf("command type = %T, want CallReducerCmd", cmd)
			}
			if call.Request.Caller.ConnectionID != connID || call.Request.Caller.Identity != identity {
				t.Fatalf("call request = %+v", call.Request)
			}
			call.ProtocolResponseCh <- ProtocolCallReducerResponse{Reducer: ReducerResponse{Status: StatusFailedUser, Error: errors.New("boom")}}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.CallReducer(context.Background(), protocol.CallReducerRequest{
		ConnID:      connID,
		Identity:    identity,
		RequestID:   55,
		ReducerName: "DoThing",
		Args:        []byte{0xAA},
		ResponseCh:  respCh,
	})
	if err != nil {
		t.Fatalf("CallReducer: %v", err)
	}

	select {
	case update := <-respCh:
		failed, ok := update.Status.(protocol.StatusFailed)
		if !ok {
			t.Fatalf("status = %T, want protocol.StatusFailed", update.Status)
		}
		if failed.Error != "boom" {
			t.Fatalf("failed.Error = %q, want boom", failed.Error)
		}
		if update.CallerConnectionID != connID || update.CallerIdentity != identity {
			t.Fatalf("update caller metadata = %+v", update)
		}
		if update.ReducerCall.RequestID != 55 || update.ReducerCall.ReducerName != "DoThing" {
			t.Fatalf("update reducer info = %+v", update.ReducerCall)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for TransactionUpdate")
	}
}

func TestProtocolInboxAdapter_CallReducer_ForwardsPermissionContext(t *testing.T) {
	principal := types.AuthPrincipal{
		Issuer:      "issuer",
		Subject:     "alice",
		Audience:    []string{"shunter-api"},
		Permissions: []string{"principal:permission"},
	}
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			call, ok := cmd.(CallReducerCmd)
			if !ok {
				t.Fatalf("command type = %T, want CallReducerCmd", cmd)
			}
			if got := call.Request.Caller.Permissions; len(got) != 1 || got[0] != "messages:send" {
				t.Fatalf("caller permissions = %#v, want messages:send", got)
			}
			if got := call.Request.Caller.Principal; got.Issuer != "issuer" || got.Subject != "alice" ||
				len(got.Audience) != 1 || got.Audience[0] != "shunter-api" ||
				len(got.Permissions) != 1 || got.Permissions[0] != "principal:permission" {
				t.Fatalf("caller principal = %+v, want copied issuer/subject/audience/permissions", got)
			}
			if !call.Request.Caller.AllowAllPermissions {
				t.Fatal("AllowAllPermissions = false, want true")
			}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.CallReducer(context.Background(), protocol.CallReducerRequest{
		ConnID:              types.ConnectionID{3},
		Identity:            types.Identity{4},
		Principal:           principal,
		RequestID:           55,
		ReducerName:         "DoThing",
		Permissions:         []string{"messages:send"},
		AllowAllPermissions: true,
	})
	if err != nil {
		t.Fatalf("CallReducer: %v", err)
	}
}

func TestProtocolInboxAdapter_CallReducer_UsesBufferedProtocolResponseChannel(t *testing.T) {
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			call, ok := cmd.(CallReducerCmd)
			if !ok {
				t.Fatalf("command type = %T, want CallReducerCmd", cmd)
			}
			if cap(call.ProtocolResponseCh) != 1 {
				t.Fatalf("cap(ProtocolResponseCh) = %d, want 1", cap(call.ProtocolResponseCh))
			}
			call.ProtocolResponseCh <- ProtocolCallReducerResponse{Reducer: ReducerResponse{Status: StatusFailedUser, Error: errors.New("boom")}}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	respCh := make(chan protocol.TransactionUpdate, 1)
	if err := adapter.CallReducer(context.Background(), protocol.CallReducerRequest{
		ConnID:      types.ConnectionID{3},
		Identity:    types.Identity{4},
		RequestID:   55,
		ReducerName: "DoThing",
		ResponseCh:  respCh,
	}); err != nil {
		t.Fatalf("CallReducer: %v", err)
	}

	select {
	case <-respCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for forwarded reducer response")
	}
}

func TestProtocolInboxAdapter_CallReducer_ForwardsCommittedHeavyEnvelopeWithRealUpdates(t *testing.T) {
	connID := types.ConnectionID{8}
	identity := types.Identity{9}
	respCh := make(chan protocol.TransactionUpdate, 1)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			call, ok := cmd.(CallReducerCmd)
			if !ok {
				t.Fatalf("command type = %T, want CallReducerCmd", cmd)
			}
			call.ProtocolResponseCh <- ProtocolCallReducerResponse{
				Reducer: ReducerResponse{Status: StatusCommitted},
				Committed: &CommittedCallerPayload{
					Outcome: subscription.CallerOutcome{
						Kind:           subscription.CallerOutcomeCommitted,
						CallerIdentity: identity,
						ReducerName:    "DoThing",
						RequestID:      55,
						Args:           []byte{0xAA},
					},
					Updates: []subscription.SubscriptionUpdate{{
						SubscriptionID: 11,
						TableName:      "users",
						Inserts:        []types.ProductValue{{types.NewUint32(42)}},
					}},
				},
			}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.CallReducer(context.Background(), protocol.CallReducerRequest{
		ConnID:      connID,
		Identity:    identity,
		RequestID:   55,
		ReducerName: "DoThing",
		Args:        []byte{0xAA},
		ResponseCh:  respCh,
	})
	if err != nil {
		t.Fatalf("CallReducer: %v", err)
	}

	select {
	case update := <-respCh:
		committed, ok := update.Status.(protocol.StatusCommitted)
		if !ok {
			t.Fatalf("status = %T, want protocol.StatusCommitted", update.Status)
		}
		if len(committed.Update) != 1 {
			t.Fatalf("committed.Update len = %d, want 1", len(committed.Update))
		}
		if update.CallerConnectionID != connID || update.CallerIdentity != identity {
			t.Fatalf("update caller metadata = %+v", update)
		}
		if update.ReducerCall.RequestID != 55 || update.ReducerCall.ReducerName != "DoThing" {
			t.Fatalf("update reducer info = %+v", update.ReducerCall)
		}
		rows, err := protocol.DecodeRowList(committed.Update[0].Inserts)
		if err != nil {
			t.Fatalf("DecodeRowList: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("insert row count = %d, want 1", len(rows))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for TransactionUpdate")
	}
}

func TestProtocolInboxAdapter_CallReducer_SuppressesCommittedSuccessForNoSuccessNotify(t *testing.T) {
	connID := types.ConnectionID{10}
	identity := types.Identity{11}
	respCh := make(chan protocol.TransactionUpdate, 1)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			call := cmd.(CallReducerCmd)
			call.ProtocolResponseCh <- ProtocolCallReducerResponse{
				Reducer: ReducerResponse{Status: StatusCommitted},
				Committed: &CommittedCallerPayload{
					Outcome: subscription.CallerOutcome{
						Kind:           subscription.CallerOutcomeCommitted,
						Flags:          subscription.CallerOutcomeFlagNoSuccessNotify,
						CallerIdentity: identity,
						ReducerName:    "QuietThing",
						RequestID:      77,
					},
				},
			}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.CallReducer(context.Background(), protocol.CallReducerRequest{
		ConnID:      connID,
		Identity:    identity,
		RequestID:   77,
		ReducerName: "QuietThing",
		Flags:       protocol.CallReducerFlagsNoSuccessNotify,
		ResponseCh:  respCh,
	})
	if err != nil {
		t.Fatalf("CallReducer: %v", err)
	}

	select {
	case update, ok := <-respCh:
		if ok {
			t.Fatalf("unexpected TransactionUpdate: %+v", update)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("expected ResponseCh to close for NoSuccessNotify committed success")
	}
}

func TestProtocolInboxAdapter_CallReducer_DeliversFailureEvenWhenNoSuccessNotifySet(t *testing.T) {
	connID := types.ConnectionID{12}
	identity := types.Identity{13}
	respCh := make(chan protocol.TransactionUpdate, 1)
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			call := cmd.(CallReducerCmd)
			call.ProtocolResponseCh <- ProtocolCallReducerResponse{
				Reducer: ReducerResponse{Status: StatusFailedUser, Error: errors.New("boom")},
			}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	err := adapter.CallReducer(context.Background(), protocol.CallReducerRequest{
		ConnID:      connID,
		Identity:    identity,
		RequestID:   88,
		ReducerName: "LoudFailure",
		Flags:       protocol.CallReducerFlagsNoSuccessNotify,
		ResponseCh:  respCh,
	})
	if err != nil {
		t.Fatalf("CallReducer: %v", err)
	}

	select {
	case update := <-respCh:
		failed, ok := update.Status.(protocol.StatusFailed)
		if !ok {
			t.Fatalf("status = %T, want protocol.StatusFailed", update.Status)
		}
		if failed.Error != "boom" {
			t.Fatalf("failed.Error = %q, want boom", failed.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for TransactionUpdate")
	}
}

func TestProtocolInboxAdapter_ForwardReducerResponse_ClosedInternalChannelFails(t *testing.T) {
	respCh := make(chan ProtocolCallReducerResponse)
	close(respCh)
	req := protocol.CallReducerRequest{
		ConnID:      types.ConnectionID{17},
		Identity:    types.Identity{18},
		RequestID:   91,
		ReducerName: "ClosedInternalResponse",
		ResponseCh:  make(chan protocol.TransactionUpdate, 1),
	}
	adapter := &ProtocolInboxAdapter{}

	adapter.forwardReducerResponse(context.Background(), req, respCh)

	select {
	case update := <-req.ResponseCh:
		failed, ok := update.Status.(protocol.StatusFailed)
		if !ok {
			t.Fatalf("status = %T, want protocol.StatusFailed", update.Status)
		}
		if failed.Error != "executor reducer response channel closed" {
			t.Fatalf("failed.Error = %q, want closed-channel error", failed.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for closed-channel failure")
	}
}

func TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnContextCancelWhenOutboundBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	respCh := make(chan ProtocolCallReducerResponse, 1)
	req := protocol.CallReducerRequest{
		ConnID:      types.ConnectionID{21},
		Identity:    types.Identity{22},
		RequestID:   99,
		ReducerName: "BlockedForward",
		ResponseCh:  make(chan protocol.TransactionUpdate),
	}
	adapter := &ProtocolInboxAdapter{}
	done := make(chan struct{})

	go func() {
		adapter.forwardReducerResponse(ctx, req, respCh)
		close(done)
	}()

	respCh <- ProtocolCallReducerResponse{Reducer: ReducerResponse{Status: StatusFailedUser, Error: errors.New("boom")}}

	select {
	case <-done:
		t.Fatal("forwardReducerResponse returned before context cancellation while outbound channel was blocked")
	case <-time.After(25 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("forwardReducerResponse did not exit after context cancellation")
	}
}

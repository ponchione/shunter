package executor

import (
	"context"
	"errors"
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

func (s stubProtocolSchemaRegistry) TableByName(string) (*schema.TableSchema, bool) {
	return nil, false
}

func (s stubProtocolSchemaRegistry) Tables() []schema.TableID { return nil }
func (s stubProtocolSchemaRegistry) Reducer(string) (schema.ReducerHandler, bool) {
	return nil, false
}
func (s stubProtocolSchemaRegistry) Reducers() []string { return nil }
func (s stubProtocolSchemaRegistry) OnConnect() func(*schema.ReducerContext) error {
	return nil
}
func (s stubProtocolSchemaRegistry) OnDisconnect() func(*schema.ReducerContext) error {
	return nil
}
func (s stubProtocolSchemaRegistry) Version() uint32 { return 1 }

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
	adapter := newProtocolInboxAdapter(
		stubProtocolSubmitter{submit: func(_ context.Context, cmd ExecutorCommand) error {
			onConnect, ok := cmd.(OnConnectCmd)
			if !ok {
				t.Fatalf("command type = %T, want OnConnectCmd", cmd)
			}
			if onConnect.ConnID != connID || onConnect.Identity != identity {
				t.Fatalf("OnConnectCmd = %+v", onConnect)
			}
			onConnect.ResponseCh <- ReducerResponse{Status: StatusCommitted}
			return nil
		}},
		stubProtocolSchemaRegistry{},
	)

	if err := adapter.OnConnect(context.Background(), connID, identity); err != nil {
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

	if err := adapter.OnConnect(context.Background(), types.ConnectionID{1}, types.Identity{2}); err != nil {
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

	if err := adapter.OnDisconnect(context.Background(), types.ConnectionID{1}, types.Identity{2}); err != nil {
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

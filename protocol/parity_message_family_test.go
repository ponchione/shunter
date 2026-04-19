package protocol

import (
	"reflect"
	"testing"
)

// These tests are *pins*, not parity implementations. Each pins the
// current message-family shape so the divergence is either explicit or
// closed. The Phase 1.5 outcome-model decision flipped the
// TransactionUpdate / ReducerCallResult pins to assert the new heavy /
// light / `UpdateStatus` shape; see `docs/parity-phase1.5-outcome-model.md`.

// TestPhase15TransactionUpdateHeavyShape pins the Phase 1.5 adoption
// of the reference heavy `TransactionUpdate` envelope. Reference:
// `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`
// `pub struct TransactionUpdate<F: WebsocketFormat>`.
func TestPhase15TransactionUpdateHeavyShape(t *testing.T) {
	fields := msgFieldNames(TransactionUpdate{})
	want := []string{
		"Status",
		"CallerIdentity",
		"CallerConnectionID",
		"ReducerCall",
		"Timestamp",
		"EnergyQuantaUsed",
		"TotalHostExecutionDuration",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("TransactionUpdate fields = %v, want %v (Phase 1.5 heavy envelope)",
			fields, want)
	}
}

// TestPhase15TransactionUpdateLightShape pins the Phase 1.5 adoption
// of the reference delta-only envelope. Reference:
// `pub struct TransactionUpdateLight<F: WebsocketFormat>`.
func TestPhase15TransactionUpdateLightShape(t *testing.T) {
	fields := msgFieldNames(TransactionUpdateLight{})
	want := []string{"RequestID", "Update"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("TransactionUpdateLight fields = %v, want %v (Phase 1.5 light envelope)",
			fields, want)
	}
}

// TestPhase15ReducerCallInfoShape pins the embedded metadata carried by
// heavy `TransactionUpdate`. Reference: `pub struct ReducerCallInfo<F>`.
func TestPhase15ReducerCallInfoShape(t *testing.T) {
	fields := msgFieldNames(ReducerCallInfo{})
	want := []string{"ReducerName", "ReducerID", "Args", "RequestID"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("ReducerCallInfo fields = %v, want %v (Phase 1.5 reducer-call metadata)",
			fields, want)
	}
}

// TestPhase15UpdateStatusVariants pins the three-arm tagged-union
// `UpdateStatus`. Reference: `pub enum UpdateStatus<F> { Committed,
// Failed, OutOfEnergy }`. `OutOfEnergy` is present for shape parity but
// is never emitted by Shunter in Phase 1.5 — see
// `docs/parity-phase1.5-outcome-model.md`.
func TestPhase15UpdateStatusVariants(t *testing.T) {
	var _ UpdateStatus = StatusCommitted{}
	var _ UpdateStatus = StatusFailed{}
	var _ UpdateStatus = StatusOutOfEnergy{}

	if got := msgFieldNames(StatusCommitted{}); !reflect.DeepEqual(got, []string{"Update"}) {
		t.Errorf("StatusCommitted fields = %v, want [Update]", got)
	}
	if got := msgFieldNames(StatusFailed{}); !reflect.DeepEqual(got, []string{"Error"}) {
		t.Errorf("StatusFailed fields = %v, want [Error]", got)
	}
	if got := msgFieldNames(StatusOutOfEnergy{}); !reflect.DeepEqual(got, []string{}) {
		t.Errorf("StatusOutOfEnergy fields = %v, want []", got)
	}
}

// TestPhase15TagReducerCallResultReserved pins that
// `TagReducerCallResult` is reserved — no encoder emits it and the
// decoder rejects it. The tag byte is not reused so a future
// reintroduction cannot silently collide.
func TestPhase15TagReducerCallResultReserved(t *testing.T) {
	if TagReducerCallResult == 0 {
		t.Fatal("TagReducerCallResult should remain defined as a reserved value, not zero")
	}
	_, _, err := DecodeServerMessage([]byte{TagReducerCallResult})
	if err == nil {
		t.Errorf("DecodeServerMessage(TagReducerCallResult) succeeded, want unknown-tag error")
	}
}

// TestPhase2SubscribeSingleShape pins the renamed single-envelope. The
// QueryID field already landed; the rename closes the Single/Multi
// variant split on the client side. Reference: SubscribeSingle at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:189.
func TestPhase2SubscribeSingleShape(t *testing.T) {
	fields := msgFieldNames(SubscribeSingleMsg{})
	want := []string{"RequestID", "QueryID", "Query"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeSingleMsg fields = %v, want %v", fields, want)
	}
}

// TestPhase2UnsubscribeSingleShape pins the renamed single-envelope.
// Reference: Unsubscribe at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:218.
func TestPhase2UnsubscribeSingleShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeSingleMsg{})
	want := []string{"RequestID", "QueryID", "SendDropped"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeSingleMsg fields = %v, want %v", fields, want)
	}
}

// TestPhase2SubscribeSingleAppliedShape pins the renamed single-applied
// envelope. Reference: SubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:317.
func TestPhase2SubscribeSingleAppliedShape(t *testing.T) {
	fields := msgFieldNames(SubscribeSingleApplied{})
	want := []string{"RequestID", "QueryID", "TableName", "Rows"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeSingleApplied fields = %v, want %v", fields, want)
	}
}

// TestPhase2UnsubscribeSingleAppliedShape pins the renamed
// single-applied envelope. Reference: UnsubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:331.
func TestPhase2UnsubscribeSingleAppliedShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeSingleApplied{})
	want := []string{"RequestID", "QueryID", "HasRows", "Rows"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeSingleApplied fields = %v, want %v", fields, want)
	}
}

// TestPhase2SubscriptionErrorCarriesQueryID pins the response-side rename
// for `SubscriptionError`. Reference: `SubscriptionError.query_id: Option<u32>`.
// Shunter always populates QueryID in Phase 2 because every error is
// correlated with a specific query.
func TestPhase2SubscriptionErrorCarriesQueryID(t *testing.T) {
	fields := msgFieldNames(SubscriptionError{})
	want := []string{"RequestID", "QueryID", "Error"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscriptionError fields = %v, want %v (Phase 2: QueryID response envelope)",
			fields, want)
	}
}

// TestPhase15CallReducerFlagsField pins the Phase 1.5 sub-slice closure:
// reference/SpacetimeDB CallReducer carries a flags byte
// (CallReducerFlags::NoSuccessNotify) that lets callers opt out of the
// successful caller-echo. Shunter's CallReducerMsg now carries it too.
func TestPhase15CallReducerFlagsField(t *testing.T) {
	fields := msgFieldNames(CallReducerMsg{})
	want := []string{"RequestID", "ReducerName", "Args", "Flags"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("CallReducerMsg fields = %v, want %v (Phase 1.5: Flags field landed)",
			fields, want)
	}
}

// TestPhase1DeferralOneOffQueryStructuredNotSQL pins the deferral:
// reference uses a SQL string; Shunter uses structured predicates.
// Flip when the SQL front door lands (Phase 2 Slice 1).
func TestPhase1DeferralOneOffQueryStructuredNotSQL(t *testing.T) {
	fields := msgFieldNames(OneOffQueryMsg{})
	want := []string{"RequestID", "TableName", "Predicates"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("OneOffQueryMsg fields = %v, want %v (deferral: structured predicates, not SQL string)",
			fields, want)
	}
}

// TestPhase2SubscribeMultiShape pins the new Phase 2 Slice 2 envelope.
// Reference: SubscribeMulti { query_strings, request_id, query_id } at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:203.
// Shunter carries structured Queries (see
// TestPhase2DeferralSubscribeMultiQueriesStructured).
func TestPhase2SubscribeMultiShape(t *testing.T) {
	fields := msgFieldNames(SubscribeMultiMsg{})
	want := []string{"RequestID", "QueryID", "Queries"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeMultiMsg fields = %v, want %v (Phase 2 Slice 2 variant split)",
			fields, want)
	}
}

// TestPhase2UnsubscribeMultiShape pins the new Phase 2 Slice 2 envelope.
// Reference: UnsubscribeMulti { request_id, query_id } at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:229.
func TestPhase2UnsubscribeMultiShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeMultiMsg{})
	want := []string{"RequestID", "QueryID"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeMultiMsg fields = %v, want %v (Phase 2 Slice 2 variant split)",
			fields, want)
	}
}

// TestPhase2SubscribeMultiAppliedShape pins the set-scoped applied
// envelope. Reference: SubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:380.
// TotalHostExecutionDurationMicros is absent — tracked by
// TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration.
func TestPhase2SubscribeMultiAppliedShape(t *testing.T) {
	fields := msgFieldNames(SubscribeMultiApplied{})
	want := []string{"RequestID", "QueryID", "Update"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeMultiApplied fields = %v, want %v", fields, want)
	}
}

// TestPhase2UnsubscribeMultiAppliedShape pins the set-scoped applied
// envelope. Reference: UnsubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:394.
func TestPhase2UnsubscribeMultiAppliedShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeMultiApplied{})
	want := []string{"RequestID", "QueryID", "Update"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeMultiApplied fields = %v, want %v", fields, want)
	}
}

// TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration pins the
// still-open deferral: reference carries
// total_host_execution_duration_micros: u64 on SubscribeApplied,
// SubscribeMultiApplied, UnsubscribeApplied, UnsubscribeMultiApplied
// (v1.rs:321/335/384/399). Shunter does not. Flip when the host
// execution duration slice lands.
func TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration(t *testing.T) {
	for _, v := range []any{
		SubscribeSingleApplied{},
		SubscribeMultiApplied{},
		UnsubscribeSingleApplied{},
		UnsubscribeMultiApplied{},
	} {
		for _, f := range msgFieldNames(v) {
			if f == "TotalHostExecutionDurationMicros" {
				t.Fatalf("%T.TotalHostExecutionDurationMicros unexpectedly present", v)
			}
		}
	}
}

// TestPhase2DeferralSubscriptionErrorNoTableID pins the three-field
// shape. Reference SubscriptionError carries
// total_host_execution_duration_micros, Option<request_id>,
// Option<query_id>, Option<TableId>, error (v1.rs:350). Shunter
// always populates RequestID/QueryID and omits TableID + duration.
// Flip when any of these close.
func TestPhase2DeferralSubscriptionErrorNoTableID(t *testing.T) {
	fields := msgFieldNames(SubscriptionError{})
	want := []string{"RequestID", "QueryID", "Error"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscriptionError fields = %v, want %v (deferral)",
			fields, want)
	}
}

// TestPhase2DeferralSubscribeMultiQueriesStructured pins the scope
// boundary with Phase 2 Slice 1. Reference SubscribeMulti carries
// query_strings: Box<[Box<str>]> (v1.rs:205); Shunter carries
// structured Queries []Query. Flip when the SQL front door lands.
func TestPhase2DeferralSubscribeMultiQueriesStructured(t *testing.T) {
	m := SubscribeMultiMsg{}
	qf, ok := reflect.TypeOf(m).FieldByName("Queries")
	if !ok {
		t.Fatal("SubscribeMultiMsg.Queries missing")
	}
	if qf.Type.Elem().Name() != "Query" {
		t.Fatalf("SubscribeMultiMsg.Queries elem = %s, want Query (structured)",
			qf.Type.Elem().Name())
	}
}

// TestPhase2TagByteStability pins the Phase 2 Slice 2 tag layout.
// Older bytes (1-8) stay fixed; 9/10 are the new multi-applied tags.
// 5/6 are the new multi request tags.
func TestPhase2TagByteStability(t *testing.T) {
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"TagSubscribeSingle", TagSubscribeSingle, 1},
		{"TagUnsubscribeSingle", TagUnsubscribeSingle, 2},
		{"TagCallReducer", TagCallReducer, 3},
		{"TagOneOffQuery", TagOneOffQuery, 4},
		{"TagSubscribeMulti", TagSubscribeMulti, 5},
		{"TagUnsubscribeMulti", TagUnsubscribeMulti, 6},
		{"TagInitialConnection", TagInitialConnection, 1},
		{"TagSubscribeSingleApplied", TagSubscribeSingleApplied, 2},
		{"TagUnsubscribeSingleApplied", TagUnsubscribeSingleApplied, 3},
		{"TagSubscriptionError", TagSubscriptionError, 4},
		{"TagTransactionUpdate", TagTransactionUpdate, 5},
		{"TagOneOffQueryResult", TagOneOffQueryResult, 6},
		{"TagReducerCallResult", TagReducerCallResult, 7},
		{"TagTransactionUpdateLight", TagTransactionUpdateLight, 8},
		{"TagSubscribeMultiApplied", TagSubscribeMultiApplied, 9},
		{"TagUnsubscribeMultiApplied", TagUnsubscribeMultiApplied, 10},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func msgFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names = append(names, t.Field(i).Name)
	}
	return names
}

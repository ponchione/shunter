package protocol

import (
	"reflect"
	"testing"

	"github.com/ponchione/shunter/schema"
)

// These tests are *pins*, not parity implementations. Each pins the
// current message-family shape so the divergence is either explicit or
// closed. The Phase 1.5 outcome-model decision flipped the
// TransactionUpdate / ReducerCallResult pins to assert the new heavy /
// light / `UpdateStatus` shape; see `docs/parity-decisions.md#outcome-model`.

// TestPhase15TransactionUpdateHeavyShape pins the Shunter-native heavy
// `TransactionUpdate` envelope. The wire byte shape is pinned separately in
// parity_transaction_update_test.go.
func TestPhase15TransactionUpdateHeavyShape(t *testing.T) {
	fields := msgFieldNames(TransactionUpdate{})
	want := []string{
		"Status",
		"Timestamp",
		"CallerIdentity",
		"CallerConnectionID",
		"ReducerCall",
		"TotalHostExecutionDuration",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("TransactionUpdate fields = %v, want %v",
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

// TestPhase15UpdateStatusVariants pins the two-arm Shunter-native
// `UpdateStatus` tagged union.
func TestPhase15UpdateStatusVariants(t *testing.T) {
	var _ UpdateStatus = StatusCommitted{}
	var _ UpdateStatus = StatusFailed{}

	if got := msgFieldNames(StatusCommitted{}); !reflect.DeepEqual(got, []string{"Update"}) {
		t.Errorf("StatusCommitted fields = %v, want [Update]", got)
	}
	if got := msgFieldNames(StatusFailed{}); !reflect.DeepEqual(got, []string{"Error"}) {
		t.Errorf("StatusFailed fields = %v, want [Error]", got)
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

// TestPhase2SubscribeSingleShape pins the Phase 2 Slice 1 SQL-string
// shape. Reference: SubscribeSingle { query: Box<str>, request_id,
// query_id } at reference/SpacetimeDB/crates/client-api-messages/src/
// websocket/v1.rs:189. The structured `Query` form was flipped to a
// `QueryString` in Phase 2 Slice 1.
func TestPhase2SubscribeSingleShape(t *testing.T) {
	fields := msgFieldNames(SubscribeSingleMsg{})
	want := []string{"RequestID", "QueryID", "QueryString"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeSingleMsg fields = %v, want %v (Phase 2 Slice 1 SQL-string flip)", fields, want)
	}
}

// TestPhase2UnsubscribeSingleShape pins the reference field order.
// Reference: Unsubscribe at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:218
// (`{ request_id: u32, query_id: QueryId }`). Byte shape is pinned in
// parity_unsubscribe_test.go. The prior extra `SendDropped` byte — a
// Shunter-local smuggle of the v2 `UnsubscribeFlags::SendDroppedRows`
// concept — has been removed to match reference v1.
func TestPhase2UnsubscribeSingleShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeSingleMsg{})
	want := []string{"RequestID", "QueryID"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeSingleMsg fields = %v, want %v", fields, want)
	}
}

// TestPhase2SubscribeSingleAppliedShape pins the reference field order.
// Reference: SubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:317
// (`request_id, total_host_execution_duration_micros, query_id, rows`).
// Duration sits at position 2. Byte shape is pinned in
// parity_applied_envelopes_test.go. The flattened TableName + Rows
// shape is a documented divergence per
// `docs/parity-decisions.md#protocol-rows-shape` — the reference wraps
// them in `SubscribeRows`.
func TestPhase2SubscribeSingleAppliedShape(t *testing.T) {
	fields := msgFieldNames(SubscribeSingleApplied{})
	want := []string{"RequestID", "TotalHostExecutionDurationMicros", "QueryID", "TableName", "Rows"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeSingleApplied fields = %v, want %v (reference field order)", fields, want)
	}
}

// TestPhase2UnsubscribeSingleAppliedShape pins the reference field order.
// Reference: UnsubscribeApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:331
// (`request_id, total_host_execution_duration_micros, query_id, rows`).
// Duration sits at position 2. Byte shape is pinned in
// parity_applied_envelopes_test.go. HasRows + Rows diverges from
// the reference required `SubscribeRows` wrapper — documented per
// `docs/parity-decisions.md#protocol-rows-shape`.
func TestPhase2UnsubscribeSingleAppliedShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeSingleApplied{})
	want := []string{"RequestID", "TotalHostExecutionDurationMicros", "QueryID", "HasRows", "Rows"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeSingleApplied fields = %v, want %v (reference field order)", fields, want)
	}
}

// TestPhase15CallReducerFlagsField pins the reference `CallReducer<Args>`
// field order from
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:110
// (`reducer, args, request_id, flags`). The wire byte shape is pinned
// separately in parity_call_reducer_test.go.
func TestPhase15CallReducerFlagsField(t *testing.T) {
	fields := msgFieldNames(CallReducerMsg{})
	want := []string{"ReducerName", "Args", "RequestID", "Flags"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("CallReducerMsg fields = %v, want %v (reference field order)",
			fields, want)
	}
}

// TestPhase2Slice1COneOffQueryMessageIDBytes pins the Phase 2 Slice 1c
// wire-shape parity flip: reference OneOffQuery carries
// `message_id: Box<[u8]>, query_string: Box<str>` and the paired result
// envelope correlates with the same opaque bytes. Shunter must therefore
// expose `MessageID []byte` on both request and response envelopes rather
// than a numeric RequestID.
func TestPhase2Slice1COneOffQueryMessageIDBytes(t *testing.T) {
	msgFields := msgFieldNames(OneOffQueryMsg{})
	if want := []string{"MessageID", "QueryString"}; !reflect.DeepEqual(msgFields, want) {
		t.Fatalf("OneOffQueryMsg fields = %v, want %v (Phase 2 Slice 1c message_id bytes)", msgFields, want)
	}
	msgField, ok := reflect.TypeOf(OneOffQueryMsg{}).FieldByName("MessageID")
	if !ok {
		t.Fatal("OneOffQueryMsg.MessageID missing")
	}
	if got := msgField.Type.String(); got != "[]uint8" {
		t.Fatalf("OneOffQueryMsg.MessageID type = %s, want []byte", got)
	}

	resultFields := msgFieldNames(OneOffQueryResponse{})
	if want := []string{"MessageID", "Error", "Tables", "TotalHostExecutionDuration"}; !reflect.DeepEqual(resultFields, want) {
		t.Fatalf("OneOffQueryResponse fields = %v, want %v (reference field order v1.rs:654)", resultFields, want)
	}
	resultField, ok := reflect.TypeOf(OneOffQueryResponse{}).FieldByName("MessageID")
	if !ok {
		t.Fatal("OneOffQueryResponse.MessageID missing")
	}
	if got := resultField.Type.String(); got != "[]uint8" {
		t.Fatalf("OneOffQueryResponse.MessageID type = %s, want []byte", got)
	}
}

// TestPhase2SubscribeMultiShape pins the Phase 2 Slice 1 SQL-string
// list. Reference: SubscribeMulti { query_strings: Box<[Box<str>]>,
// request_id, query_id } at reference/SpacetimeDB/crates/
// client-api-messages/src/websocket/v1.rs:203. The structured Queries
// list was flipped to QueryStrings in Phase 2 Slice 1.
func TestPhase2SubscribeMultiShape(t *testing.T) {
	fields := msgFieldNames(SubscribeMultiMsg{})
	want := []string{"RequestID", "QueryID", "QueryStrings"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeMultiMsg fields = %v, want %v (Phase 2 Slice 1 SQL-string flip)",
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

// TestPhase2SubscribeMultiAppliedShape pins the reference field order.
// Reference: SubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:380
// (`request_id, total_host_execution_duration_micros, query_id, update`).
// Duration sits at position 2. Byte shape is pinned in
// parity_applied_envelopes_test.go. Update flattens the reference
// `DatabaseUpdate` wrapper — documented per
// `docs/parity-decisions.md#protocol-rows-shape`.
func TestPhase2SubscribeMultiAppliedShape(t *testing.T) {
	fields := msgFieldNames(SubscribeMultiApplied{})
	want := []string{"RequestID", "TotalHostExecutionDurationMicros", "QueryID", "Update"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeMultiApplied fields = %v, want %v (reference field order)", fields, want)
	}
}

// TestPhase2UnsubscribeMultiAppliedShape pins the reference field order.
// Reference: UnsubscribeMultiApplied at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:394
// (`request_id, total_host_execution_duration_micros, query_id, update`).
// Duration sits at position 2. Byte shape is pinned in
// parity_applied_envelopes_test.go. Update flattens the reference
// `DatabaseUpdate` wrapper — documented per
// `docs/parity-decisions.md#protocol-rows-shape`.
func TestPhase2UnsubscribeMultiAppliedShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeMultiApplied{})
	want := []string{"RequestID", "TotalHostExecutionDurationMicros", "QueryID", "Update"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeMultiApplied fields = %v, want %v (reference field order)", fields, want)
	}
}

// TestPhase2SubscribeAppliedCarriesHostExecutionDuration pins the
// reference-style host execution duration on all four applied envelopes.
func TestPhase2SubscribeAppliedCarriesHostExecutionDuration(t *testing.T) {
	for _, v := range []any{
		SubscribeSingleApplied{},
		SubscribeMultiApplied{},
		UnsubscribeSingleApplied{},
		UnsubscribeMultiApplied{},
	} {
		found := false
		for _, f := range msgFieldNames(v) {
			if f == "TotalHostExecutionDurationMicros" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%T missing TotalHostExecutionDurationMicros", v)
		}
	}
}

// TestPhase2SubscriptionErrorOptionalShape pins the narrowed
// SubscriptionError follow-through: request_id / query_id are now
// explicit optionals and table_id is present on the Go envelope.
// `TotalHostExecutionDurationMicros` is the reference-position first
// field (v1.rs:350); live emit sites now populate a measured non-zero
// microsecond duration via the receipt-timestamp seam. See
// `parity_subscription_error_test.go` for the byte-shape pin.
func TestPhase2SubscriptionErrorOptionalShape(t *testing.T) {
	fields := msgFieldNames(SubscriptionError{})
	want := []string{"TotalHostExecutionDurationMicros", "RequestID", "QueryID", "TableID", "Error"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscriptionError fields = %v, want %v", fields, want)
	}

	typ := reflect.TypeOf(SubscriptionError{})
	requestField, ok := typ.FieldByName("RequestID")
	if !ok {
		t.Fatal("SubscriptionError.RequestID missing")
	}
	if requestField.Type.Kind() != reflect.Pointer || requestField.Type.Elem().Kind() != reflect.Uint32 {
		t.Fatalf("SubscriptionError.RequestID type = %s, want *uint32", requestField.Type)
	}
	queryField, ok := typ.FieldByName("QueryID")
	if !ok {
		t.Fatal("SubscriptionError.QueryID missing")
	}
	if queryField.Type.Kind() != reflect.Pointer || queryField.Type.Elem().Kind() != reflect.Uint32 {
		t.Fatalf("SubscriptionError.QueryID type = %s, want *uint32", queryField.Type)
	}
	tableField, ok := typ.FieldByName("TableID")
	if !ok {
		t.Fatal("SubscriptionError.TableID missing")
	}
	if tableField.Type.Kind() != reflect.Pointer || tableField.Type.Elem() != reflect.TypeOf(schema.TableID(0)) {
		t.Fatalf("SubscriptionError.TableID type = %s, want *schema.TableID", tableField.Type)
	}
}

// TestPhase2Slice1SubscribeMultiQueryStringsList pins the positive
// shape after Phase 2 Slice 1: `query_strings: Box<[Box<str>]>` on the
// reference (v1.rs:205) maps to a Go `[]string` carrying SQL strings.
func TestPhase2Slice1SubscribeMultiQueryStringsList(t *testing.T) {
	m := SubscribeMultiMsg{}
	qf, ok := reflect.TypeOf(m).FieldByName("QueryStrings")
	if !ok {
		t.Fatal("SubscribeMultiMsg.QueryStrings missing")
	}
	if qf.Type.Kind().String() != "slice" || qf.Type.Elem().Kind().String() != "string" {
		t.Fatalf("SubscribeMultiMsg.QueryStrings type = %s, want []string",
			qf.Type.String())
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
		{"TagIdentityToken", TagIdentityToken, 1},
		{"TagSubscribeSingleApplied", TagSubscribeSingleApplied, 2},
		{"TagUnsubscribeSingleApplied", TagUnsubscribeSingleApplied, 3},
		{"TagSubscriptionError", TagSubscriptionError, 4},
		{"TagTransactionUpdate", TagTransactionUpdate, 5},
		{"TagOneOffQueryResponse", TagOneOffQueryResponse, 6},
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

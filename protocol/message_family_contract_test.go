package protocol

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/ponchione/shunter/schema"
)

// These tests are *pins*, not compatibility implementations. Each pins the
// current message-family shape so the divergence is either explicit or
// closed. The Outcome-model decision flipped the
// TransactionUpdate / ReducerCallResult pins to assert the new heavy /
// light / `UpdateStatus` shape; see `working-docs/shunter-design-decisions.md#outcome-model`.

// TestShunterTransactionUpdateHeavyShape pins the Shunter-native heavy
// `TransactionUpdate` envelope. The wire byte shape is pinned separately in
// transaction_update_wire_test.go.
func TestShunterTransactionUpdateHeavyShape(t *testing.T) {
	fields := msgFieldNames(TransactionUpdate{})
	want := []string{
		"Status",
		"Timestamp",
		"CallerIdentity",
		"CallerConnectionID",
		"ReducerCall",
		"TotalHostExecutionDuration",
	}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Errorf("TransactionUpdate fields mismatch (-want +got):\n%s", diff)
	}
}

// TestShunterReducerCallInfoShape pins the embedded metadata carried by
// heavy `TransactionUpdate`. Reference: `pub struct ReducerCallInfo<F>`.
func TestShunterReducerCallInfoShape(t *testing.T) {
	fields := msgFieldNames(ReducerCallInfo{})
	want := []string{"ReducerName", "ReducerID", "Args", "RequestID"}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Errorf("ReducerCallInfo fields mismatch (-want +got):\n%s", diff)
	}
}

// TestShunterUpdateStatusVariants pins the two-arm Shunter-native
// `UpdateStatus` tagged union.
func TestShunterUpdateStatusVariants(t *testing.T) {
	var _ UpdateStatus = StatusCommitted{}
	var _ UpdateStatus = StatusFailed{}

	if diff := cmp.Diff([]string{"Update"}, msgFieldNames(StatusCommitted{})); diff != "" {
		t.Errorf("StatusCommitted fields mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Error"}, msgFieldNames(StatusFailed{})); diff != "" {
		t.Errorf("StatusFailed fields mismatch (-want +got):\n%s", diff)
	}
}

// TestShunterTagReducerCallResultReserved pins that
// `TagReducerCallResult` is reserved — no encoder emits it and the
// decoder rejects it. The tag byte is not reused so a future
// reintroduction cannot silently collide.
func TestShunterTagReducerCallResultReserved(t *testing.T) {
	if TagReducerCallResult == 0 {
		t.Fatal("TagReducerCallResult should remain defined as a reserved value, not zero")
	}
	_, _, err := DecodeServerMessage([]byte{TagReducerCallResult})
	if err == nil {
		t.Errorf("DecodeServerMessage(TagReducerCallResult) succeeded, want unknown-tag error")
	}
}

// TestShunterSubscribeSingleShape pins the SQL-string SQL-string
// shape. Reference: SubscribeSingle { query: Box<str>, request_id,
// query_id } at reference/SpacetimeDB/crates/client-api-messages/src/
// websocket/v1.rs:189. The structured `Query` form was flipped to a
// `QueryString` in SQL-string.
func TestShunterSubscribeSingleShape(t *testing.T) {
	fields := msgFieldNames(SubscribeSingleMsg{})
	want := []string{"RequestID", "QueryID", "QueryString"}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Fatalf("SubscribeSingleMsg fields mismatch (-want +got):\n%s", diff)
	}
}

// TestShunterUnsubscribeSingleShape pins the reference field order.
// Reference: Unsubscribe at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:218
// (`{ request_id: u32, query_id: QueryId }`). Byte shape is pinned in
// unsubscribe_wire_test.go. The prior extra `SendDropped` byte — a
// Shunter-local smuggle of the v2 `UnsubscribeFlags::SendDroppedRows`
// concept — has been removed to match reference v1.
func TestShunterUnsubscribeSingleShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeSingleMsg{})
	want := []string{"RequestID", "QueryID"}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Fatalf("UnsubscribeSingleMsg fields mismatch (-want +got):\n%s", diff)
	}
}

// TestShunterCallReducerFlagsField pins the reference `CallReducer<Args>`
// field order from
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:110
// (`reducer, args, request_id, flags`). The wire byte shape is pinned
// separately in call_reducer_wire_test.go.
func TestShunterCallReducerFlagsField(t *testing.T) {
	fields := msgFieldNames(CallReducerMsg{})
	want := []string{"ReducerName", "Args", "RequestID", "Flags"}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Fatalf("CallReducerMsg fields mismatch (-want +got):\n%s", diff)
	}
}

// TestShunterOneOffQueryMessageIDBytes pins the one-off message-id
// wire-shape contract change: reference OneOffQuery carries
// `message_id: Box<[u8]>, query_string: Box<str>` and the paired result
// envelope correlates with the same opaque bytes. Shunter must therefore
// expose `MessageID []byte` on both request and response envelopes rather
// than a numeric RequestID.
func TestShunterOneOffQueryMessageIDBytes(t *testing.T) {
	msgFields := msgFieldNames(OneOffQueryMsg{})
	wantMsgFields := []string{"MessageID", "QueryString"}
	if diff := cmp.Diff(wantMsgFields, msgFields); diff != "" {
		t.Fatalf("OneOffQueryMsg fields mismatch (-want +got):\n%s", diff)
	}
	msgField, ok := reflect.TypeOf(OneOffQueryMsg{}).FieldByName("MessageID")
	if !ok {
		t.Fatal("OneOffQueryMsg.MessageID missing")
	}
	if got := msgField.Type.String(); got != "[]uint8" {
		t.Fatalf("OneOffQueryMsg.MessageID type = %s, want []byte", got)
	}

	resultFields := msgFieldNames(OneOffQueryResponse{})
	wantResultFields := []string{"MessageID", "Error", "Tables", "TotalHostExecutionDuration"}
	if diff := cmp.Diff(wantResultFields, resultFields); diff != "" {
		t.Fatalf("OneOffQueryResponse fields mismatch (-want +got):\n%s", diff)
	}
	resultField, ok := reflect.TypeOf(OneOffQueryResponse{}).FieldByName("MessageID")
	if !ok {
		t.Fatal("OneOffQueryResponse.MessageID missing")
	}
	if got := resultField.Type.String(); got != "[]uint8" {
		t.Fatalf("OneOffQueryResponse.MessageID type = %s, want []byte", got)
	}
}

// TestShunterSubscribeMultiShape pins the SQL-string SQL-string
// list. Reference: SubscribeMulti { query_strings: Box<[Box<str>]>,
// request_id, query_id } at reference/SpacetimeDB/crates/
// client-api-messages/src/websocket/v1.rs:203. The structured Queries
// list was flipped to QueryStrings in SQL-string.
func TestShunterSubscribeMultiShape(t *testing.T) {
	fields := msgFieldNames(SubscribeMultiMsg{})
	want := []string{"RequestID", "QueryID", "QueryStrings"}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Fatalf("SubscribeMultiMsg fields mismatch (-want +got):\n%s", diff)
	}
}

func TestShunterParameterizedDeclaredReadShapes(t *testing.T) {
	queryFields := msgFieldNames(DeclaredQueryWithParametersMsg{})
	if diff := cmp.Diff([]string{"MessageID", "Name", "Params"}, queryFields); diff != "" {
		t.Fatalf("DeclaredQueryWithParametersMsg fields mismatch (-want +got):\n%s", diff)
	}

	viewFields := msgFieldNames(SubscribeDeclaredViewWithParametersMsg{})
	if diff := cmp.Diff([]string{"RequestID", "QueryID", "Name", "Params"}, viewFields); diff != "" {
		t.Fatalf("SubscribeDeclaredViewWithParametersMsg fields mismatch (-want +got):\n%s", diff)
	}
}

// TestShunterUnsubscribeMultiShape pins the new single/multi variant envelope.
// Reference: UnsubscribeMulti { request_id, query_id } at
// reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:229.
func TestShunterUnsubscribeMultiShape(t *testing.T) {
	fields := msgFieldNames(UnsubscribeMultiMsg{})
	want := []string{"RequestID", "QueryID"}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Fatalf("UnsubscribeMultiMsg fields mismatch (-want +got):\n%s", diff)
	}
}

// TestShunterSubscribeAppliedCarriesHostExecutionDuration pins the
// reference-style host execution duration on all four applied envelopes.
func TestShunterSubscribeAppliedCarriesHostExecutionDuration(t *testing.T) {
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

// TestShunterSubscriptionErrorOptionalShape pins the narrowed
// SubscriptionError follow-through: request_id / query_id are now
// explicit optionals and table_id is present on the Go envelope.
// `TotalHostExecutionDurationMicros` is the reference-position first
// field (v1.rs:350); live emit sites now populate a measured non-zero
// microsecond duration via the receipt-timestamp seam. See
// `subscription_error_wire_test.go` for the byte-shape pin.
func TestShunterSubscriptionErrorOptionalShape(t *testing.T) {
	fields := msgFieldNames(SubscriptionError{})
	want := []string{"TotalHostExecutionDurationMicros", "RequestID", "QueryID", "TableID", "Error"}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Fatalf("SubscriptionError fields mismatch (-want +got):\n%s", diff)
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

// TestShunterSlice1SubscribeMultiQueryStringsList pins the positive
// shape after SQL-string: `query_strings: Box<[Box<str>]>` on the
// reference (v1.rs:205) maps to a Go `[]string` carrying SQL strings.
func TestShunterSlice1SubscribeMultiQueryStringsList(t *testing.T) {
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

// TestShunterTagByteStability pins the single/multi variant tag layout.
// Older bytes (1-8) stay fixed; 9/10 are the new multi-applied tags.
// 5/6 are the new multi request tags.
func TestShunterTagByteStability(t *testing.T) {
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
		{"TagDeclaredQuery", TagDeclaredQuery, 7},
		{"TagSubscribeDeclaredView", TagSubscribeDeclaredView, 8},
		{"TagDeclaredQueryWithParameters", TagDeclaredQueryWithParameters, 9},
		{"TagSubscribeDeclaredViewWithParameters", TagSubscribeDeclaredViewWithParameters, 10},
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

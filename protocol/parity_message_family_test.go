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

// TestPhase2SubscribeCarriesQueryID pins the Phase 2 Slice 2 opener:
// SubscribeMsg now carries a QueryID field matching the reference
// `query_id: QueryId` on `SubscribeSingle` / `SubscribeMulti`. The
// Multi / Single variant split remains deferred — see
// TestPhase2DeferralSubscribeNoMultiOrSingleVariants below.
func TestPhase2SubscribeCarriesQueryID(t *testing.T) {
	fields := msgFieldNames(SubscribeMsg{})
	want := []string{"RequestID", "QueryID", "Query"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("SubscribeMsg fields = %v, want %v (Phase 2: QueryID field landed)",
			fields, want)
	}
}

// TestPhase2UnsubscribeCarriesQueryID pins the Phase 2 Slice 2 opener
// on Unsubscribe: reference `Unsubscribe` carries `query_id: QueryId`.
func TestPhase2UnsubscribeCarriesQueryID(t *testing.T) {
	fields := msgFieldNames(UnsubscribeMsg{})
	want := []string{"RequestID", "QueryID", "SendDropped"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("UnsubscribeMsg fields = %v, want %v (Phase 2: QueryID field landed)",
			fields, want)
	}
}

// TestPhase2DeferralSubscribeNoMultiOrSingleVariants pins the still-open
// deferral: reference exposes `SubscribeSingle` / `SubscribeMulti` as
// separate envelopes with batch registration semantics. Shunter still
// exposes a single Subscribe envelope per query. Flip when the
// Multi / Single split lands.
func TestPhase2DeferralSubscribeNoMultiOrSingleVariants(t *testing.T) {
	if TagSubscribe == 0 {
		t.Fatal("TagSubscribe should stay defined for the single-envelope path")
	}
	// No SubscribeMultiMsg / SubscribeSingleMsg types exist yet. When
	// they land, replace this test with positive shape pins on those
	// envelopes.
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

func msgFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names = append(names, t.Field(i).Name)
	}
	return names
}

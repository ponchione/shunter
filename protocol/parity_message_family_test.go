package protocol

import (
	"reflect"
	"testing"
)

// These tests are *pins*, not parity implementations. Each pins the
// current Phase 1 deferral so the divergence is explicit and a later
// phase can flip the test when the divergence is closed. Each test
// documents the reference outcome in its own comment; the test body
// asserts the current (not the target) shape.

// TestPhase1DeferralTransactionUpdateNoHeavyLightSplit pins the
// intentional deferral: Shunter has a single TransactionUpdate shape,
// whereas reference/SpacetimeDB distinguishes TransactionUpdate (heavy)
// vs TransactionUpdateLight (delta-only). Flip this test when the
// split lands.
func TestPhase1DeferralTransactionUpdateNoHeavyLightSplit(t *testing.T) {
	fields := msgFieldNames(TransactionUpdate{})
	// Current shape: TxID, Updates. No caller-side reducer metadata
	// on the wire object.
	want := []string{"TxID", "Updates"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("TransactionUpdate fields = %v, want %v (reference has heavy/light split — deferral)",
			fields, want)
	}
}

// TestPhase1DeferralSubscribeNoQueryIdOrMultiVariants pins the
// deferral: Shunter uses a single Subscribe message with a structured
// query list; reference/SpacetimeDB exposes SubscribeSingle /
// SubscribeMulti variants with a QueryId. Flip when grouping lands.
//
// Note: Shunter's type is SubscribeMsg (not SubscribeMessage) — the
// plan guessed SubscribeMessage; the real name is SubscribeMsg.
func TestPhase1DeferralSubscribeNoQueryIdOrMultiVariants(t *testing.T) {
	if TagSubscribe != 1 {
		t.Errorf("TagSubscribe = %d, want 1 (Phase 1 deferral: no Multi/Single split)",
			TagSubscribe)
	}
	fields := msgFieldNames(SubscribeMsg{})
	if containsMsg(fields, "QueryId") {
		t.Fatal("SubscribeMsg has QueryId — deferral has been closed; update this test and the P0-PROTOCOL-004 ledger row")
	}
}

// TestPhase1DeferralCallReducerNoFlagsField pins the deferral:
// reference/SpacetimeDB CallReducer carries a flags field
// (e.g., NoSuccessfulUpdate). Shunter does not. Flip when flags land.
//
// Note: Shunter's type is CallReducerMsg (not CallReducerMessage) — the
// plan guessed CallReducerMessage; the real name is CallReducerMsg.
func TestPhase1DeferralCallReducerNoFlagsField(t *testing.T) {
	fields := msgFieldNames(CallReducerMsg{})
	if containsMsg(fields, "Flags") {
		t.Fatal("CallReducerMsg has Flags — deferral closed; update this test and the P0-PROTOCOL-004 ledger row")
	}
}

// TestPhase1DeferralOneOffQueryStructuredNotSQL pins the deferral:
// reference uses a SQL string; Shunter uses structured predicates.
// Flip when the SQL front door lands (Phase 2 Slice 1).
//
// Note: Shunter's type is OneOffQueryMsg (not OneOffQueryMessage) — the
// plan guessed OneOffQueryMessage; the real name is OneOffQueryMsg.
func TestPhase1DeferralOneOffQueryStructuredNotSQL(t *testing.T) {
	fields := msgFieldNames(OneOffQueryMsg{})
	if containsMsg(fields, "QueryString") || containsMsg(fields, "SQL") {
		t.Fatal("OneOffQueryMsg carries a SQL string — deferral closed; update this test and the P0-PROTOCOL-004 ledger row")
	}
}

// TestPhase1DeferralReducerCallResultFlatStatus pins the deferral:
// reference uses a tagged union (UpdateStatus); Shunter uses a flat
// uint8. Flip when the enum is restructured.
func TestPhase1DeferralReducerCallResultFlatStatus(t *testing.T) {
	var r ReducerCallResult
	if reflect.TypeOf(r.Status).Kind() != reflect.Uint8 {
		t.Errorf("ReducerCallResult.Status kind = %v, want Uint8 (deferral)",
			reflect.TypeOf(r.Status).Kind())
	}
}

func msgFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names[i] = t.Field(i).Name
	}
	return names
}

func containsMsg(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

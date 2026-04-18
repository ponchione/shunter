package protocol

import (
	"testing"
)

// TestPhase1ParityReferenceSubprotocolAccepted locks the Phase 1
// parity decision: the upgrade handler admits a client that offers the
// SpacetimeDB reference subprotocol token "v1.bsatn.spacetimedb", and
// returns that exact token as the selected subprotocol.
//
// Reference outcome matched: reference/SpacetimeDB subprotocol token
// v1.bsatn.spacetimedb declared in
// crates/client-api-messages/src/websocket/v1.rs (ref constant).
func TestPhase1ParityReferenceSubprotocolAccepted(t *testing.T) {
	s, _ := anonymousServer(t)
	srv := newTestServer(t, s)

	conn, resp, err := dialWS(t, srv, wsDialOpts{
		subprotocols: []string{SubprotocolReference},
	})
	if err != nil {
		t.Fatalf("dial with reference subprotocol: %v (resp=%v)", err, resp)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != SubprotocolReference {
		t.Fatalf("server selected subprotocol = %q, want %q",
			got, SubprotocolReference)
	}
}

// TestPhase1ParityLegacyShunterSubprotocolStillAccepted pins the
// intentional deferral: the Shunter-native token "v1.bsatn.shunter"
// remains accepted so existing clients do not break. Update this test
// when the retention window closes.
func TestPhase1ParityLegacyShunterSubprotocolStillAccepted(t *testing.T) {
	s, _ := anonymousServer(t)
	srv := newTestServer(t, s)

	conn, resp, err := dialWS(t, srv, wsDialOpts{
		subprotocols: []string{SubprotocolV1},
	})
	if err != nil {
		t.Fatalf("dial with legacy subprotocol: %v (resp=%v)", err, resp)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != SubprotocolV1 {
		t.Fatalf("server selected subprotocol = %q, want %q",
			got, SubprotocolV1)
	}
}

// TestPhase1ParityReferenceSubprotocolPreferred verifies that when a
// client offers both tokens, the server selects the reference token
// (preferred in acceptedSubprotocols order).
func TestPhase1ParityReferenceSubprotocolPreferred(t *testing.T) {
	s, _ := anonymousServer(t)
	srv := newTestServer(t, s)

	conn, resp, err := dialWS(t, srv, wsDialOpts{
		subprotocols: []string{SubprotocolV1, SubprotocolReference},
	})
	if err != nil {
		t.Fatalf("dial with both subprotocols: %v (resp=%v)", err, resp)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != SubprotocolReference {
		t.Fatalf("server selected subprotocol = %q, want %q (reference should be preferred)",
			got, SubprotocolReference)
	}
}

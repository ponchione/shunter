package protocol

import (
	"testing"
)

// TestPhase1ParityReferenceSubprotocolAccepted pins the historical
// compatibility behavior: the upgrade handler still admits a client
// that offers the SpacetimeDB reference subprotocol token
// "v1.bsatn.spacetimedb" and returns that exact token as selected.
// This is not a current Shunter product-compatibility target.
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
// Shunter-native token "v1.bsatn.shunter". This is the token new
// Shunter-owned clients should offer.
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

// TestPhase1ParityReferenceSubprotocolPreferred pins the current
// selection order when a client offers both tokens. This is historical
// behavior and may change in a Shunter-native cleanup slice.
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

package protocol

import "testing"

const referenceSubprotocolToken = "v1.bsatn.spacetimedb"

func TestSubprotocolReferenceTokenRejected(t *testing.T) {
	s, _ := anonymousServer(t)
	srv := newTestServer(t, s)

	_, resp, err := dialWS(t, srv, wsDialOpts{
		subprotocols: []string{referenceSubprotocolToken},
	})
	if err == nil {
		t.Fatal("dial with reference subprotocol should fail")
	}
	if resp == nil || resp.StatusCode != 400 {
		t.Fatalf("status = %v, want 400", resp)
	}
}

func TestSubprotocolShunterTokenAccepted(t *testing.T) {
	s, _ := anonymousServer(t)
	srv := newTestServer(t, s)

	conn, resp, err := dialWS(t, srv, wsDialOpts{
		subprotocols: []string{SubprotocolV1},
	})
	if err != nil {
		t.Fatalf("dial with Shunter subprotocol: %v (resp=%v)", err, resp)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != SubprotocolV1 {
		t.Fatalf("server selected subprotocol = %q, want %q",
			got, SubprotocolV1)
	}
}

func TestSubprotocolShunterTokenSelectedWhenBothOffered(t *testing.T) {
	s, _ := anonymousServer(t)
	srv := newTestServer(t, s)

	conn, resp, err := dialWS(t, srv, wsDialOpts{
		subprotocols: []string{referenceSubprotocolToken, SubprotocolV1},
	})
	if err != nil {
		t.Fatalf("dial with both subprotocols: %v (resp=%v)", err, resp)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != SubprotocolV1 {
		t.Fatalf("server selected subprotocol = %q, want %q",
			got, SubprotocolV1)
	}
}

package protocol

import (
	"net/http"
	"testing"

	"github.com/ponchione/websocket"
)

// TestShunterCloseCodeConstants pins the four close codes used by
// the server. Reference: RFC 6455 §7.4.1 + ignored reference tree
// standard usage.
//
// TestCloseConstants in close_test.go already asserts the same values;
// this test is the contract pin for the close-code contract.
func TestShunterCloseCodeConstants(t *testing.T) {
	if CloseNormal != websocket.StatusNormalClosure {
		t.Errorf("CloseNormal = %d, want %d (1000 Normal Closure)", CloseNormal, websocket.StatusNormalClosure)
	}
	if CloseProtocol != websocket.StatusProtocolError {
		t.Errorf("CloseProtocol = %d, want %d (1002 Protocol Error)", CloseProtocol, websocket.StatusProtocolError)
	}
	if ClosePolicy != websocket.StatusPolicyViolation {
		t.Errorf("ClosePolicy = %d, want %d (1008 Policy Violation)", ClosePolicy, websocket.StatusPolicyViolation)
	}
	if CloseInternal != websocket.StatusInternalError {
		t.Errorf("CloseInternal = %d, want %d (1011 Internal Error)", CloseInternal, websocket.StatusInternalError)
	}
}

func TestShunterCloseReasonConstants(t *testing.T) {
	reasons := []string{
		CloseReasonTextFrameUnsupported,
		CloseReasonBrotliUnsupported,
		CloseReasonMalformedMessage,
		CloseReasonUnsupportedMessage,
		CloseReasonMessageTooLarge,
		CloseReasonTooManyRequests,
		CloseReasonSendBufferFull,
		CloseReasonIdleTimeout,
		CloseReasonServerShutdown,
	}
	seen := map[string]bool{}
	for _, reason := range reasons {
		if reason == "" {
			t.Fatal("close reason must not be empty")
		}
		if truncateCloseReason(reason) != reason {
			t.Fatalf("close reason %q exceeds close-frame budget", reason)
		}
		if seen[reason] {
			t.Fatalf("duplicate close reason %q", reason)
		}
		seen[reason] = true
	}
}

func TestShunterCloseReasonTelemetryTaxonomy(t *testing.T) {
	cases := []struct {
		name       string
		code       websocket.StatusCode
		wireReason string
		want       string
	}{
		{"normal_empty", CloseNormal, "", "normal"},
		{"server_shutdown", CloseNormal, CloseReasonServerShutdown, "server_shutdown"},
		{"send_buffer_full", ClosePolicy, CloseReasonSendBufferFull, "buffer_full"},
		{"idle_timeout", ClosePolicy, CloseReasonIdleTimeout, "idle_timeout"},
		{"generic_protocol", CloseProtocol, "unknown tag", "protocol_error"},
		{"generic_policy", ClosePolicy, "policy", "policy_violation"},
		{"generic_internal", CloseInternal, "panic", "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := closeReason(tc.code, tc.wireReason); got != tc.want {
				t.Fatalf("closeReason(%d, %q) = %q, want %q", tc.code, tc.wireReason, got, tc.want)
			}
		})
	}
}

func TestShunterProtocolErrorReasonTaxonomy(t *testing.T) {
	cases := []struct {
		wireReason string
		want       string
	}{
		{CloseReasonTextFrameUnsupported, "text_frame"},
		{CloseReasonBrotliUnsupported, "brotli_unsupported"},
		{CloseReasonUnsupportedMessage, "unsupported_message"},
		{CloseReasonTooManyRequests, "buffer_full"},
		{CloseReasonMalformedMessage, "malformed"},
		{"protocol: unknown message tag: tag=255", "malformed"},
	}
	for _, tc := range cases {
		if got := protocolErrorReason(tc.wireReason); got != tc.want {
			t.Fatalf("protocolErrorReason(%q) = %q, want %q", tc.wireReason, got, tc.want)
		}
	}
}

// TestShunterHandshakeRejectionStatuses pins the HTTP status codes
// the server returns before the WebSocket upgrade for each rejection
// class. These sub-tests exercise the upgrade.go guard sequence in order
// (auth → connection_id → compression → subprotocol). Each uses the
// same httptest.Server + dialWS harness that upgrade_test.go uses.
func TestShunterHandshakeRejectionStatuses(t *testing.T) {
	// Each case describes the rejection class being pinned. Cases that
	// test auth are set up without a valid token; cases that test
	// post-auth guards (connection_id, compression, subprotocol) carry a
	// valid token so that auth passes and the guard under test fires.
	// The server-side guard order in upgrade.go is:
	//   (1) auth  →  (2) connection_id  →  (3) compression  →  (4) subprotocol
	// This table covers one representative failure from each class.
	cases := []struct {
		name       string
		useAuth    bool   // inject a valid token before dialing
		serverMode string // "strict" | "anonymous"
		extraQuery string
		skipProto  bool
		authHeader string
		wantStatus int
	}{
		// Auth guard — strict server, no token.
		{
			name:       "strict_auth_no_token",
			serverMode: "strict",
			wantStatus: http.StatusUnauthorized,
		},
		// Auth guard — strict server, malformed JWT.
		{
			name:       "invalid_token",
			serverMode: "strict",
			authHeader: "Bearer not.a.jwt",
			wantStatus: http.StatusUnauthorized,
		},
		// connection_id guard — passes auth with a valid token.
		{
			name:       "zero_connection_id",
			serverMode: "strict",
			useAuth:    true,
			extraQuery: "connection_id=00000000000000000000000000000000",
			wantStatus: http.StatusBadRequest,
		},
		// Compression guard — passes auth + connection_id; anonymous server
		// is used so no token is required.
		{
			name:       "invalid_compression_param",
			serverMode: "anonymous",
			extraQuery: "compression=bogus",
			wantStatus: http.StatusBadRequest,
		},
		// Subprotocol guard — passes auth + connection_id + compression;
		// anonymous server so no token is required.
		{
			name:       "missing_subprotocol",
			serverMode: "anonymous",
			skipProto:  true,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s *Server
			if tc.serverMode == "strict" {
				s, _ = strictServer(t)
			} else {
				s, _ = anonymousServer(t)
			}
			srv := newTestServer(t, s)

			opts := wsDialOpts{
				subprotocols: []string{"v1.bsatn.shunter"},
				query:        tc.extraQuery,
			}
			if tc.useAuth {
				opts.authHeader = "Bearer " + mintValidToken(t)
			}
			if tc.authHeader != "" {
				opts.authHeader = tc.authHeader
			}
			if tc.skipProto {
				opts.skipSubprotocol = true
				opts.subprotocols = nil
			}

			_, resp, err := dialWS(t, srv, opts)
			if err == nil {
				t.Fatalf("dial succeeded, expected rejection with HTTP %d", tc.wantStatus)
			}
			if resp == nil {
				t.Fatalf("dial error %v but no HTTP response", err)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

package shunter

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/protocol"
)

func TestHostedStrictAuthOIDCDiscoveryExtraClaimsReachProcedure(t *testing.T) {
	const kid = "hosted-rsa-1"
	privateKey, jwk := generateHostedRS256JWK(t, kid)

	var discoveryRequests atomic.Int32
	var jwksRequests atomic.Int32
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			discoveryRequests.Add(1)
			writeHostedJSON(t, w, map[string]any{
				"issuer":                                idp.URL,
				"jwks_uri":                              idp.URL + "/jwks",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			jwksRequests.Add(1)
			writeHostedJSON(t, w, map[string]any{"keys": []map[string]string{jwk}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(idp.Close)

	var procedureCalls atomic.Int32
	rt, err := Build(validChatModule().Procedure("inspect_claims", func(ctx *ProcedureContext, _ []byte) ([]byte, error) {
		call := procedureCalls.Add(1)
		email, ok := ctx.Caller.Principal.Claims.Get("email")
		if !ok {
			return nil, fmt.Errorf("missing copied email claim")
		}
		if string(email) != `"alice@example.com"` {
			return nil, fmt.Errorf("email claim = %s, want compact JSON string", email)
		}
		email[1] = 'X'
		emailAgain, ok := ctx.Caller.Principal.Claims.Get("email")
		if !ok || string(emailAgain) != `"alice@example.com"` {
			return nil, fmt.Errorf("mutating Get result changed stored email claim: %s", emailAgain)
		}

		metadata, ok := ctx.Caller.Principal.Claims.Get("app_metadata")
		if !ok {
			return nil, fmt.Errorf("missing copied app_metadata claim")
		}
		if string(metadata) != `{"role":"member","tier":"pro"}` {
			return nil, fmt.Errorf("app_metadata claim = %s, want compact JSON object", metadata)
		}
		if call == 1 {
			stored := ctx.Caller.Principal.Claims.Values["app_metadata"]
			if len(stored) < 3 {
				return nil, fmt.Errorf("stored app_metadata claim too short: %s", stored)
			}
			stored[2] = 'X'
		}
		return metadata, nil
	}), Config{
		DataDir:        t.TempDir(),
		EnableProtocol: true,
		AuthMode:       AuthModeStrict,
		AuthOIDCDiscoveryIssuers: []AuthOIDCDiscoveryIssuer{{
			Issuer:     idp.URL,
			Algorithms: []AuthAlgorithm{AuthAlgorithmRS256},
			CacheTTL:   time.Hour,
		}},
		AuthIssuers:     []string{idp.URL},
		AuthAudiences:   []string{"shunter-api"},
		AuthExtraClaims: []string{"email", "app_metadata"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	hosted := httptest.NewServer(rt.HTTPHandler())
	t.Cleanup(hosted.Close)

	token := mintHostedRS256Token(t, privateKey, kid, idp.URL)
	conn, resp, err := dialHostedSubscribe(t, hosted, token)
	if err != nil {
		t.Fatalf("dial hosted strict-auth runtime: %v (resp=%v)", err, resp)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d, want 101", resp.StatusCode)
	}

	identity := readHostedIdentityToken(t, conn)
	if identity.Identity != auth.DeriveIdentity(idp.URL, "alice") {
		t.Fatalf("Identity = %x, want derived OIDC issuer/subject identity", identity.Identity)
	}
	if identity.Token != "" {
		t.Fatalf("strict-auth IdentityToken token = %q, want empty minted token", identity.Token)
	}
	if got := discoveryRequests.Load(); got != 1 {
		t.Fatalf("discovery requests = %d, want 1 token-validation fetch", got)
	}
	if got := jwksRequests.Load(); got != 1 {
		t.Fatalf("jwks requests = %d, want 1 token-validation fetch", got)
	}

	first := callHostedProcedure(t, conn, "first")
	assertHostedProcedureClaimResponse(t, first, "first")
	second := callHostedProcedure(t, conn, "second")
	assertHostedProcedureClaimResponse(t, second, "second")
	if got := procedureCalls.Load(); got != 2 {
		t.Fatalf("procedure calls = %d, want 2", got)
	}
}

func generateHostedRS256JWK(t *testing.T, kid string) (*rsa.PrivateKey, map[string]string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public := &privateKey.PublicKey
	jwk := map[string]string{
		"kty": "RSA",
		"kid": kid,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
	}
	return privateKey, jwk
}

func mintHostedRS256Token(t *testing.T, key *rsa.PrivateKey, kid, issuer string) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":   "alice",
		"iss":   issuer,
		"aud":   []string{"mobile", "shunter-api"},
		"iat":   now.Add(-time.Minute).Unix(),
		"nbf":   now.Add(-time.Minute).Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"email": "alice@example.com",
		"app_metadata": map[string]any{
			"role": "member",
			"tier": "pro",
		},
	})
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func writeHostedJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func dialHostedSubscribe(t *testing.T, srv *httptest.Server, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{protocol.SubprotocolV2},
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + token}},
	})
}

func readHostedIdentityToken(t *testing.T, conn *websocket.Conn) protocol.IdentityToken {
	t.Helper()
	frame := readHostedBinary(t, conn)
	tag, msg, err := protocol.DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("DecodeServerMessage IdentityToken: %v", err)
	}
	if tag != protocol.TagIdentityToken {
		t.Fatalf("server tag = %d, want IdentityToken", tag)
	}
	identity, ok := msg.(protocol.IdentityToken)
	if !ok {
		t.Fatalf("server message = %T, want protocol.IdentityToken", msg)
	}
	return identity
}

func callHostedProcedure(t *testing.T, conn *websocket.Conn, messageID string) protocol.ProcedureResponse {
	t.Helper()
	frame, err := protocol.EncodeClientMessage(protocol.CallProcedureMsg{
		MessageID: []byte(messageID),
		Name:      "inspect_claims",
	})
	if err != nil {
		t.Fatalf("EncodeClientMessage CallProcedure: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write CallProcedure: %v", err)
	}

	data := readHostedBinary(t, conn)
	tag, msg, err := protocol.DecodeServerMessage(data)
	if err != nil {
		t.Fatalf("DecodeServerMessage ProcedureResponse: %v", err)
	}
	if tag != protocol.TagProcedureResponse {
		t.Fatalf("server tag = %d, want ProcedureResponse", tag)
	}
	response, ok := msg.(protocol.ProcedureResponse)
	if !ok {
		t.Fatalf("server message = %T, want protocol.ProcedureResponse", msg)
	}
	return response
}

func assertHostedProcedureClaimResponse(t *testing.T, response protocol.ProcedureResponse, messageID string) {
	t.Helper()
	if string(response.MessageID) != messageID {
		t.Fatalf("procedure message ID = %q, want %q", response.MessageID, messageID)
	}
	if response.Error != nil {
		t.Fatalf("procedure response error = %q", *response.Error)
	}
	if string(response.Result) != `{"role":"member","tier":"pro"}` {
		t.Fatalf("procedure result = %s, want copied compact JSON claim", response.Result)
	}
}

func readHostedBinary(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	messageType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if messageType != websocket.MessageBinary {
		t.Fatalf("websocket message type = %v, want binary", messageType)
	}
	return data
}

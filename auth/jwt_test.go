package auth

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testKey = []byte("test-secret-key")

// mintHS256 builds an HS256-signed token for tests. The claims map is
// passed through to jwt.MapClaims verbatim, giving each test fine
// control over which claims are present or absent.
func mintHS256(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(testKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestValidateJWTNilConfigFailsWithoutPanic(t *testing.T) {
	s := mintHS256(t, jwt.MapClaims{"sub": "alice", "iss": "issuer"})

	claims, err := ValidateJWT(s, nil)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("ValidateJWT nil config error = %v, want ErrJWTInvalid", err)
	}
	if claims != nil {
		t.Fatalf("ValidateJWT nil config claims = %+v, want nil", claims)
	}
}

func TestValidateJWTEmptySigningKeyRejected(t *testing.T) {
	s := mintHS256(t, jwt.MapClaims{"sub": "alice", "iss": "issuer"})

	claims, err := ValidateJWT(s, &JWTConfig{})
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("ValidateJWT empty signing key error = %v, want ErrJWTInvalid", err)
	}
	if claims != nil {
		t.Fatalf("ValidateJWT empty signing key claims = %+v, want nil", claims)
	}
}

func TestValidateJWTFullyPopulated(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, AuthMode: AuthModeStrict}
	now := time.Now()
	exp := now.Add(time.Hour)
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": "https://issuer.example",
		"iat": now.Unix(),
		"exp": exp.Unix(),
	})

	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "alice" || claims.Issuer != "https://issuer.example" {
		t.Errorf("claims mismatch: %+v", claims)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Equal(time.Unix(exp.Unix(), 0)) {
		t.Errorf("ExpiresAt = %v, want %v", claims.ExpiresAt, exp)
	}
}

func TestValidateJWTWithoutExpAccepted(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"iat": time.Now().Unix(),
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt != nil {
		t.Errorf("ExpiresAt should be nil without exp claim; got %v", *claims.ExpiresAt)
	}
}

func TestValidateJWTIssuerAllowlistAccepted(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Issuers: []string{"https://issuer.example"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": "https://issuer.example",
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != "https://issuer.example" {
		t.Fatalf("Issuer = %q, want configured issuer", claims.Issuer)
	}
}

func TestValidateJWTIssuerAllowlistRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Issuers: []string{"https://issuer.example"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "alice",
		"iss": "https://other.example",
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTIssuerMismatch) {
		t.Fatalf("error = %v, want ErrJWTIssuerMismatch", err)
	}
}

func TestValidateJWTExpiredRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Errorf("got %v, want ErrJWTInvalid for expired token", err)
	}
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Fatalf("expired token error should preserve jwt.ErrTokenExpired, got %v", err)
	}
}

func TestValidateJWTBadSignatureRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: []byte("WRONG-KEY")}
	s := mintHS256(t, jwt.MapClaims{"sub": "a", "iss": "b"})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Errorf("got %v, want ErrJWTInvalid for bad signature", err)
	}
	if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		t.Fatalf("bad signature error should preserve jwt.ErrTokenSignatureInvalid, got %v", err)
	}
}

func TestValidateJWTRejectsNonHS256HMAC(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{"sub": "a", "iss": "b"})
	s, err := tok.SignedString(testKey)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("got %v, want ErrJWTInvalid for HS384 token", err)
	}
}

func TestValidateJWTMissingSub(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{"iss": "b"})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTMissingClaim) {
		t.Errorf("got %v, want ErrJWTMissingClaim for missing sub", err)
	}
}

func TestValidateJWTMissingIss(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{"sub": "a"})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTMissingClaim) {
		t.Errorf("got %v, want ErrJWTMissingClaim for missing iss", err)
	}
}

func TestValidateJWTHexIdentityMatches(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	derived := DeriveIdentity("issuer", "alice")
	s := mintHS256(t, jwt.MapClaims{
		"sub":          "alice",
		"iss":          "issuer",
		"hex_identity": derived.Hex(),
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.HexIdentity != derived.Hex() {
		t.Errorf("HexIdentity mismatch")
	}
}

func TestValidateJWTHexIdentityMismatchRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub":          "alice",
		"iss":          "issuer",
		"hex_identity": "0000000000000000000000000000000000000000000000000000000000000000",
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTHexIdentityMismatch) {
		t.Errorf("got %v, want ErrJWTHexIdentityMismatch", err)
	}
}

func TestValidateJWTHexIdentityAbsent(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{"sub": "alice", "iss": "issuer"})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if claims.HexIdentity != "" {
		t.Errorf("HexIdentity should be empty when claim absent; got %q", claims.HexIdentity)
	}
}

func TestValidateJWTAudienceAccepted(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Audiences: []string{"shunter-prod"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"aud": "shunter-prod",
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "shunter-prod" {
		t.Errorf("Audience mismatch: %+v", claims.Audience)
	}
}

func TestValidateJWTAudienceRejected(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey, Audiences: []string{"shunter-prod"}}
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"aud": "shunter-staging",
	})
	_, err := ValidateJWT(s, cfg)
	if !errors.Is(err, ErrJWTAudienceMismatch) {
		t.Errorf("got %v, want ErrJWTAudienceMismatch", err)
	}
}

func TestValidateJWTAudienceDisabledAcceptsAny(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey} // no Audiences configured
	s := mintHS256(t, jwt.MapClaims{
		"sub": "a",
		"iss": "b",
		"aud": "literally-anything",
	})
	_, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatalf("audience validation should be skipped when Audiences is empty; got %v", err)
	}
}

func TestValidateJWTPermissionClaims(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	s := mintHS256(t, jwt.MapClaims{
		"sub":         "alice",
		"iss":         "issuer",
		"permissions": []string{"messages:send", "messages:read"},
	})
	claims, err := ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := claims.Permissions; len(got) != 2 || got[0] != "messages:send" || got[1] != "messages:read" {
		t.Fatalf("Permissions = %#v, want send/read tags", got)
	}

	s = mintHS256(t, jwt.MapClaims{
		"sub":         "alice",
		"iss":         "issuer",
		"permissions": "messages:admin",
	})
	claims, err = ValidateJWT(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := claims.Permissions; len(got) != 1 || got[0] != "messages:admin" {
		t.Fatalf("single-string Permissions = %#v, want admin tag", got)
	}
}

func TestValidateJWTLooseStringListClaims(t *testing.T) {
	cfg := &JWTConfig{SigningKey: testKey}
	tests := []struct {
		name            string
		audience        any
		permissions     any
		wantAudience    []string
		wantPermissions []string
	}{
		{
			name:            "strings",
			audience:        "",
			permissions:     "",
			wantAudience:    []string{""},
			wantPermissions: nil,
		},
		{
			name:            "lists",
			audience:        []any{"", "shunter-prod", 42},
			permissions:     []any{"", "messages:send", 17},
			wantAudience:    []string{"", "shunter-prod"},
			wantPermissions: []string{"messages:send"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mintHS256(t, jwt.MapClaims{
				"sub":         "alice",
				"iss":         "issuer",
				"aud":         tt.audience,
				"permissions": tt.permissions,
			})
			claims, err := ValidateJWT(s, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(claims.Audience, tt.wantAudience) {
				t.Fatalf("Audience = %#v, want %#v", claims.Audience, tt.wantAudience)
			}
			if !reflect.DeepEqual(claims.Permissions, tt.wantPermissions) {
				t.Fatalf("Permissions = %#v, want %#v", claims.Permissions, tt.wantPermissions)
			}
		})
	}
}

func TestValidateJWTConcurrentValidationShortSoak(t *testing.T) {
	const seed = uint64(0xa17c0de)
	const workers = 8
	const iterations = 64

	issuer := "issuer"
	subject := "alice"
	audience := "shunter-prod"
	identity := DeriveIdentity(issuer, subject)
	now := time.Now()
	cfg := &JWTConfig{SigningKey: testKey, Audiences: []string{audience}, AuthMode: AuthModeStrict}
	s := mintHS256(t, jwt.MapClaims{
		"sub":          subject,
		"iss":          issuer,
		"aud":          audience,
		"iat":          now.Unix(),
		"exp":          now.Add(time.Hour).Unix(),
		"hex_identity": identity.Hex(),
		"permissions":  []string{"messages:send", "messages:read"},
	})

	start := make(chan struct{})
	failures := make(chan string, workers*iterations)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for iteration := range iterations {
				opIndex := worker*iterations + iteration
				if ((uint64(worker)<<8)^uint64(iteration)^seed)&3 == 0 {
					runtime.Gosched()
				}
				claims, err := ValidateJWT(s, cfg)
				if err != nil {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ValidateJWT observed_error=%v expected=nil",
						seed, opIndex, worker, workers, iterations, err)
					return
				}
				if claims.Subject != subject || claims.Issuer != issuer {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ValidateJWT observed_claims=%+v expected_subject=%q expected_issuer=%q",
						seed, opIndex, worker, workers, iterations, claims, subject, issuer)
					return
				}
				if got := claims.DeriveIdentity(); got != identity {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=DeriveIdentity observed=%s expected=%s",
						seed, opIndex, worker, workers, iterations, got.Hex(), identity.Hex())
					return
				}
				if len(claims.Audience) != 1 || claims.Audience[0] != audience {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ValidateJWT audience observed=%v expected=[%s]",
						seed, opIndex, worker, workers, iterations, claims.Audience, audience)
					return
				}
				if len(claims.Permissions) != 2 || claims.Permissions[0] != "messages:send" || claims.Permissions[1] != "messages:read" {
					failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ValidateJWT permissions observed=%v expected=[messages:send messages:read]",
						seed, opIndex, worker, workers, iterations, claims.Permissions)
					return
				}
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func TestClaimsDeriveIdentity(t *testing.T) {
	c := &Claims{Issuer: "issuer", Subject: "alice"}
	got := c.DeriveIdentity()
	want := DeriveIdentity("issuer", "alice")
	if got != want {
		t.Errorf("Claims.DeriveIdentity returned %x, want %x", got, want)
	}
}

func TestClaimsPrincipalCopiesExternalClaims(t *testing.T) {
	c := &Claims{
		Issuer:      "issuer",
		Subject:     "alice",
		Audience:    []string{"shunter-api"},
		Permissions: []string{"messages:send"},
	}

	principal := c.Principal()
	if principal.Issuer != "issuer" || principal.Subject != "alice" {
		t.Fatalf("Principal identity = %+v, want issuer/alice", principal)
	}
	if len(principal.Audience) != 1 || principal.Audience[0] != "shunter-api" {
		t.Fatalf("Principal audience = %#v, want shunter-api", principal.Audience)
	}
	if len(principal.Permissions) != 1 || principal.Permissions[0] != "messages:send" {
		t.Fatalf("Principal permissions = %#v, want messages:send", principal.Permissions)
	}

	principal.Audience[0] = "mutated"
	principal.Permissions[0] = "mutated"
	if c.Audience[0] != "shunter-api" || c.Permissions[0] != "messages:send" {
		t.Fatalf("Principal aliases Claims slices: claims=%+v principal=%+v", c, principal)
	}

	var nilClaims *Claims
	if got := nilClaims.Principal(); got.Issuer != "" || got.Subject != "" || got.Audience != nil || got.Permissions != nil {
		t.Fatalf("nil Claims Principal = %+v, want zero", got)
	}
}

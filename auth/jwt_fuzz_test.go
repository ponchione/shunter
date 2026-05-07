package auth

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func FuzzValidateJWTGeneratedClaims(f *testing.F) {
	for _, seed := range [][]byte{
		{0, 1, 2, 3},
		{1, 3, 5, 7},
		{2, 4, 6, 8},
		{3, 9, 2, 6},
		{4, 1, 8, 5},
		{5, 2, 7, 4},
		{6, 3, 6, 9},
		{7, 4, 5, 8},
		{8, 5, 4, 7},
		{9, 6, 3, 2},
		{10, 7, 2, 1},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, seed []byte) {
		const maxSeedBytes = 32
		originalSeedLen := len(seed)
		if len(seed) > maxSeedBytes {
			seed = seed[:maxSeedBytes]
		}
		if len(seed) == 0 {
			seed = []byte{0}
		}

		scenario := buildValidateJWTGeneratedClaimsScenario(t, seed)
		label := fmt.Sprintf("seed=%x seed_len=%d op_index=%d runtime_config=%q operation=%s",
			seed, originalSeedLen, scenario.opIndex, scenario.runtimeConfig, scenario.operation)

		got, err := ValidateJWT(scenario.token, scenario.config)
		if scenario.wantErr != nil {
			if !errors.Is(err, scenario.wantErr) {
				t.Fatalf("%s observed_error=%v expected_error=%v", label, err, scenario.wantErr)
			}
			if got != nil {
				t.Fatalf("%s observed_claims=%s expected_claims=<nil>", label, summarizeJWTClaims(got))
			}
			return
		}
		if err != nil {
			t.Fatalf("%s observed_error=%v expected_error=<nil>", label, err)
		}
		assertJWTClaimsEquivalent(t, label, got, scenario.want)

		again, err := ValidateJWT(scenario.token, scenario.config)
		if err != nil {
			t.Fatalf("%s operation=ValidateJWT(replay) observed_error=%v expected_error=<nil>", label, err)
		}
		if !sameJWTClaims(got, again) {
			t.Fatalf("%s operation=ValidateJWT(replay) observed_claims=%s expected_claims=%s",
				label, summarizeJWTClaims(again), summarizeJWTClaims(got))
		}
	})
}

type validateJWTGeneratedClaimsScenario struct {
	opIndex       int
	operation     string
	runtimeConfig string
	token         string
	config        *JWTConfig
	want          *Claims
	wantErr       error
}

func buildValidateJWTGeneratedClaimsScenario(t testing.TB, seed []byte) validateJWTGeneratedClaimsScenario {
	t.Helper()

	const (
		fixedIAT = int64(1704067200) // 2024-01-01T00:00:00Z
		futureEP = int64(2524608000) // 2050-01-01T00:00:00Z
		pastEP   = int64(946684800)  // 2000-01-01T00:00:00Z
	)

	reader := jwtFuzzSeedReader{data: seed}
	opIndex := int(reader.next() % 11)
	subject := reader.pick("alice", "bob", "carol")
	issuer := reader.pick("issuer-a", "issuer-b", "https://issuer.example")
	audience := reader.pick("aud-a", "aud-b", "aud-c")
	permissionRaw, permissions := jwtFuzzPermissions(&reader)
	baseClaims := jwt.MapClaims{
		"sub":         subject,
		"iss":         issuer,
		"iat":         fixedIAT,
		"exp":         futureEP,
		"permissions": permissionRaw,
	}
	wantIssuedAt := time.Unix(fixedIAT, 0)
	wantExpiresAt := time.Unix(futureEP, 0)
	want := &Claims{
		Subject:     subject,
		Issuer:      issuer,
		ExpiresAt:   &wantExpiresAt,
		IssuedAt:    wantIssuedAt,
		Permissions: permissions,
	}

	signingKey := testKey
	method := jwt.SigningMethodHS256
	cfg := &JWTConfig{SigningKey: testKey, AuthMode: AuthModeStrict}
	operation := "ValidateJWT(valid-minimal)"
	wantErr := error(nil)

	switch opIndex {
	case 0:
		// valid minimal token
	case 1:
		baseClaims["aud"] = audience
		cfg.Audiences = []string{"aud-z", audience}
		want.Audience = []string{audience}
		operation = "ValidateJWT(valid-string-audience)"
	case 2:
		baseClaims["aud"] = []string{"aud-x", audience}
		cfg.Audiences = []string{audience, "aud-y"}
		want.Audience = []string{"aud-x", audience}
		operation = "ValidateJWT(valid-list-audience)"
	case 3:
		delete(baseClaims, "sub")
		want = nil
		wantErr = ErrJWTMissingClaim
		operation = "ValidateJWT(missing-sub)"
	case 4:
		delete(baseClaims, "iss")
		want = nil
		wantErr = ErrJWTMissingClaim
		operation = "ValidateJWT(missing-iss)"
	case 5:
		baseClaims["aud"] = audience
		cfg.Audiences = []string{"aud-denied"}
		want = nil
		wantErr = ErrJWTAudienceMismatch
		operation = "ValidateJWT(audience-mismatch)"
	case 6:
		baseClaims["hex_identity"] = "0000000000000000000000000000000000000000000000000000000000000000"
		want = nil
		wantErr = ErrJWTHexIdentityMismatch
		operation = "ValidateJWT(hex-identity-mismatch)"
	case 7:
		signingKey = []byte("wrong-validation-fuzz-key")
		want = nil
		wantErr = ErrJWTInvalid
		operation = "ValidateJWT(bad-signature)"
	case 8:
		baseClaims["exp"] = pastEP
		want = nil
		wantErr = ErrJWTInvalid
		operation = "ValidateJWT(expired)"
	case 9:
		want = nil
		wantErr = ErrJWTInvalid
		operation = "ValidateJWT(malformed-token)"
		return validateJWTGeneratedClaimsScenario{
			opIndex:       opIndex,
			operation:     operation,
			runtimeConfig: formatJWTValidationRuntimeConfig(cfg, "malformed", len(seed)),
			token:         "not-a-jwt." + fmt.Sprintf("%x", seed),
			config:        cfg,
			wantErr:       wantErr,
		}
	case 10:
		method = jwt.SigningMethodHS384
		want = nil
		wantErr = ErrJWTInvalid
		operation = "ValidateJWT(unsupported-alg)"
	}

	if wantErr == nil && reader.next()%2 == 0 {
		identity := DeriveIdentity(issuer, subject).Hex()
		baseClaims["hex_identity"] = identity
		want.HexIdentity = identity
	}

	token := mintJWTForValidationFuzz(t, method, baseClaims, signingKey)
	return validateJWTGeneratedClaimsScenario{
		opIndex:       opIndex,
		operation:     operation,
		runtimeConfig: formatJWTValidationRuntimeConfig(cfg, method.Alg(), len(seed)),
		token:         token,
		config:        cfg,
		want:          want,
		wantErr:       wantErr,
	}
}

func mintJWTForValidationFuzz(t testing.TB, method jwt.SigningMethod, claims jwt.MapClaims, key []byte) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("operation=SignJWT observed_error=%v expected_error=<nil>", err)
	}
	return s
}

type jwtFuzzSeedReader struct {
	data []byte
	pos  int
}

func (r *jwtFuzzSeedReader) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[r.pos%len(r.data)]
	r.pos++
	return b
}

func (r *jwtFuzzSeedReader) pick(values ...string) string {
	return values[int(r.next())%len(values)]
}

func jwtFuzzPermissions(reader *jwtFuzzSeedReader) (any, []string) {
	first := "tasks:" + reader.pick("read", "write", "admin")
	second := "rooms:" + reader.pick("read", "write", "admin")
	switch reader.next() % 3 {
	case 0:
		return first, []string{first}
	case 1:
		return []string{first, second}, []string{first, second}
	default:
		return []any{first, "", 17, second}, []string{first, second}
	}
}

func formatJWTValidationRuntimeConfig(cfg *JWTConfig, alg string, seedBytes int) string {
	return fmt.Sprintf("auth=strict alg=%s issuers=%v audiences=%v seed_bytes<=%d signing_key_bytes=%d",
		alg, cfg.Issuers, cfg.Audiences, seedBytes, len(cfg.SigningKey))
}

func assertJWTClaimsEquivalent(t testing.TB, label string, got, want *Claims) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s observed_claims=<nil> expected_claims=%s", label, summarizeJWTClaims(want))
	}
	if !sameJWTClaims(got, want) {
		t.Fatalf("%s observed_claims=%s expected_claims=%s", label, summarizeJWTClaims(got), summarizeJWTClaims(want))
	}
	if got.DeriveIdentity() != DeriveIdentity(want.Issuer, want.Subject) {
		t.Fatalf("%s operation=DeriveIdentity observed=%s expected=%s",
			label, got.DeriveIdentity().Hex(), DeriveIdentity(want.Issuer, want.Subject).Hex())
	}
}

func sameJWTClaims(a, b *Claims) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Subject != b.Subject || a.Issuer != b.Issuer || a.HexIdentity != b.HexIdentity {
		return false
	}
	if !reflect.DeepEqual(a.Audience, b.Audience) || !reflect.DeepEqual(a.Permissions, b.Permissions) {
		return false
	}
	if !a.IssuedAt.Equal(b.IssuedAt) {
		return false
	}
	if a.ExpiresAt == nil || b.ExpiresAt == nil {
		return a.ExpiresAt == b.ExpiresAt
	}
	return a.ExpiresAt.Equal(*b.ExpiresAt)
}

func summarizeJWTClaims(c *Claims) string {
	if c == nil {
		return "<nil>"
	}
	expiresAt := "<nil>"
	if c.ExpiresAt != nil {
		expiresAt = c.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("{subject:%q issuer:%q audience:%v issued_at:%s expires_at:%s hex_identity:%q permissions:%v derived_identity:%s}",
		c.Subject,
		c.Issuer,
		c.Audience,
		c.IssuedAt.UTC().Format(time.RFC3339),
		expiresAt,
		c.HexIdentity,
		c.Permissions,
		c.DeriveIdentity().Hex(),
	)
}

package auth

import (
	"testing"
	"time"
)

func testMintConfig() *MintConfig {
	return &MintConfig{
		Issuer:     "https://shunter.local/anonymous",
		Audience:   "shunter-local",
		SigningKey: testKey,
		Expiry:     time.Hour,
	}
}

func TestMintAnonymousTokenValidatesRoundTrip(t *testing.T) {
	cfg := testMintConfig()
	token, id, err := MintAnonymousToken(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if id.IsZero() {
		t.Error("minted Identity should not be zero")
	}

	// Reconnect path: validate the freshly minted token.
	valCfg := &JWTConfig{
		SigningKey: cfg.SigningKey,
		Audiences:  []string{cfg.Audience},
	}
	claims, err := ValidateJWT(token, valCfg)
	if err != nil {
		t.Fatalf("minted token should re-validate: %v", err)
	}
	if claims.Issuer != cfg.Issuer {
		t.Errorf("iss = %q, want %q", claims.Issuer, cfg.Issuer)
	}
	if claims.Subject == "" {
		t.Error("subject must be populated")
	}
	if claims.DeriveIdentity() != id {
		t.Error("Identity derived from claims must match the returned Identity")
	}
}

func TestMintAnonymousTokenExpirySet(t *testing.T) {
	cfg := testMintConfig()
	cfg.Expiry = 2 * time.Hour
	token, _, err := MintAnonymousToken(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateJWT(token, &JWTConfig{SigningKey: cfg.SigningKey})
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("exp claim should be present when Expiry > 0")
	}
	// Expiry should be roughly now + 2h; allow 5s slack for test timing.
	want := time.Now().Add(2 * time.Hour)
	diff := claims.ExpiresAt.Sub(want)
	if diff < -5*time.Second || diff > 5*time.Second {
		t.Errorf("ExpiresAt = %v, want around %v (diff %v)", *claims.ExpiresAt, want, diff)
	}
}

func TestMintAnonymousTokenNoExpiry(t *testing.T) {
	cfg := testMintConfig()
	cfg.Expiry = 0
	token, _, err := MintAnonymousToken(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateJWT(token, &JWTConfig{SigningKey: cfg.SigningKey})
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt != nil {
		t.Errorf("no exp claim expected when Expiry=0; got %v", *claims.ExpiresAt)
	}
}

func TestMintAnonymousTokenDistinctSubjects(t *testing.T) {
	cfg := testMintConfig()
	tok1, id1, err := MintAnonymousToken(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tok2, id2, err := MintAnonymousToken(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if tok1 == tok2 {
		t.Error("two mints should produce different tokens (subject randomness)")
	}
	if id1 == id2 {
		t.Error("two mints should produce different Identities")
	}
}

func TestMintAnonymousTokenCarriesAudience(t *testing.T) {
	cfg := testMintConfig()
	token, _, err := MintAnonymousToken(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateJWT(token, &JWTConfig{
		SigningKey: cfg.SigningKey,
		Audiences:  []string{cfg.Audience},
	})
	if err != nil {
		t.Fatalf("minted token should satisfy configured audience: %v", err)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != cfg.Audience {
		t.Errorf("Audience = %v, want [%q]", claims.Audience, cfg.Audience)
	}
}

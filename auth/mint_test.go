package auth

import (
	"errors"
	"fmt"
	"strings"
	"sync"
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

func TestMintAnonymousTokenNilConfigFailsBeforeRandomRead(t *testing.T) {
	originalReadRandom := readRandom
	readRandom = func([]byte) (int, error) {
		t.Fatal("MintAnonymousToken read randomness before rejecting nil config")
		return 0, nil
	}
	defer func() { readRandom = originalReadRandom }()

	token, id, err := MintAnonymousToken(nil)
	if err == nil {
		t.Fatal("MintAnonymousToken nil config returned nil error")
	}
	if token != "" || !id.IsZero() {
		t.Fatalf("MintAnonymousToken nil config returned token=%q identity=%s, want empty", token, id.Hex())
	}
	if !strings.Contains(err.Error(), "config is required") {
		t.Fatalf("MintAnonymousToken nil config error = %v, want config context", err)
	}
}

func TestMintAnonymousTokenEmptySigningKeyFailsBeforeRandomRead(t *testing.T) {
	originalReadRandom := readRandom
	readRandom = func([]byte) (int, error) {
		t.Fatal("MintAnonymousToken read randomness before rejecting empty signing key")
		return 0, nil
	}
	defer func() { readRandom = originalReadRandom }()

	cfg := testMintConfig()
	cfg.SigningKey = nil
	token, id, err := MintAnonymousToken(cfg)
	if err == nil {
		t.Fatal("MintAnonymousToken empty signing key returned nil error")
	}
	if token != "" || !id.IsZero() {
		t.Fatalf("MintAnonymousToken empty signing key returned token=%q identity=%s, want empty", token, id.Hex())
	}
	if !strings.Contains(err.Error(), "signing key is required") {
		t.Fatalf("MintAnonymousToken empty signing key error = %v, want signing key context", err)
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

func TestMintAnonymousTokenRandomFailureWrapsCause(t *testing.T) {
	faultErr := errors.New("injected random failure")
	originalReadRandom := readRandom
	readRandom = func([]byte) (int, error) {
		return 0, faultErr
	}
	defer func() { readRandom = originalReadRandom }()

	token, id, err := MintAnonymousToken(testMintConfig())
	if err == nil {
		t.Fatal("MintAnonymousToken returned nil error, want random fault")
	}
	if token != "" || !id.IsZero() {
		t.Fatalf("failed mint returned token=%q identity=%s, want empty", token, id.Hex())
	}
	if !errors.Is(err, faultErr) {
		t.Fatalf("MintAnonymousToken error = %v, want wrapped injected fault", err)
	}
	if !strings.Contains(err.Error(), "mint random subject") {
		t.Fatalf("MintAnonymousToken error = %v, want random subject context", err)
	}
}

func TestMintAnonymousTokenConcurrentValidateSoak(t *testing.T) {
	const (
		workers = 6
		ops     = 16
	)
	cfg := testMintConfig()
	validateCfg := &JWTConfig{
		SigningKey: cfg.SigningKey,
		Audiences:  []string{cfg.Audience},
	}
	type mintResult struct {
		worker  int
		op      int
		token   string
		subject string
		id      string
	}

	results := make(chan mintResult, workers*ops)
	errs := make(chan error, workers*ops)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for op := 0; op < ops; op++ {
				label := fmt.Sprintf("worker=%d op=%d", worker, op)
				token, id, err := MintAnonymousToken(cfg)
				if err != nil {
					errs <- fmt.Errorf("%s MintAnonymousToken: %w", label, err)
					continue
				}
				if id.IsZero() {
					errs <- fmt.Errorf("%s minted zero identity", label)
					continue
				}
				claims, err := ValidateJWT(token, validateCfg)
				if err != nil {
					errs <- fmt.Errorf("%s ValidateJWT: %w", label, err)
					continue
				}
				if claims.Issuer != cfg.Issuer {
					errs <- fmt.Errorf("%s issuer = %q, want %q", label, claims.Issuer, cfg.Issuer)
					continue
				}
				if len(claims.Audience) != 1 || claims.Audience[0] != cfg.Audience {
					errs <- fmt.Errorf("%s audience = %v, want [%q]", label, claims.Audience, cfg.Audience)
					continue
				}
				if got := claims.DeriveIdentity(); got != id {
					errs <- fmt.Errorf("%s derived identity = %s, want %s", label, got.Hex(), id.Hex())
					continue
				}
				results <- mintResult{
					worker:  worker,
					op:      op,
					token:   token,
					subject: claims.Subject,
					id:      id.Hex(),
				}
			}
		}(worker)
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	seenTokens := make(map[string]mintResult, workers*ops)
	seenSubjects := make(map[string]mintResult, workers*ops)
	seenIdentities := make(map[string]mintResult, workers*ops)
	count := 0
	for result := range results {
		count++
		if previous, ok := seenTokens[result.token]; ok {
			t.Fatalf("duplicate token current=worker=%d op=%d previous=worker=%d op=%d", result.worker, result.op, previous.worker, previous.op)
		}
		if previous, ok := seenSubjects[result.subject]; ok {
			t.Fatalf("duplicate subject %q current=worker=%d op=%d previous=worker=%d op=%d", result.subject, result.worker, result.op, previous.worker, previous.op)
		}
		if previous, ok := seenIdentities[result.id]; ok {
			t.Fatalf("duplicate identity %s current=worker=%d op=%d previous=worker=%d op=%d", result.id, result.worker, result.op, previous.worker, previous.op)
		}
		seenTokens[result.token] = result
		seenSubjects[result.subject] = result
		seenIdentities[result.id] = result
	}
	if count != workers*ops {
		t.Fatalf("mint results = %d, want %d", count, workers*ops)
	}
}

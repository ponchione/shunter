package shunter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
)

func TestConfigFromEnvUsesDefaultsWhenUnset(t *testing.T) {
	cfg, err := ConfigFromEnvE()
	if err != nil {
		t.Fatalf("ConfigFromEnvE returned error: %v", err)
	}
	if cfg.DataDir != "" || cfg.ListenAddr != "" || cfg.EnableProtocol || cfg.AuthMode != AuthModeDev {
		t.Fatalf("ConfigFromEnvE defaults = %+v, want zero-value hosted config", cfg)
	}
}

func TestConfigFromEnvMapsHostedVariables(t *testing.T) {
	t.Setenv("SHUNTER_DATA_DIR", " ./data/app ")
	t.Setenv("SHUNTER_LISTEN_ADDR", "127.0.0.1:3111")
	t.Setenv("SHUNTER_ENABLE_PROTOCOL", "true")
	t.Setenv("SHUNTER_AUTH_MODE", "strict")
	t.Setenv("SHUNTER_AUTH_SIGNING_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SHUNTER_AUTH_ISSUERS", " issuer-a,issuer-b ,, ")
	t.Setenv("SHUNTER_AUTH_AUDIENCES", " app ")

	cfg, err := ConfigFromEnvE()
	if err != nil {
		t.Fatalf("ConfigFromEnvE returned error: %v", err)
	}
	if cfg.DataDir != "./data/app" ||
		cfg.ListenAddr != "127.0.0.1:3111" ||
		!cfg.EnableProtocol ||
		cfg.AuthMode != AuthModeStrict ||
		string(cfg.AuthSigningKey) != "0123456789abcdef0123456789abcdef" ||
		len(cfg.AuthIssuers) != 2 ||
		cfg.AuthIssuers[0] != "issuer-a" ||
		cfg.AuthIssuers[1] != "issuer-b" ||
		len(cfg.AuthAudiences) != 1 ||
		cfg.AuthAudiences[0] != "app" {
		t.Fatalf("ConfigFromEnvE mapped config = %+v", cfg)
	}
}

func TestConfigFromEnvRejectsInvalidValues(t *testing.T) {
	t.Setenv("SHUNTER_ENABLE_PROTOCOL", "sometimes")
	if _, err := ConfigFromEnvE(); err == nil {
		t.Fatal("ConfigFromEnvE accepted invalid SHUNTER_ENABLE_PROTOCOL")
	}

	t.Setenv("SHUNTER_ENABLE_PROTOCOL", "")
	t.Setenv("SHUNTER_AUTH_MODE", "unknown")
	if _, err := ConfigFromEnvE(); err == nil {
		t.Fatal("ConfigFromEnvE accepted invalid SHUNTER_AUTH_MODE")
	}
}

func TestRunBuildsServesAndStopsOnContextCancel(t *testing.T) {
	startedHook := make(chan struct{})
	oldHook := runtimeStartAfterDurabilityHook
	runtimeStartAfterDurabilityHook = func(*Runtime) error {
		close(startedHook)
		return nil
	}
	defer func() { runtimeStartAfterDurabilityHook = oldHook }()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, validChatModule(), Config{
			DataDir:    t.TempDir(),
			ListenAddr: "127.0.0.1:0",
		})
	}()

	select {
	case <-startedHook:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not start runtime before deadline")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned %v, want graceful nil on context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestRunReturnsDurabilityCloseFailureOnContextCancel(t *testing.T) {
	startedHook := make(chan struct{})
	oldStartHook := runtimeStartAfterDurabilityHook
	runtimeStartAfterDurabilityHook = func(*Runtime) error {
		close(startedHook)
		return nil
	}
	closeFailure := errors.New("injected durability close failure")
	oldCloseHook := closeRuntimeDurability
	closeRuntimeDurability = func(worker *commitlog.DurabilityWorker) (uint64, error) {
		finalTxID, err := worker.Close()
		return finalTxID, errors.Join(err, closeFailure)
	}
	t.Cleanup(func() {
		runtimeStartAfterDurabilityHook = oldStartHook
		closeRuntimeDurability = oldCloseHook
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, validChatModule(), Config{
			DataDir:    t.TempDir(),
			ListenAddr: "127.0.0.1:0",
		})
	}()

	select {
	case <-startedHook:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not start runtime before deadline")
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, closeFailure) {
			t.Fatalf("Run error = %v, want durability close failure", err)
		}
		if errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, do not want context cancellation joined with durability close failure", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestRunReturnsBuildAndServeErrors(t *testing.T) {
	if err := Run(context.Background(), nil, Config{}); err == nil {
		t.Fatal("Run with nil module succeeded")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, validChatModule(), Config{DataDir: t.TempDir(), ListenAddr: "127.0.0.1:0\x00"})
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("Run listen error = %v, want concrete serving error", err)
	}
}

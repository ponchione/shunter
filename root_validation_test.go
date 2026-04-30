package shunter

import (
	"errors"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestBuildRejectsNilModuleBeforeSchemaBuild(t *testing.T) {
	_, err := Build(nil, Config{})
	if err == nil {
		t.Fatal("Build succeeded with nil module")
	}
	assertErrorMentions(t, err, "module")
}

func TestBuildRejectsBlankModuleNameBeforeSchemaBuild(t *testing.T) {
	_, err := Build(NewModule("   "), Config{})
	if err == nil {
		t.Fatal("Build succeeded with blank module name")
	}
	assertErrorMentions(t, err, "name")
	assertNotSchemaValidationError(t, err)
}

func TestBuildRejectsNegativeExecutorQueueCapacityBeforeSchemaBuild(t *testing.T) {
	_, err := Build(NewModule("chat"), Config{ExecutorQueueCapacity: -1})
	if err == nil {
		t.Fatal("Build succeeded with negative executor queue capacity")
	}
	assertErrorMentions(t, err, "executor")
	assertNotSchemaValidationError(t, err)
}

func TestBuildRejectsNegativeDurabilityQueueCapacityBeforeSchemaBuild(t *testing.T) {
	_, err := Build(NewModule("chat"), Config{DurabilityQueueCapacity: -1})
	if err == nil {
		t.Fatal("Build succeeded with negative durability queue capacity")
	}
	assertErrorMentions(t, err, "durability")
	assertNotSchemaValidationError(t, err)
}

func TestBuildRejectsInvalidAuthModeBeforeSchemaBuild(t *testing.T) {
	_, err := Build(NewModule("chat"), Config{AuthMode: AuthMode(99)})
	if err == nil {
		t.Fatal("Build succeeded with invalid auth mode")
	}
	assertErrorMentions(t, err, "auth")
	assertNotSchemaValidationError(t, err)
}

func TestBuildWithDefaultConfigReachesSchemaValidation(t *testing.T) {
	_, err := Build(NewModule("chat"), Config{})
	if err == nil {
		t.Fatal("Build succeeded for empty V1-A module; want schema validation failure")
	}
	if !errors.Is(err, schema.ErrSchemaVersionNotSet) && !errors.Is(err, schema.ErrNoTables) {
		t.Fatalf("Build error = %v, want schema validation error", err)
	}
}

func TestConfigKeepsFutureProtocolFieldsWithoutServingAPI(t *testing.T) {
	cfg := Config{
		EnableProtocol: true,
		ListenAddr:     "127.0.0.1:0",
		AuthMode:       AuthModeStrict,
	}

	if !cfg.EnableProtocol {
		t.Fatal("EnableProtocol was not retained on Config")
	}
	if cfg.ListenAddr != "127.0.0.1:0" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:0")
	}
	if cfg.AuthMode != AuthModeStrict {
		t.Fatalf("AuthMode = %v, want AuthModeStrict", cfg.AuthMode)
	}
}

func assertErrorMentions(t *testing.T, err error, want string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
		t.Fatalf("error %q does not mention %q", err, want)
	}
}

func assertNotSchemaValidationError(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, schema.ErrSchemaVersionNotSet) || errors.Is(err, schema.ErrNoTables) {
		t.Fatalf("error %v reached schema validation before root validation", err)
	}
}

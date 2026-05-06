package protocol

import (
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestProtocolV1CompatibilityPinsTokenAndVersionNumbers(t *testing.T) {
	if ProtocolVersionV1 != 1 {
		t.Fatalf("ProtocolVersionV1 = %d, want 1", ProtocolVersionV1)
	}
	if MinSupportedProtocolVersion != 1 {
		t.Fatalf("MinSupportedProtocolVersion = %d, want 1", MinSupportedProtocolVersion)
	}
	if CurrentProtocolVersion != 1 {
		t.Fatalf("CurrentProtocolVersion = %d, want 1", CurrentProtocolVersion)
	}
	if SubprotocolV1 != "v1.bsatn.shunter" {
		t.Fatalf("SubprotocolV1 = %q, want v1.bsatn.shunter", SubprotocolV1)
	}

	token, ok := SubprotocolForVersion(1)
	if !ok || token != "v1.bsatn.shunter" {
		t.Fatalf("SubprotocolForVersion(1) = %q, %v; want v1.bsatn.shunter, true", token, ok)
	}
	version, ok := ProtocolVersionForSubprotocol("v1.bsatn.shunter")
	if !ok || version != 1 {
		t.Fatalf("ProtocolVersionForSubprotocol(v1 token) = %d, %v; want 1, true", version, ok)
	}
}

func TestProtocolCompiledAggregateOrderByIsObservable(t *testing.T) {
	sl := newMockSchema("messages", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint64},
	)
	compiled, err := CompileSQLQueryString("SELECT COUNT(*) AS n FROM messages ORDER BY n", sl, nil, SQLQueryValidationOptions{
		AllowProjection: true,
		AllowOrderBy:    true,
	})
	if err != nil {
		t.Fatalf("CompileSQLQueryString returned error: %v", err)
	}
	if compiled.SubscriptionAggregate() == nil {
		t.Fatal("SubscriptionAggregate = nil, want COUNT aggregate")
	}
	if !compiled.HasOrderBy() {
		t.Fatal("HasOrderBy = false, want source ORDER BY to remain observable")
	}
}

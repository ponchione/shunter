package shunter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestV1CompatibilityModuleContractGolden(t *testing.T) {
	rt := buildV1CompatibilityRuntime(t)

	got, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	assertGoldenBytes(t, filepath.Join("testdata", "v1_module_contract.json"), got)

	var decoded ModuleContract
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("Unmarshal golden contract JSON: %v", err)
	}
	if err := ValidateModuleContract(decoded); err != nil {
		t.Fatalf("golden contract did not validate: %v", err)
	}
	recoded, err := decoded.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON after decode returned error: %v", err)
	}
	if !bytes.Equal(got, recoded) {
		t.Fatalf("golden contract did not canonicalize idempotently\nfirst:\n%s\nsecond:\n%s", got, recoded)
	}
}

func TestV1CompatibilityModuleContractJSONIgnoresUnknownFields(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "v1_module_contract.json"))
	if err != nil {
		t.Fatalf("read v1 contract fixture: %v", err)
	}
	withUnknown := v1ContractJSONWithUnknownFields(t, want)

	var decoded ModuleContract
	if err := json.Unmarshal(withUnknown, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON with unknown fields: %v", err)
	}
	if err := ValidateModuleContract(decoded); err != nil {
		t.Fatalf("contract with unknown fields did not validate: %v", err)
	}
	got, err := decoded.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unknown fields affected canonical contract JSON\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func buildV1CompatibilityRuntime(t *testing.T) *Runtime {
	t.Helper()

	mod := NewModule("v1_guardrails").
		Version("v1.0.0").
		Metadata(map[string]string{"owner": "v1-contract", "purpose": "compatibility-fixture"}).
		SchemaVersion(3).
		TableDef(v1CompatibilityMessagesTableDef(), schema.WithReadPermissions("messages:read")).
		Reducer("create_message", noopReducer, WithReducerPermissions(PermissionMetadata{
			Required: []string{"messages:write"},
		})).
		OnConnect(noopLifecycle).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE sender = :sender",
		}).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history", "v1"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
				Notes:           "declared query fixture",
			},
		}).
		View(ViewDeclaration{
			Name:        "live_message_projection",
			SQL:         "SELECT id, body AS text FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"projection", "v1"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
				Notes:           "declared live view projection fixture",
			},
		}).
		View(ViewDeclaration{
			Name:        "live_message_count",
			SQL:         "SELECT COUNT(*) AS n FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"aggregate", "v1"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
				Notes:           "declared live view count fixture",
			},
		}).
		Migration(MigrationMetadata{
			ModuleVersion:   "v1.0.0",
			SchemaVersion:   3,
			ContractVersion: ModuleContractVersion,
			PreviousVersion: "v0.9.0",
			Compatibility:   MigrationCompatibilityCompatible,
			Classifications: []MigrationClassification{MigrationClassificationAdditive},
			Notes:           "representative v1 contract fixture",
		}).
		TableMigration("messages", MigrationMetadata{
			Compatibility:   MigrationCompatibilityCompatible,
			Classifications: []MigrationClassification{MigrationClassificationAdditive},
			Notes:           "messages table fixture",
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return rt
}

func v1CompatibilityMessagesTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "messages",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "sender", Type: types.KindString},
			{Name: "body", Type: types.KindString},
			{Name: "sent_at", Type: types.KindTimestamp},
		},
		Indexes: []schema.IndexDefinition{
			{Name: "messages_sender_idx", Columns: []string{"sender"}},
			{Name: "messages_sent_at_idx", Columns: []string{"sent_at"}},
		},
	}
}

func v1ContractJSONWithUnknownFields(t *testing.T, data []byte) []byte {
	t.Helper()
	replacements := []struct {
		old string
		new string
	}{
		{
			old: "{\n  \"contract_version\": 1,",
			new: "{\n  \"future_top_level\": {\n    \"ignored\": true\n  },\n  \"contract_version\": 1,",
		},
		{
			old: "  \"module\": {\n    \"name\": \"v1_guardrails\",",
			new: "  \"module\": {\n    \"future_module_field\": \"ignored\",\n    \"name\": \"v1_guardrails\",",
		},
		{
			old: "  \"schema\": {\n    \"version\": 3,",
			new: "  \"schema\": {\n    \"future_schema_field\": [\n      \"ignored\"\n    ],\n    \"version\": 3,",
		},
		{
			old: "    {\n      \"name\": \"recent_messages\",\n      \"sql\": \"SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25\"\n    }",
			new: "    {\n      \"future_query_field\": \"ignored\",\n      \"name\": \"recent_messages\",\n      \"sql\": \"SELECT id, sender, body FROM messages ORDER BY sent_at DESC LIMIT 25\"\n    }",
		},
	}

	out := append([]byte(nil), data...)
	for _, replacement := range replacements {
		next := bytes.Replace(out, []byte(replacement.old), []byte(replacement.new), 1)
		if bytes.Equal(next, out) {
			t.Fatalf("v1 contract fixture missing replacement target %q", replacement.old)
		}
		out = next
	}
	return out
}

func assertGoldenBytes(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("SHUNTER_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("update golden file %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden file %s mismatch\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

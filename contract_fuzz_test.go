package shunter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func FuzzModuleContractJSONCanonicalRoundTrip(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		[]byte("not-json"),
		[]byte(`{"contract_version":0}`),
		contractFuzzAcceptedSeed(f),
		[]byte(`{
  "contract_version": 1,
  "module": {"name": "chat", "version": "v1.0.0", "metadata": {}},
  "schema": {"version": 1, "tables": [], "reducers": []},
  "queries": [{"Name": "recent_messages", "SQL": "SELECT * FROM messages"}],
  "views": [{"Name": "live_messages", "SQL": "SELECT * FROM messages"}],
  "permissions": {"reducers": [], "queries": [], "views": []},
  "read_model": {"declarations": []},
  "migrations": {"module": {"classifications": []}, "declarations": []},
  "codegen": {
    "contract_format": "shunter.module_contract",
    "contract_version": 1,
    "default_snapshot_filename": "shunter.contract.json"
  }
}`),
	} {
		f.Add(seed)
	}

	const maxContractFuzzBytes = 64 << 10
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxContractFuzzBytes {
			t.Skip("module contract fuzz input above bounded local limit")
		}
		label := contractFuzzLabel(data, maxContractFuzzBytes)

		var contract ModuleContract
		if err := json.Unmarshal(data, &contract); err != nil {
			return
		}
		if err := ValidateModuleContract(contract); err != nil {
			return
		}

		canonical, err := contract.MarshalCanonicalJSON()
		if err != nil {
			t.Fatalf("%s operation=MarshalCanonicalJSON observed_error=%v expected=nil", label, err)
		}
		if len(canonical) == 0 || canonical[len(canonical)-1] != '\n' {
			t.Fatalf("%s operation=MarshalCanonicalJSON observed_trailing_newline=%v expected=true canonical=%q",
				label, len(canonical) > 0 && canonical[len(canonical)-1] == '\n', canonical)
		}

		var decoded ModuleContract
		if err := json.Unmarshal(canonical, &decoded); err != nil {
			t.Fatalf("%s operation=UnmarshalCanonical observed_error=%v expected=nil canonical=%s", label, err, canonical)
		}
		if err := ValidateModuleContract(decoded); err != nil {
			t.Fatalf("%s operation=ValidateCanonical observed_error=%v expected=nil canonical=%s", label, err, canonical)
		}
		again, err := decoded.MarshalCanonicalJSON()
		if err != nil {
			t.Fatalf("%s operation=MarshalCanonicalJSONAgain observed_error=%v expected=nil", label, err)
		}
		if !bytes.Equal(canonical, again) {
			t.Fatalf("%s operation=CanonicalDeterminism observed=%s expected=%s", label, again, canonical)
		}
	})
}

func contractFuzzAcceptedSeed(tb testing.TB) []byte {
	tb.Helper()
	rt, err := Build(validChatModule().
		Version("v1.2.3").
		Metadata(map[string]string{"team": "runtime"}).
		Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"history"}},
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
			},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			ReadModel:   ReadModelMetadata{Tables: []string{"messages"}, Tags: []string{"realtime"}},
		}), Config{DataDir: tb.TempDir()})
	if err != nil {
		tb.Fatalf("build accepted contract fuzz seed: %v", err)
	}
	data, err := rt.ExportContractJSON()
	if err != nil {
		tb.Fatalf("export accepted contract fuzz seed: %v", err)
	}
	return data
}

func contractFuzzLabel(data []byte, maxBytes int) string {
	if len(data) <= 80 {
		return fmt.Sprintf("seed_len=%d seed=%x runtime_config=max_bytes=%d", len(data), data, maxBytes)
	}
	return fmt.Sprintf("seed_len=%d seed_prefix=%x runtime_config=max_bytes=%d", len(data), data[:80], maxBytes)
}

package shunter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

var contractFuzzLiteralSeeds = [][]byte{
	nil,
	[]byte("not-json"),
	[]byte(`{"contract_version":0}`),
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
}

const maxContractFuzzBytes = 64 << 10

func FuzzModuleContractJSONCanonicalRoundTrip(f *testing.F) {
	for _, seed := range contractJSONFuzzSeeds(f) {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxContractFuzzBytes {
			t.Skip("module contract fuzz input above bounded local limit")
		}
		assertModuleContractJSONInput(t, data)
	})
}

func contractJSONFuzzSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	seeds := append([][]byte(nil), contractFuzzLiteralSeeds...)
	accepted := contractFuzzAcceptedSeed(tb)
	seeds = append(seeds, accepted)
	seeds = append(seeds, contractFuzzSemanticInvalidSeeds(tb, accepted)...)
	return seeds
}

func contractFuzzSemanticInvalidSeeds(tb testing.TB, accepted []byte) [][]byte {
	tb.Helper()

	return [][]byte{
		mustContractFuzzMutatedJSON(tb, accepted, func(contract *ModuleContract) {
			contract.Queries[0].SQL = "SELECT * FROM missing_table"
		}),
		mustContractFuzzMutatedJSON(tb, accepted, func(contract *ModuleContract) {
			contract.Views[0].SQL = "SELECT * FROM missing_table"
		}),
		mustContractFuzzMutatedJSON(tb, accepted, func(contract *ModuleContract) {
			contract.Permissions.Queries = append(contract.Permissions.Queries, PermissionContractDeclaration{
				Name:     "missing_query",
				Required: []string{"messages:read"},
			})
		}),
		mustContractFuzzMutatedJSON(tb, accepted, func(contract *ModuleContract) {
			contract.ReadModel.Declarations[0].Tables = []string{"missing_table"}
		}),
		mustContractFuzzMutatedJSON(tb, accepted, func(contract *ModuleContract) {
			contract.VisibilityFilters = append(contract.VisibilityFilters, VisibilityFilterDescription{
				Name:          "missing_table_filter",
				SQL:           "SELECT * FROM missing_table",
				ReturnTable:   "messages",
				ReturnTableID: 0,
			})
		}),
	}
}

func mustContractFuzzMutatedJSON(tb testing.TB, accepted []byte, mutate func(*ModuleContract)) []byte {
	tb.Helper()
	var contract ModuleContract
	if err := json.Unmarshal(accepted, &contract); err != nil {
		tb.Fatalf("unmarshal accepted contract fuzz seed: %v", err)
	}
	mutate(&contract)
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		tb.Fatalf("marshal mutated contract fuzz seed: %v", err)
	}
	return data
}

func assertModuleContractJSONInput(tb testing.TB, data []byte) {
	tb.Helper()
	if err := checkModuleContractJSONInput(data); err != nil {
		tb.Fatal(err)
	}
}

func checkModuleContractJSONInput(data []byte) error {
	label := contractFuzzLabel(data, maxContractFuzzBytes)

	var contract ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return nil
	}
	if err := ValidateModuleContract(contract); err != nil {
		return nil
	}

	canonical, err := contract.MarshalCanonicalJSON()
	if err != nil {
		return fmt.Errorf("%s operation=MarshalCanonicalJSON observed_error=%v expected=nil", label, err)
	}
	if len(canonical) == 0 || canonical[len(canonical)-1] != '\n' {
		return fmt.Errorf("%s operation=MarshalCanonicalJSON observed_trailing_newline=%v expected=true canonical=%q",
			label, len(canonical) > 0 && canonical[len(canonical)-1] == '\n', canonical)
	}

	var decoded ModuleContract
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		return fmt.Errorf("%s operation=UnmarshalCanonical observed_error=%v expected=nil canonical=%s", label, err, canonical)
	}
	if err := ValidateModuleContract(decoded); err != nil {
		return fmt.Errorf("%s operation=ValidateCanonical observed_error=%v expected=nil canonical=%s", label, err, canonical)
	}
	again, err := decoded.MarshalCanonicalJSON()
	if err != nil {
		return fmt.Errorf("%s operation=MarshalCanonicalJSONAgain observed_error=%v expected=nil", label, err)
	}
	if !bytes.Equal(canonical, again) {
		return fmt.Errorf("%s operation=CanonicalDeterminism observed=%s expected=%s", label, again, canonical)
	}
	return nil
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

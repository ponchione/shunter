package contractdiff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

func TestContractDiffAndPlanDeclarationOrderMetamorphic(t *testing.T) {
	old, current := contractOrderMetamorphicFixtures(t)

	wantReportText := Compare(old, current).Text()
	wantPlanJSON := mustMetamorphicPlanJSON(t, Plan(old, current, PlanOptions{
		Policy:            PolicyOptions{RequirePreviousVersion: true},
		ValidateContracts: true,
	}))

	const seed int64 = 0x5eedc0de
	for iteration := 0; iteration < 32; iteration++ {
		r := rand.New(rand.NewSource(seed + int64(iteration)*7919))
		shuffledOld := shuffleContractDeclarationOrder(cloneMetamorphicContract(t, old), r)
		shuffledCurrent := shuffleContractDeclarationOrder(cloneMetamorphicContract(t, current), r)

		if got := Compare(shuffledOld, shuffledCurrent).Text(); got != wantReportText {
			t.Fatalf("seed=%d iteration=%d operation=CompareOrderInvariant\nobserved:\n%s\nexpected:\n%s",
				seed, iteration, got, wantReportText)
		}

		gotPlanJSON := mustMetamorphicPlanJSON(t, Plan(shuffledOld, shuffledCurrent, PlanOptions{
			Policy:            PolicyOptions{RequirePreviousVersion: true},
			ValidateContracts: true,
		}))
		if !bytes.Equal(gotPlanJSON, wantPlanJSON) {
			t.Fatalf("seed=%d iteration=%d operation=PlanOrderInvariant\nobserved:\n%s\nexpected:\n%s",
				seed, iteration, gotPlanJSON, wantPlanJSON)
		}
	}
}

func TestContractPolicyDeclarationOrderMetamorphic(t *testing.T) {
	old, current := contractOrderMetamorphicFixtures(t)
	current.Migrations = shunter.MigrationContract{}
	opts := PolicyOptions{RequirePreviousVersion: true, Strict: true}

	want := CheckPolicy(Compare(old, current), current, opts)
	if !want.Failed || len(want.Warnings) < 5 {
		t.Fatalf("policy fixture produced failed=%v warnings=%#v, want strict warnings", want.Failed, want.Warnings)
	}
	wantWarnings := policyWarningSignatures(want.Warnings)

	const seed int64 = 0x7011c1e5
	for iteration := 0; iteration < 32; iteration++ {
		r := rand.New(rand.NewSource(seed + int64(iteration)*7919))
		shuffledOld := shuffleContractDeclarationOrder(cloneMetamorphicContract(t, old), r)
		shuffledCurrent := shuffleContractDeclarationOrder(cloneMetamorphicContract(t, current), r)

		got := CheckPolicy(Compare(shuffledOld, shuffledCurrent), shuffledCurrent, opts)
		if got.Failed != want.Failed {
			t.Fatalf("seed=%d iteration=%d operation=PolicyFailedOrderInvariant observed=%v expected=%v warnings=%#v",
				seed, iteration, got.Failed, want.Failed, got.Warnings)
		}
		gotWarnings := policyWarningSignatures(got.Warnings)
		if !equalStringSlices(gotWarnings, wantWarnings) {
			t.Fatalf("seed=%d iteration=%d operation=PolicyWarningsOrderInvariant\nobserved=%q\nexpected=%q",
				seed, iteration, gotWarnings, wantWarnings)
		}
	}
}

func contractOrderMetamorphicFixtures(tb testing.TB) (shunter.ModuleContract, shunter.ModuleContract) {
	tb.Helper()

	old := contractFixture()
	old.Module.Metadata = map[string]string{
		"region": "use1",
		"team":   "runtime",
	}
	old.Schema.Tables = append(old.Schema.Tables, schema.TableExport{
		Name: "members",
		Columns: []schema.ColumnExport{
			{Name: "id", Type: "uint64"},
			{Name: "display_name", Type: "string"},
		},
		Indexes: []schema.IndexExport{
			{Name: "members_pk", Columns: []string{"id"}, Unique: true, Primary: true},
			{Name: "members_name", Columns: []string{"display_name"}},
		},
	})
	old.Schema.Reducers = append(old.Schema.Reducers, schema.ReducerExport{Name: "update_member"})
	old.Queries = append(old.Queries, shunter.QueryDescription{Name: "member_lookup"})
	old.Views = append(old.Views, shunter.ViewDescription{Name: "members_live"})
	old.ReadModel.Declarations = []shunter.ReadModelContractDeclaration{
		{Surface: shunter.ReadModelSurfaceQuery, Name: "history", Tables: []string{"messages"}, Tags: []string{"history"}},
		{Surface: shunter.ReadModelSurfaceView, Name: "live", Tables: []string{"messages"}, Tags: []string{"live"}},
	}

	current := cloneMetamorphicContract(tb, old)
	current.Module.Version = "v1.1.0"
	current.Module.Metadata = map[string]string{
		"region": "use1",
		"team":   "platform",
		"tier":   "gauntlet",
	}
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	current.Schema.Tables[1].Columns[1].Type = "bytes"
	current.Schema.Tables = append(current.Schema.Tables, schema.TableExport{
		Name: "audit_log",
		Columns: []schema.ColumnExport{
			{Name: "id", Type: "uint64"},
			{Name: "message_id", Type: "uint64"},
		},
		Indexes: []schema.IndexExport{{Name: "audit_log_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
	})
	current.Schema.Reducers = append(current.Schema.Reducers, schema.ReducerExport{Name: "archive_message"})
	current.Queries[0].SQL = "SELECT * FROM messages"
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "audit_history"})
	current.Views[0].SQL = "SELECT * FROM messages"
	current.Views = append(current.Views, shunter.ViewDescription{Name: "audit_live"})
	current.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:          "published_messages",
		SQL:           "SELECT * FROM messages WHERE body = 'published'",
		ReturnTable:   "messages",
		ReturnTableID: 0,
	}}
	current.Permissions.Queries = []shunter.PermissionContractDeclaration{
		{Name: "history", Required: []string{"messages:read"}},
		{Name: "audit_history", Required: []string{"audit:read"}},
	}
	current.Permissions.Views = []shunter.PermissionContractDeclaration{
		{Name: "live", Required: []string{"messages:subscribe"}},
		{Name: "audit_live", Required: []string{"audit:subscribe"}},
	}
	current.ReadModel.Declarations = append(current.ReadModel.Declarations,
		shunter.ReadModelContractDeclaration{Surface: shunter.ReadModelSurfaceQuery, Name: "audit_history", Tables: []string{"audit_log"}, Tags: []string{"audit"}},
		shunter.ReadModelContractDeclaration{Surface: shunter.ReadModelSurfaceView, Name: "audit_live", Tables: []string{"audit_log"}, Tags: []string{"audit"}},
	)
	current.Migrations.Module = shunter.MigrationMetadata{
		ModuleVersion:   "v1.1.0",
		SchemaVersion:   current.Schema.Version,
		ContractVersion: current.ContractVersion,
		PreviousVersion: old.Module.Version,
		Compatibility:   shunter.MigrationCompatibilityBreaking,
		Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationManualReviewNeeded},
		Notes:           "fixed-seed declaration order metamorphic coverage",
	}
	current.Migrations.Declarations = []shunter.MigrationContractDeclaration{
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "messages",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationAdditive},
				Notes:           "add sent_at",
			},
		},
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "members",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityBreaking,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationDataRewriteNeeded},
				Notes:           "rewrite member display names",
			},
		},
		{
			Surface: shunter.MigrationSurfaceTable,
			Name:    "audit_log",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationAdditive},
				Notes:           "new audit table",
			},
		},
		{
			Surface: shunter.MigrationSurfaceQuery,
			Name:    "history",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationManualReviewNeeded},
				Notes:           "declared history query",
			},
		},
		{
			Surface: shunter.MigrationSurfaceQuery,
			Name:    "audit_history",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationAdditive},
				Notes:           "new audit query",
			},
		},
		{
			Surface: shunter.MigrationSurfaceView,
			Name:    "live",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationManualReviewNeeded},
				Notes:           "declared live view",
			},
		},
		{
			Surface: shunter.MigrationSurfaceView,
			Name:    "audit_live",
			Metadata: shunter.MigrationMetadata{
				Compatibility:   shunter.MigrationCompatibilityCompatible,
				Classifications: []shunter.MigrationClassification{shunter.MigrationClassificationAdditive},
				Notes:           "new audit view",
			},
		},
	}

	return old, current
}

func shuffleContractDeclarationOrder(contract shunter.ModuleContract, r *rand.Rand) shunter.ModuleContract {
	shuffleMetamorphicSlice(r, contract.Schema.Tables)
	for i := range contract.Schema.Tables {
		shuffleMetamorphicSlice(r, contract.Schema.Tables[i].Columns)
		shuffleMetamorphicSlice(r, contract.Schema.Tables[i].Indexes)
	}
	shuffleMetamorphicSlice(r, contract.Schema.Reducers)
	shuffleMetamorphicSlice(r, contract.Queries)
	shuffleMetamorphicSlice(r, contract.Views)
	shuffleMetamorphicSlice(r, contract.VisibilityFilters)
	shuffleMetamorphicSlice(r, contract.Permissions.Reducers)
	shuffleMetamorphicSlice(r, contract.Permissions.Queries)
	shuffleMetamorphicSlice(r, contract.Permissions.Views)
	shuffleMetamorphicSlice(r, contract.ReadModel.Declarations)
	shuffleMetamorphicSlice(r, contract.Migrations.Declarations)
	return contract
}

func shuffleMetamorphicSlice[T any](r *rand.Rand, values []T) {
	r.Shuffle(len(values), func(i, j int) {
		values[i], values[j] = values[j], values[i]
	})
}

func cloneMetamorphicContract(tb testing.TB, contract shunter.ModuleContract) shunter.ModuleContract {
	tb.Helper()
	data, err := json.Marshal(contract)
	if err != nil {
		tb.Fatalf("marshal metamorphic contract clone: %v", err)
	}
	var cloned shunter.ModuleContract
	if err := json.Unmarshal(data, &cloned); err != nil {
		tb.Fatalf("unmarshal metamorphic contract clone: %v", err)
	}
	return cloned
}

func mustMetamorphicPlanJSON(t *testing.T, plan MigrationPlan) []byte {
	t.Helper()
	data, err := plan.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("operation=MarshalCanonicalJSON observed_trailing_newline=%v expected=true plan=%s",
			len(data) > 0 && data[len(data)-1] == '\n', data)
	}
	return data
}

func policyWarningSignatures(warnings []PolicyWarning) []string {
	signatures := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		signatures = append(signatures, fmt.Sprintf("%s %s %s: %s", warning.Code, warning.Surface, warning.Name, warning.Detail))
	}
	return signatures
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package shunter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestRuntimeExportContractIncludesMigrationMetadata(t *testing.T) {
	mod := validChatModule().
		Version("v2.0.0").
		Migration(MigrationMetadata{
			ModuleVersion:   "v2.0.0",
			SchemaVersion:   2,
			ContractVersion: ModuleContractVersion,
			PreviousVersion: "v1.0.0",
			Compatibility:   MigrationCompatibilityBreaking,
			Classifications: []MigrationClassification{
				MigrationClassificationDataRewriteNeeded,
				MigrationClassificationManualReviewNeeded,
			},
			Notes: "messages payload changes shape",
		}).
		TableMigration("messages", MigrationMetadata{
			Compatibility:   MigrationCompatibilityBreaking,
			Classifications: []MigrationClassification{MigrationClassificationDataRewriteNeeded},
			Notes:           "backfill message bodies",
		}).
		Query(QueryDeclaration{
			Name: "recent_messages",
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityCompatible,
				Classifications: []MigrationClassification{MigrationClassificationAdditive},
				Notes:           "query is new",
			},
		}).
		View(ViewDeclaration{
			Name: "live_messages",
			Migration: MigrationMetadata{
				Compatibility:   MigrationCompatibilityUnknown,
				Classifications: []MigrationClassification{MigrationClassificationManualReviewNeeded},
				Notes:           "subscription shape needs review",
			},
		})

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	contract := rt.ExportContract()
	if contract.Migrations.Module.ModuleVersion != "v2.0.0" {
		t.Fatalf("module migration version = %q, want v2.0.0", contract.Migrations.Module.ModuleVersion)
	}
	if contract.Migrations.Module.SchemaVersion != 2 {
		t.Fatalf("module schema version = %d, want 2", contract.Migrations.Module.SchemaVersion)
	}
	if contract.Migrations.Module.ContractVersion != ModuleContractVersion {
		t.Fatalf("module contract version = %d, want %d", contract.Migrations.Module.ContractVersion, ModuleContractVersion)
	}
	if contract.Migrations.Module.PreviousVersion != "v1.0.0" {
		t.Fatalf("module previous version = %q, want v1.0.0", contract.Migrations.Module.PreviousVersion)
	}
	if contract.Migrations.Module.Compatibility != MigrationCompatibilityBreaking {
		t.Fatalf("module compatibility = %q, want breaking", contract.Migrations.Module.Compatibility)
	}
	assertMigrationDeclaration(t, contract.Migrations.Declarations, MigrationSurfaceTable, "messages", MigrationCompatibilityBreaking, MigrationClassificationDataRewriteNeeded)
	assertMigrationDeclaration(t, contract.Migrations.Declarations, MigrationSurfaceQuery, "recent_messages", MigrationCompatibilityCompatible, MigrationClassificationAdditive)
	assertMigrationDeclaration(t, contract.Migrations.Declarations, MigrationSurfaceView, "live_messages", MigrationCompatibilityUnknown, MigrationClassificationManualReviewNeeded)
}

func TestRuntimeExportContractMigrationMetadataJSONIsDeterministic(t *testing.T) {
	rt, err := Build(validChatModule().Migration(MigrationMetadata{
		ModuleVersion:   "v1.1.0",
		SchemaVersion:   1,
		ContractVersion: ModuleContractVersion,
		PreviousVersion: "v1.0.0",
		Compatibility:   MigrationCompatibilityCompatible,
		Classifications: []MigrationClassification{MigrationClassificationAdditive},
	}), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	first, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("ExportContractJSON returned error: %v", err)
	}
	second, err := rt.ExportContractJSON()
	if err != nil {
		t.Fatalf("second ExportContractJSON returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("migration contract JSON was not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	var decoded ModuleContract
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("Unmarshal contract JSON: %v", err)
	}
	if decoded.Migrations.Module.Compatibility != MigrationCompatibilityCompatible {
		t.Fatalf("decoded compatibility = %q, want compatible", decoded.Migrations.Module.Compatibility)
	}
	if got := decoded.Migrations.Module.Classifications; len(got) != 1 || got[0] != MigrationClassificationAdditive {
		t.Fatalf("decoded classifications = %#v, want additive", got)
	}
}

func TestMissingMigrationMetadataDoesNotBlockRuntimeBuildOrStart(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error without migration metadata: %v", err)
	}
	if err := rt.Start(t.Context()); err != nil {
		t.Fatalf("Start returned error without migration metadata: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	contract := rt.ExportContract()
	if contract.Migrations.Module.Classifications == nil {
		t.Fatalf("missing module migration classifications = nil, want stable empty slice")
	}
	if contract.Migrations.Declarations == nil {
		t.Fatalf("missing migration declarations = nil, want stable empty slice")
	}
}

func TestTableMigrationMetadataForUnknownTableFailsBuildWithoutFreezingModule(t *testing.T) {
	mod := validChatModule().TableMigration("missing", MigrationMetadata{
		Compatibility: MigrationCompatibilityBreaking,
		Notes:         "typo should not become dead contract metadata",
	})

	_, err := Build(mod, Config{DataDir: t.TempDir()})
	if err == nil || !errors.Is(err, ErrUnknownTableMigration) {
		t.Fatalf("expected ErrUnknownTableMigration, got %v", err)
	}

	missing := messagesTableDef()
	missing.Name = "missing"
	mod.TableDef(missing)
	if _, err := Build(mod, Config{DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Build after adding missing table returned error: %v", err)
	}
}

func TestMigrationHookCommitsDurablyBeforeRuntimeReady(t *testing.T) {
	dir := t.TempDir()
	hookCalls := 0
	mod := validChatModule().
		Version("v2.0.0").
		Reducer("insert_after_migration", func(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
			_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(2), types.NewString("after")})
			return nil, err
		}).
		MigrationHook(func(ctx context.Context, mc *MigrationContext) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			hookCalls++
			if mc.ModuleName() != "chat" {
				return fmt.Errorf("module name = %q, want chat", mc.ModuleName())
			}
			if mc.ModuleVersion() != "v2.0.0" {
				return fmt.Errorf("module version = %q, want v2.0.0", mc.ModuleVersion())
			}
			if mc.CommittedTxID() != 0 {
				return fmt.Errorf("committed tx id = %d, want bootstrap horizon", mc.CommittedTxID())
			}
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("seed")})
			return err
		})

	if err := CheckDataDirCompatibility(mod, Config{DataDir: dir}); err != nil {
		t.Fatalf("CheckDataDirCompatibility: %v", err)
	}
	if hookCalls != 0 {
		t.Fatalf("migration hook ran during preflight")
	}
	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if hookCalls != 0 {
		t.Fatalf("migration hook ran during Build")
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if hookCalls != 1 {
		t.Fatalf("migration hook calls = %d, want 1", hookCalls)
	}
	requireMigrationMessageBodies(t, rt, "seed")

	res, err := rt.CallReducer(context.Background(), "insert_after_migration", nil)
	if err != nil {
		t.Fatalf("CallReducer after migration hook: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}
	if res.TxID != 2 {
		t.Fatalf("first post-migration reducer tx id = %d, want 2", res.TxID)
	}
	requireMigrationMessageBodies(t, rt, "seed", "after")
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	restarted, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("restarted Build: %v", err)
	}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatalf("restarted Start: %v", err)
	}
	defer restarted.Close()
	requireMigrationMessageBodies(t, restarted, "seed", "after")
}

func TestMigrationHookFailureRollsBackAndBlocksStart(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("boom")
	mod := validChatModule().MigrationHook(func(_ context.Context, mc *MigrationContext) error {
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		if _, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("rolled-back")}); err != nil {
			return err
		}
		return boom
	})

	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	err = rt.Start(context.Background())
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("Start error = %v, want boom", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after migration hook failure")
	}

	restarted, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("restarted Build: %v", err)
	}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatalf("restarted Start: %v", err)
	}
	defer restarted.Close()
	requireMigrationMessageBodies(t, restarted)
}

func assertMigrationDeclaration(t *testing.T, declarations []MigrationContractDeclaration, surface, name string, compatibility MigrationCompatibility, classification MigrationClassification) {
	t.Helper()
	for _, declaration := range declarations {
		if declaration.Surface != surface || declaration.Name != name {
			continue
		}
		if declaration.Metadata.Compatibility != compatibility {
			t.Fatalf("%s %q compatibility = %q, want %q", surface, name, declaration.Metadata.Compatibility, compatibility)
		}
		for _, got := range declaration.Metadata.Classifications {
			if got == classification {
				return
			}
		}
		t.Fatalf("%s %q classifications = %#v, want %q", surface, name, declaration.Metadata.Classifications, classification)
	}
	t.Fatalf("migration declarations = %#v, want %s %q", declarations, surface, name)
}

func requireMigrationMessageBodies(t *testing.T, rt *Runtime, want ...string) {
	t.Helper()
	got := make(map[string]int, len(want))
	err := rt.Read(context.Background(), func(view LocalReadView) error {
		for _, row := range view.TableScan(0) {
			if len(row) < 2 {
				return fmt.Errorf("row = %#v, want body column", row)
			}
			got[row[1].AsString()]++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Read migration rows: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("message bodies = %#v, want %#v", got, want)
	}
	for _, body := range want {
		if got[body] != 1 {
			t.Fatalf("message bodies = %#v, want one %q", got, body)
		}
	}
}

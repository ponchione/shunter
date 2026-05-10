package shunter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
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

func TestRuntimeStartSnapshotsRegisteredMigrationHooks(t *testing.T) {
	dir := t.TempDir()
	var calls []string
	mod := validChatModule()
	mod.MigrationHook(func(_ context.Context, mc *MigrationContext) error {
		calls = append(calls, "first")
		mod.migrationHooks[1] = func(context.Context, *MigrationContext) error {
			calls = append(calls, "mutated")
			return fmt.Errorf("mutated hook ran during runtime start")
		}
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("first-start-hook")})
		return err
	})
	mod.MigrationHook(func(_ context.Context, mc *MigrationContext) error {
		calls = append(calls, "second")
		if mc.CommittedTxID() != 1 {
			return fmt.Errorf("committed tx id = %d, want first hook horizon", mc.CommittedTxID())
		}
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(2), types.NewString("second-start-hook")})
		return err
	})

	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build with snapshot startup hooks: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start with snapshot startup hooks: %v", err)
	}
	defer rt.Close()
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("startup migration hook calls = %#v, want first then second", calls)
	}
	requireMigrationMessageBodies(t, rt, "first-start-hook", "second-start-hook")
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

func TestMigrationHookContextCancelAfterHookRollsBackAndBlocksStart(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	hookCalls := 0
	mod := validChatModule().MigrationHook(func(_ context.Context, mc *MigrationContext) error {
		hookCalls++
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		if _, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("startup-canceled")}); err != nil {
			return err
		}
		cancel()
		return nil
	})

	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	err = rt.Start(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context.Canceled", err)
	}
	if hookCalls != 1 {
		t.Fatalf("hook calls = %d, want 1", hookCalls)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after canceled migration hook")
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

func TestMigrationHookContextCancelAfterSecondHookAllowsSameRuntimeRetryFromDurableHorizon(t *testing.T) {
	dir := t.TempDir()
	startCtx, cancel := context.WithCancel(context.Background())
	cancelFirstAttempt := true
	firstCalls := 0
	secondCalls := 0
	mod := validChatModule().
		MigrationHook(func(_ context.Context, mc *MigrationContext) error {
			firstCalls++
			if mc.CommittedTxID() != 0 {
				return nil
			}
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("startup-first")})
			return err
		}).
		MigrationHook(func(_ context.Context, mc *MigrationContext) error {
			secondCalls++
			if mc.CommittedTxID() != 1 {
				return fmt.Errorf("second hook committed tx id = %d, want durable first hook horizon", mc.CommittedTxID())
			}
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			body := "startup-second-after-retry"
			if cancelFirstAttempt {
				body = "startup-second-canceled"
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(2), types.NewString(body)})
			if err != nil {
				return err
			}
			if cancelFirstAttempt {
				cancel()
			}
			return err
		})

	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	err = rt.Start(startCtx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context.Canceled", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after canceled second migration hook")
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("hook calls after cancellation first/second = %d/%d, want 1/1", firstCalls, secondCalls)
	}
	health := rt.Health()
	if health.State != RuntimeStateFailed || health.Ready || health.LastError == "" {
		t.Fatalf("health after canceled second migration hook = %+v, want failed not-ready with LastError", health)
	}

	cancelFirstAttempt = false
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("same-runtime retry after canceled second migration hook: %v", err)
	}
	if firstCalls != 2 || secondCalls != 2 {
		t.Fatalf("hook calls after same-runtime retry first/second = %d/%d, want 2/2", firstCalls, secondCalls)
	}
	requireMigrationMessageBodies(t, rt, "startup-first", "startup-second-after-retry")
	if err := rt.Close(); err != nil {
		t.Fatalf("Close after same-runtime retry: %v", err)
	}

	restarted, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("restarted Build: %v", err)
	}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatalf("restarted Start: %v", err)
	}
	defer restarted.Close()
	requireMigrationMessageBodies(t, restarted, "startup-first", "startup-second-after-retry")
}

func TestMigrationHookDurabilityFailureBlocksSameRuntimeRetry(t *testing.T) {
	dir := t.TempDir()
	injected := errors.New("injected durability failure")
	injectFailure := true
	prevHook := migrationAfterCommitBeforeDurabilityHook
	migrationAfterCommitBeforeDurabilityHook = func(types.TxID, *store.Changeset) error {
		if !injectFailure {
			return nil
		}
		return injected
	}
	t.Cleanup(func() {
		migrationAfterCommitBeforeDurabilityHook = prevHook
	})

	hookCalls := 0
	hook := func(_ context.Context, mc *MigrationContext) error {
		hookCalls++
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("dirty-only")})
		return err
	}
	mod := validChatModule().MigrationHook(hook)

	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	err = rt.Start(context.Background())
	if !errors.Is(err, ErrRuntimeRestartRequired) || !errors.Is(err, injected) {
		t.Fatalf("Start error = %v, want ErrRuntimeRestartRequired wrapping injected failure", err)
	}
	if hookCalls != 1 {
		t.Fatalf("hook calls = %d, want 1", hookCalls)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after dirty migration durability failure")
	}
	health := rt.Health()
	if health.State != RuntimeStateFailed || health.Ready || !health.Degraded {
		t.Fatalf("health after dirty migration failure = %+v, want failed not-ready degraded", health)
	}
	if health.LastError == "" {
		t.Fatal("LastError not recorded after dirty migration durability failure")
	}

	err = rt.Start(context.Background())
	if !errors.Is(err, ErrRuntimeRestartRequired) || !errors.Is(err, injected) {
		t.Fatalf("same-runtime retry error = %v, want ErrRuntimeRestartRequired wrapping injected failure", err)
	}
	if hookCalls != 1 {
		t.Fatalf("same-runtime retry reran migration hook; calls = %d, want 1", hookCalls)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after blocked same-runtime retry")
	}

	injectFailure = false
	restarted, err := Build(validChatModule().MigrationHook(hook), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("restarted Build: %v", err)
	}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatalf("restarted Start: %v", err)
	}
	defer restarted.Close()
	if hookCalls != 2 {
		t.Fatalf("fresh-runtime retry hook calls = %d, want 2", hookCalls)
	}
	requireMigrationMessageBodies(t, restarted, "dirty-only")
}

func TestMigrationHookLaterFailureAllowsSameRuntimeRetryFromDurableHorizon(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("later hook failed")
	secondFails := true
	firstCalls := 0
	secondCalls := 0

	mod := validChatModule().
		MigrationHook(func(_ context.Context, mc *MigrationContext) error {
			firstCalls++
			if mc.CommittedTxID() != 0 {
				return nil
			}
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("durable-first")})
			return err
		}).
		MigrationHook(func(_ context.Context, mc *MigrationContext) error {
			secondCalls++
			if secondFails {
				return boom
			}
			if mc.CommittedTxID() != 1 {
				return fmt.Errorf("second hook committed tx id = %d, want durable first hook horizon", mc.CommittedTxID())
			}
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(2), types.NewString("second-after-retry")})
			return err
		})

	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	err = rt.Start(context.Background())
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("Start error = %v, want later hook failure", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after later migration hook failure")
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("hook calls after failure first/second = %d/%d, want 1/1", firstCalls, secondCalls)
	}
	health := rt.Health()
	if health.State != RuntimeStateFailed || health.Ready || health.LastError == "" {
		t.Fatalf("health after later hook failure = %+v, want failed not-ready with LastError", health)
	}

	secondFails = false
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("same-runtime retry after non-dirty later hook failure: %v", err)
	}
	defer rt.Close()
	if firstCalls != 2 || secondCalls != 2 {
		t.Fatalf("hook calls after same-runtime retry first/second = %d/%d, want 2/2", firstCalls, secondCalls)
	}
	requireMigrationMessageBodies(t, rt, "durable-first", "second-after-retry")
}

func TestRuntimeCloseDuringMigrationHookKeepsRuntimeNotReadyAndCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	hookStarted := make(chan struct{})
	releaseHook := make(chan struct{})
	hookCalls := 0
	mod := validChatModule().MigrationHook(func(ctx context.Context, _ *MigrationContext) error {
		hookCalls++
		close(hookStarted)
		select {
		case <-releaseHook:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	startErr := make(chan error, 1)
	go func() {
		startErr <- rt.Start(context.Background())
	}()
	<-hookStarted

	if err := rt.Close(); err != nil {
		t.Fatalf("Close during migration hook: %v", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready immediately after Close during migration hook")
	}
	if got := rt.Health().State; got != RuntimeStateClosed {
		t.Fatalf("state after Close during migration hook = %q, want closed", got)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close during blocked startup: %v", err)
	}

	close(releaseHook)
	if err := <-startErr; !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("Start after Close during migration hook error = %v, want ErrRuntimeClosed", err)
	}
	if hookCalls != 1 {
		t.Fatalf("hook calls = %d, want 1", hookCalls)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after Start unwound from Close during migration hook")
	}
	health := rt.Health()
	if health.State != RuntimeStateClosed || health.Ready ||
		health.Durability.Started || health.Executor.Started || health.Subscriptions.Started {
		t.Fatalf("health after Start unwound from Close during migration hook = %+v, want closed with stopped subsystems", health)
	}
	if rt.durability != nil || rt.executor != nil || rt.scheduler != nil || rt.fanOutWorker != nil || rt.subscriptions != nil {
		t.Fatalf("partial resources retained after Close during migration hook: health=%+v", health)
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

func TestRunDataDirMigrationsExecutesOfflineAndLeavesModuleBuildable(t *testing.T) {
	dir := t.TempDir()
	mod := validChatModule()

	result, err := RunDataDirMigrations(context.Background(), mod, Config{DataDir: dir}, func(_ context.Context, mc *MigrationContext) error {
		if mc.ModuleName() != "chat" {
			return fmt.Errorf("module name = %q, want chat", mc.ModuleName())
		}
		if mc.CommittedTxID() != 0 {
			return fmt.Errorf("committed tx id = %d, want bootstrap horizon", mc.CommittedTxID())
		}
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("offline-seed")})
		return err
	})
	if err != nil {
		t.Fatalf("RunDataDirMigrations: %v", err)
	}
	if result.DataDir != dir {
		t.Fatalf("result data dir = %q, want %q", result.DataDir, dir)
	}
	if result.RecoveredTxID != 0 || result.DurableTxID != 1 {
		t.Fatalf("result tx ids recovered/durable = %d/%d, want 0/1", result.RecoveredTxID, result.DurableTxID)
	}
	requireMigrationHookResults(t, result.Hooks, MigrationHookResult{Index: 0, TxID: 1, Changed: true})

	mod.Reducer("insert_after_runner", func(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
		_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(2), types.NewString("after-runner")})
		return nil, err
	})
	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build after explicit migration runner: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start after explicit migration runner: %v", err)
	}
	requireMigrationMessageBodies(t, rt, "offline-seed")

	res, err := rt.CallReducer(context.Background(), "insert_after_runner", nil)
	if err != nil {
		t.Fatalf("CallReducer after explicit migration runner: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}
	if res.TxID != 2 {
		t.Fatalf("first runtime tx id after explicit migration = %d, want 2", res.TxID)
	}
	requireMigrationMessageBodies(t, rt, "offline-seed", "after-runner")
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
	requireMigrationMessageBodies(t, restarted, "offline-seed", "after-runner")
}

func TestRunModuleDataDirMigrationsUsesRegisteredHooks(t *testing.T) {
	dir := t.TempDir()
	mod := validChatModule().
		Reducer("insert_after_module_runner", func(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
			_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(2), types.NewString("after-module-runner")})
			return nil, err
		}).
		MigrationHook(func(_ context.Context, mc *MigrationContext) error {
			if mc.CommittedTxID() != 0 {
				return nil
			}
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("registered-hook")})
			return err
		})

	result, err := RunModuleDataDirMigrations(context.Background(), mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("RunModuleDataDirMigrations: %v", err)
	}
	if result.DataDir != dir {
		t.Fatalf("result data dir = %q, want %q", result.DataDir, dir)
	}
	if result.RecoveredTxID != 0 || result.DurableTxID != 1 {
		t.Fatalf("result tx ids recovered/durable = %d/%d, want 0/1", result.RecoveredTxID, result.DurableTxID)
	}
	requireMigrationHookResults(t, result.Hooks, MigrationHookResult{Index: 0, TxID: 1, Changed: true})

	rt, err := Build(mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build after module migration runner: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start after module migration runner: %v", err)
	}
	defer rt.Close()
	requireMigrationMessageBodies(t, rt, "registered-hook")

	res, err := rt.CallReducer(context.Background(), "insert_after_module_runner", nil)
	if err != nil {
		t.Fatalf("CallReducer after module migration runner: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}
	if res.TxID != 2 {
		t.Fatalf("first runtime tx id after module migration runner = %d, want 2", res.TxID)
	}
	requireMigrationMessageBodies(t, rt, "registered-hook", "after-module-runner")
}

func TestRunModuleDataDirMigrationsNoHooksDoesNotBootstrapMissingDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing-data-dir")
	result, err := RunModuleDataDirMigrations(context.Background(), validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("RunModuleDataDirMigrations no hooks: %v", err)
	}
	if result.DataDir != dir || result.RecoveredTxID != 0 || result.DurableTxID != 0 || len(result.Hooks) != 0 {
		t.Fatalf("no-hook module migration result = %+v, want data dir only", result)
	}
	if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("data dir stat after no-hook module migration = %v, want not exist", statErr)
	}
}

func TestRunModuleDataDirMigrationsSnapshotsRegisteredHooks(t *testing.T) {
	dir := t.TempDir()
	var calls []string
	mod := validChatModule()
	mod.MigrationHook(func(_ context.Context, mc *MigrationContext) error {
		calls = append(calls, "first")
		mod.migrationHooks[1] = func(context.Context, *MigrationContext) error {
			calls = append(calls, "mutated")
			return fmt.Errorf("mutated hook ran during active migration")
		}
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("first-snapshot-hook")})
		return err
	})
	mod.MigrationHook(func(_ context.Context, mc *MigrationContext) error {
		calls = append(calls, "second")
		if mc.CommittedTxID() != 1 {
			return fmt.Errorf("committed tx id = %d, want first hook horizon", mc.CommittedTxID())
		}
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(2), types.NewString("second-snapshot-hook")})
		return err
	})

	result, err := RunModuleDataDirMigrations(context.Background(), mod, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("RunModuleDataDirMigrations snapshot hooks: %v", err)
	}
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("migration hook calls = %#v, want first then second", calls)
	}
	if result.RecoveredTxID != 0 || result.DurableTxID != 2 {
		t.Fatalf("snapshot result tx ids recovered/durable = %d/%d, want 0/2", result.RecoveredTxID, result.DurableTxID)
	}
	requireMigrationHookResults(t, result.Hooks,
		MigrationHookResult{Index: 0, TxID: 1, Changed: true},
		MigrationHookResult{Index: 1, TxID: 2, Changed: true},
	)

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build after snapshot module migration runner: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start after snapshot module migration runner: %v", err)
	}
	defer rt.Close()
	requireMigrationMessageBodies(t, rt, "first-snapshot-hook", "second-snapshot-hook")
}

func TestRunDataDirMigrationsNoopHookDoesNotConsumeTxID(t *testing.T) {
	dir := t.TempDir()
	result, err := RunDataDirMigrations(context.Background(), validChatModule(), Config{DataDir: dir}, func(_ context.Context, mc *MigrationContext) error {
		if mc.Transaction() == nil {
			return fmt.Errorf("migration transaction is nil")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunDataDirMigrations noop: %v", err)
	}
	if result.RecoveredTxID != 0 || result.DurableTxID != 0 {
		t.Fatalf("noop result tx ids recovered/durable = %d/%d, want 0/0", result.RecoveredTxID, result.DurableTxID)
	}
	requireMigrationHookResults(t, result.Hooks, MigrationHookResult{Index: 0})

	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().Reducer("insert_message", insertMessageReducer), Config{DataDir: dir})
	defer rt.Close()
	insertMessage(t, rt, "first")
	if got := rt.state.CommittedTxID(); got != 1 {
		t.Fatalf("first runtime tx id after noop migration = %d, want 1", got)
	}
}

func TestRunDataDirMigrationsNilContextUsesBackground(t *testing.T) {
	dir := t.TempDir()
	//lint:ignore SA1012 This test pins RunDataDirMigrations nil context compatibility.
	result, err := RunDataDirMigrations(nil, validChatModule(), Config{DataDir: dir}, func(_ context.Context, mc *MigrationContext) error {
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("nil-context-hook")})
		return err
	})
	if err != nil {
		t.Fatalf("RunDataDirMigrations nil context: %v", err)
	}
	if result.RecoveredTxID != 0 || result.DurableTxID != 1 {
		t.Fatalf("nil-context result tx ids recovered/durable = %d/%d, want 0/1", result.RecoveredTxID, result.DurableTxID)
	}
	requireMigrationHookResults(t, result.Hooks, MigrationHookResult{Index: 0, TxID: 1, Changed: true})

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build after nil-context migration runner: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start after nil-context migration runner: %v", err)
	}
	defer rt.Close()
	requireMigrationMessageBodies(t, rt, "nil-context-hook")
}

func TestRunDataDirMigrationsNoHooksDoesNotBootstrapMissingDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing-data-dir")
	result, err := RunDataDirMigrations(context.Background(), validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("RunDataDirMigrations no hooks: %v", err)
	}
	if result.DataDir != dir || result.RecoveredTxID != 0 || result.DurableTxID != 0 || len(result.Hooks) != 0 {
		t.Fatalf("no-hook migration result = %+v, want data dir only", result)
	}
	if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("data dir stat after no-hook migration = %v, want not exist", statErr)
	}
}

func TestRunDataDirMigrationsSkipsNilHooksWithoutConsumingIndexOrTxID(t *testing.T) {
	dir := t.TempDir()
	result, err := RunDataDirMigrations(context.Background(), validChatModule(), Config{DataDir: dir},
		func(_ context.Context, mc *MigrationContext) error {
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("first-hook")})
			return err
		},
		nil,
		func(_ context.Context, mc *MigrationContext) error {
			if mc.CommittedTxID() != 1 {
				return fmt.Errorf("committed tx id = %d, want first hook horizon", mc.CommittedTxID())
			}
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(2), types.NewString("third-hook")})
			return err
		},
	)
	if err != nil {
		t.Fatalf("RunDataDirMigrations nil hook: %v", err)
	}
	if result.RecoveredTxID != 0 || result.DurableTxID != 2 {
		t.Fatalf("nil-hook result tx ids recovered/durable = %d/%d, want 0/2", result.RecoveredTxID, result.DurableTxID)
	}
	requireMigrationHookResults(t, result.Hooks,
		MigrationHookResult{Index: 0, TxID: 1, Changed: true},
		MigrationHookResult{Index: 2, TxID: 2, Changed: true},
	)

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build after nil-hook migration runner: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start after nil-hook migration runner: %v", err)
	}
	defer rt.Close()
	requireMigrationMessageBodies(t, rt, "first-hook", "third-hook")
}

func TestRunDataDirMigrationsFailureRollsBackHookTransaction(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("offline boom")
	_, err := RunDataDirMigrations(context.Background(), validChatModule(), Config{DataDir: dir}, func(_ context.Context, mc *MigrationContext) error {
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		if _, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("offline-rolled-back")}); err != nil {
			return err
		}
		return boom
	})
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("RunDataDirMigrations error = %v, want offline boom", err)
	}

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build after failed explicit migration runner: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start after failed explicit migration runner: %v", err)
	}
	defer rt.Close()
	requireMigrationMessageBodies(t, rt)
}

func TestRunDataDirMigrationsCanceledContextBeforeBuildDoesNotRunHooks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing-data-dir")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hookCalls := 0

	_, err := RunDataDirMigrations(ctx, validChatModule(), Config{DataDir: dir}, func(context.Context, *MigrationContext) error {
		hookCalls++
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDataDirMigrations error = %v, want context.Canceled", err)
	}
	if hookCalls != 0 {
		t.Fatalf("hook calls = %d, want 0", hookCalls)
	}
	if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("data dir stat after pre-canceled migration = %v, want not exist", statErr)
	}
}

func TestRunDataDirMigrationsContextCancelAfterHookRollsBackHookTransaction(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	hookCalls := 0
	_, err := RunDataDirMigrations(ctx, validChatModule(), Config{DataDir: dir}, func(_ context.Context, mc *MigrationContext) error {
		hookCalls++
		tableID, _, ok := mc.Schema().TableByName("messages")
		if !ok {
			return fmt.Errorf("messages table missing from migration schema")
		}
		if _, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("offline-canceled")}); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDataDirMigrations error = %v, want context.Canceled", err)
	}
	if hookCalls != 1 {
		t.Fatalf("hook calls = %d, want 1", hookCalls)
	}

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build after canceled explicit migration runner: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start after canceled explicit migration runner: %v", err)
	}
	defer rt.Close()
	requireMigrationMessageBodies(t, rt)
}

func TestRunDataDirMigrationsContextCancelAfterSecondHookPreservesDurableFirstHook(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	result, err := RunDataDirMigrations(ctx, validChatModule(), Config{DataDir: dir},
		func(_ context.Context, mc *MigrationContext) error {
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			_, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(1), types.NewString("offline-first")})
			return err
		},
		func(_ context.Context, mc *MigrationContext) error {
			tableID, _, ok := mc.Schema().TableByName("messages")
			if !ok {
				return fmt.Errorf("messages table missing from migration schema")
			}
			if _, err := mc.Transaction().Insert(tableID, types.ProductValue{types.NewUint64(2), types.NewString("offline-second-canceled")}); err != nil {
				return err
			}
			cancel()
			return nil
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDataDirMigrations error = %v, want context.Canceled", err)
	}
	if result.DataDir != "" || result.RecoveredTxID != 0 || result.DurableTxID != 0 || len(result.Hooks) != 0 {
		t.Fatalf("RunDataDirMigrations result after canceled second hook = %+v, want zero result", result)
	}

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build after canceled second migration hook: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start after canceled second migration hook: %v", err)
	}
	defer rt.Close()
	requireMigrationMessageBodies(t, rt, "offline-first")
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

func requireMigrationHookResults(t *testing.T, got []MigrationHookResult, want ...MigrationHookResult) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("migration hook results = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("migration hook result[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

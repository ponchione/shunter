package shunter_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestRuntimeGauntletStorageFaultSafeZeroTailRecoversAndResumes(t *testing.T) {
	dataDir := t.TempDir()
	runRuntimeCrashChild(t, dataDir, "durable-protocol-commit")
	appendRuntimeGauntletZeroTail(t, dataDir, 1)

	rt := buildGauntletRuntime(t, dataDir)
	model := gauntletModel{players: map[uint64]string{1: "crash_durable"}}
	assertGauntletReadMatchesModel(t, rt, model, "runtime storage safe zero tail restart")
	assertRuntimeCrashCanCommitQueryAndFanout(t, rt, &model, 2, "runtime storage safe zero tail restart")
	if err := rt.Close(); err != nil {
		t.Fatalf("runtime storage safe zero tail Close: %v", err)
	}

	restarted := buildGauntletRuntime(t, dataDir)
	defer restarted.Close()
	assertGauntletReadMatchesModel(t, restarted, model, "runtime storage safe zero tail second restart")
}

func TestRuntimeGauntletStorageFaultDamagedSnapshotFallsBackToCompleteLog(t *testing.T) {
	dataDir := t.TempDir()
	runRuntimeCrashChild(t, dataDir, "durable-protocol-commit")
	corruptRuntimeGauntletSnapshot(t, dataDir, 0)

	rt := buildGauntletRuntime(t, dataDir)
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{1: "crash_durable"}}
	assertGauntletReadMatchesModel(t, rt, model, "runtime storage damaged snapshot fallback")
	assertRuntimeCrashCanCommitQueryAndFanout(t, rt, &model, 2, "runtime storage damaged snapshot fallback")
}

func TestRuntimeGauntletStorageFaultCorruptSegmentFailsLoudly(t *testing.T) {
	dataDir := t.TempDir()
	runRuntimeCrashChild(t, dataDir, "durable-protocol-commit")
	corruptRuntimeGauntletSegmentHeader(t, dataDir, 1)

	rt, err := buildRuntimeStorageFaultGauntletRuntime(dataDir)
	if err == nil {
		_ = rt.Close()
		t.Fatal("runtime storage corrupt segment Build succeeded, want fail-loud recovery error")
	}
	if text := err.Error(); !strings.Contains(text, "state") || !strings.Contains(text, "commitlog") {
		t.Fatalf("runtime storage corrupt segment error = %q, want hosted-runtime state/commitlog context", text)
	}
}

func appendRuntimeGauntletZeroTail(t *testing.T, dataDir string, segmentStart uint64) {
	t.Helper()
	path := filepath.Join(dataDir, commitlog.SegmentFileName(segmentStart))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open segment for zero tail append: %v", err)
	}
	if _, err := f.Write(make([]byte, commitlog.RecordHeaderSize)); err != nil {
		_ = f.Close()
		t.Fatalf("append zero tail: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zero-tail segment: %v", err)
	}
}

func corruptRuntimeGauntletSnapshot(t *testing.T, dataDir string, txID uint64) {
	t.Helper()
	path := runtimeGauntletSnapshotPath(t, dataDir, txID)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open snapshot for corruption: %v", err)
	}
	if _, err := f.WriteAt([]byte{0xff}, 0); err != nil {
		_ = f.Close()
		t.Fatalf("corrupt snapshot: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close corrupted snapshot: %v", err)
	}
}

func runtimeGauntletSnapshotPath(t *testing.T, dataDir string, txID uint64) string {
	t.Helper()
	name := fmt.Sprint(txID)
	for _, path := range []string{
		filepath.Join(dataDir, "snapshots", name, "snapshot"),
		filepath.Join(dataDir, name, "snapshot"),
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	t.Fatalf("snapshot %d not found under %s", txID, dataDir)
	return ""
}

func corruptRuntimeGauntletSegmentHeader(t *testing.T, dataDir string, segmentStart uint64) {
	t.Helper()
	path := filepath.Join(dataDir, commitlog.SegmentFileName(segmentStart))
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open segment for corruption: %v", err)
	}
	if _, err := f.WriteAt([]byte{0x00}, 0); err != nil {
		_ = f.Close()
		t.Fatalf("corrupt segment header: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close corrupted segment: %v", err)
	}
}

func buildRuntimeStorageFaultGauntletRuntime(dataDir string) (*shunter.Runtime, error) {
	mod := shunter.NewModule("gauntlet").
		SchemaVersion(1).
		TableDef(schema.TableDefinition{
			Name: "players",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: types.KindUint64, PrimaryKey: true},
				{Name: "name", Type: types.KindString},
			},
		}, schema.WithPublicRead()).
		Reducer("insert_player", insertPlayerReducer).
		Reducer("insert_next_player", insertNextPlayerReducer).
		Reducer("rename_player", renamePlayerReducer).
		Reducer("delete_player", deletePlayerReducer).
		Reducer("schedule_insert_next_player", scheduleInsertNextPlayerReducer).
		Reducer("schedule_past_due_insert_next_player", schedulePastDueInsertNextPlayerReducer).
		Reducer("schedule_insert_next_player_then_fail", scheduleInsertNextPlayerThenFailReducer).
		Reducer("schedule_fail_after_insert", scheduleFailAfterInsertReducer).
		Reducer("schedule_panic_after_insert", schedulePanicAfterInsertReducer).
		Reducer("schedule_repeat_insert_next_player", scheduleRepeatInsertNextPlayerReducer).
		Reducer("cancel_schedule", cancelScheduleReducer).
		Reducer("fail_after_insert", failAfterInsertReducer).
		Reducer("panic_after_insert", panicAfterInsertReducer)

	rt, err := shunter.Build(mod, shunter.Config{DataDir: dataDir})
	if err != nil {
		return nil, err
	}
	if err := rt.Start(context.Background()); err != nil {
		_ = rt.Close()
		return nil, err
	}
	return rt, nil
}

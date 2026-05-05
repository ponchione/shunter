package commitlog

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestSelectSnapshotNullableColumnMismatchIsSchemaMismatch(t *testing.T) {
	root := t.TempDir()
	reg := buildSelectionRegistry(t, selectionRegistryConfig{})
	cs := buildSelectionCommittedState(t, reg)
	nullableSnapshotReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		players := tables[0]
		players.Columns[1].Nullable = true
		tables[0] = players
	})
	writeSelectionSnapshot(t, root, nullableSnapshotReg, cs, 5)

	_, err := SelectSnapshot(root, 5, reg)
	var mismatch *SchemaMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SchemaMismatchError, got %v", err)
	}
}

func TestSelectSnapshotAcceptsNullableSnapshotWhenRegistryMatches(t *testing.T) {
	root := t.TempDir()
	reg := buildSelectionRegistry(t, selectionRegistryConfig{})
	cs := buildSelectionCommittedState(t, reg)
	nullableReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		players := tables[0]
		players.Columns[1].Nullable = true
		tables[0] = players
	})
	writeSelectionSnapshot(t, root, nullableReg, cs, 5)

	selected, err := SelectSnapshot(root, 5, nullableReg)
	if err != nil {
		t.Fatalf("SelectSnapshot nullable match: %v", err)
	}
	if selected == nil || selected.TxID != 5 {
		t.Fatalf("selected snapshot = %+v, want tx 5", selected)
	}
}

func TestNewDurabilityWorkerAcceptsExportedFsyncModes(t *testing.T) {
	for _, mode := range []FsyncMode{FsyncBatch, FsyncPerTx} {
		t.Run(fmt.Sprintf("mode_%d", mode), func(t *testing.T) {
			dir := t.TempDir()
			opts := DefaultCommitLogOptions()
			opts.FsyncMode = mode

			dw, err := NewDurabilityWorker(dir, 1, opts)
			if err != nil {
				t.Fatalf("NewDurabilityWorker rejected exported mode %d: %v", mode, err)
			}
			if _, err := dw.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
		})
	}
}

func TestNewDurabilityWorkerRejectsUnknownFsyncMode(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.FsyncMode = FsyncMode(99)

	_, err := NewDurabilityWorker(dir, 1, opts)
	if !errors.Is(err, ErrUnknownFsyncMode) {
		t.Fatalf("expected ErrUnknownFsyncMode, got %v", err)
	}
}

func TestNewDurabilityWorkerWithResumePlanRejectsUnknownFsyncMode(t *testing.T) {
	dir := t.TempDir()
	plan := RecoveryResumePlan{SegmentStartTx: 1, NextTxID: 1, AppendMode: AppendInPlace}
	opts := DefaultCommitLogOptions()
	opts.FsyncMode = FsyncMode(99)

	_, err := NewDurabilityWorkerWithResumePlan(dir, plan, opts)
	if !errors.Is(err, ErrUnknownFsyncMode) {
		t.Fatalf("expected ErrUnknownFsyncMode, got %v", err)
	}
}

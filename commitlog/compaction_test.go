package commitlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestSegmentCoverageBuildsRangesFromSegmentInfo(t *testing.T) {
	segments := []SegmentInfo{
		{Path: "00000000000000000001.log", StartTx: 1, LastTx: 4},
		{Path: "00000000000000000005.log", StartTx: 5, LastTx: 7},
		{Path: "00000000000000000008.log", StartTx: 8, LastTx: 10},
	}

	got := SegmentCoverage(segments)
	want := []SegmentRange{
		{Path: "00000000000000000001.log", MinTxID: 1, MaxTxID: 4, Active: false},
		{Path: "00000000000000000005.log", MinTxID: 5, MaxTxID: 7, Active: false},
		{Path: "00000000000000000008.log", MinTxID: 8, MaxTxID: 10, Active: true},
	}

	assertSegmentRangesEqual(t, got, want)
}

func TestSegmentCoverageHandlesSingleRecordAndEmptySegments(t *testing.T) {
	segments := []SegmentInfo{
		{Path: "00000000000000000011.log", StartTx: 11, LastTx: 11},
		{Path: "00000000000000000012.log", StartTx: 12, LastTx: 11},
	}

	got := SegmentCoverage(segments)
	want := []SegmentRange{
		{Path: "00000000000000000011.log", MinTxID: 11, MaxTxID: 11, Active: false},
		{Path: "00000000000000000012.log", MinTxID: 12, MaxTxID: 11, Active: true},
	}

	assertSegmentRangesEqual(t, got, want)
}

func TestCompactDeletesFullyCoveredSegmentsOnly(t *testing.T) {
	deleted, retained := Compact([]SegmentRange{
		{Path: "seg-1", MinTxID: 1, MaxTxID: 900},
		{Path: "seg-2", MinTxID: 900, MaxTxID: 1100},
		{Path: "seg-3", MinTxID: 1001, MaxTxID: 1500},
		{Path: "seg-4", MinTxID: 1501, MaxTxID: 1700, Active: true},
	}, 1000)

	assertStringSlicesEqual(t, deleted, []string{"seg-1"})
	assertStringSlicesEqual(t, retained, []string{"seg-2", "seg-3", "seg-4"})
}

func TestCompactKeepsEverythingWithoutSnapshotAndDeletesMultipleCoveredSegments(t *testing.T) {
	segments := []SegmentRange{
		{Path: "seg-1", MinTxID: 1, MaxTxID: 4},
		{Path: "seg-2", MinTxID: 5, MaxTxID: 8},
		{Path: "seg-3", MinTxID: 9, MaxTxID: 12, Active: true},
	}

	deleted, retained := Compact(segments, 0)
	assertStringSlicesEqual(t, deleted, nil)
	assertStringSlicesEqual(t, retained, []string{"seg-1", "seg-2", "seg-3"})

	deleted, retained = Compact(segments, 8)
	assertStringSlicesEqual(t, deleted, []string{"seg-1", "seg-2"})
	assertStringSlicesEqual(t, retained, []string{"seg-3"})
}

func FuzzCompactPlanner(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8},
		{0xff, 0, 0x7f, 0x80, 0x40, 0x20},
		[]byte("compaction-boundary"),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			return
		}
		r := newFuzzByteReader(data)
		snapshotTxID := r.txID(64)
		segments := r.segmentRanges(16)
		label := compactFuzzLabel(data, snapshotTxID, segments)

		deleted, retained := Compact(segments, snapshotTxID)
		deletedAgain, retainedAgain := Compact(segments, snapshotTxID)
		assertStringSlicesEqual(t, deletedAgain, deleted)
		assertStringSlicesEqual(t, retainedAgain, retained)

		seen := make(map[string]string, len(segments))
		for _, path := range deleted {
			if _, ok := seen[path]; ok {
				t.Fatalf("duplicate compact output path %q in deleted: deleted=%v retained=%v %s", path, deleted, retained, label)
			}
			seen[path] = "deleted"
		}
		for _, path := range retained {
			if _, ok := seen[path]; ok {
				t.Fatalf("duplicate compact output path %q in retained: deleted=%v retained=%v %s", path, deleted, retained, label)
			}
			seen[path] = "retained"
		}
		if len(seen) != len(segments) {
			t.Fatalf("compact output cardinality = %d, want %d: deleted=%v retained=%v %s", len(seen), len(segments), deleted, retained, label)
		}

		for _, seg := range segments {
			decision, ok := seen[seg.Path]
			if !ok {
				t.Fatalf("missing compact output for %q: deleted=%v retained=%v %s", seg.Path, deleted, retained, label)
			}
			wantDeleted := snapshotTxID != 0 && !seg.Active && seg.MaxTxID <= snapshotTxID
			if wantDeleted && decision != "deleted" {
				t.Fatalf("covered sealed segment retained: seg=%+v snapshot=%d deleted=%v retained=%v %s", seg, snapshotTxID, deleted, retained, label)
			}
			if !wantDeleted && decision != "retained" {
				t.Fatalf("uncovered/active segment deleted: seg=%+v snapshot=%d deleted=%v retained=%v %s", seg, snapshotTxID, deleted, retained, label)
			}
		}
		if snapshotTxID == 0 && len(deleted) != 0 {
			t.Fatalf("snapshot=0 deleted segments: deleted=%v retained=%v %s", deleted, retained, label)
		}
	})
}

func TestRunCompactionDeletesCoveredSegmentsAndFsyncsDirectory(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5, 6)
	seg3 := makeScanTestSegment(t, dir, 7, 7, 8)

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return nil
	})

	if err := RunCompaction(dir, 6); err != nil {
		t.Fatalf("RunCompaction() error = %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, seg1)
	assertFileMissing(t, seg2)
	assertFileExists(t, seg3)
}

// Pin 21.
func TestCompactionRemovesSidecarIndex(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5, 6)
	seg3 := makeScanTestSegment(t, dir, 7, 7, 8)

	idx1Path := filepath.Join(dir, OffsetIndexFileName(1))
	idx2Path := filepath.Join(dir, OffsetIndexFileName(4))
	idx3Path := filepath.Join(dir, OffsetIndexFileName(7))
	for _, p := range []string{idx1Path, idx2Path, idx3Path} {
		idx, err := CreateOffsetIndex(p, 4)
		if err != nil {
			t.Fatalf("CreateOffsetIndex(%s): %v", p, err)
		}
		_ = idx.Close()
	}

	ignoreCompactionSyncDir(t)

	if err := RunCompaction(dir, 6); err != nil {
		t.Fatalf("RunCompaction: %v", err)
	}
	assertFileMissing(t, seg1)
	assertFileMissing(t, seg2)
	assertFileExists(t, seg3)
	assertFileMissing(t, idx1Path)
	assertFileMissing(t, idx2Path)
	assertFileExists(t, idx3Path)
}

func TestRunCompactionRemovesCoveredSidecarSymlinkWithoutTouchingTarget(t *testing.T) {
	dir := t.TempDir()
	covered := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	tail := makeScanTestSegment(t, dir, 4, 4, 5)
	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "external.idx")
	before := []byte("external sidecar target")
	if err := os.WriteFile(targetPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, targetPath, idxPath)

	ignoreCompactionSyncDir(t)

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction: %v", err)
	}
	assertFileMissing(t, covered)
	assertFileMissing(t, idxPath)
	assertFileExists(t, tail)
	after, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("sidecar symlink target changed: got %q want %q", after, before)
	}
}

// TestCompactionToleratesMissingSidecar covers the backwards-compat path:
// segments compacted on old deployments have no paired .idx file. Compaction
// must treat os.IsNotExist as non-fatal.
func TestCompactionToleratesMissingSidecar(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5, 6)

	ignoreCompactionSyncDir(t)

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction with no sidecars: %v", err)
	}
	assertFileMissing(t, seg1)
}

func TestRunCompactionWithoutSnapshotRetainsLogAndOffsetArtifacts(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5)

	idx1Path := filepath.Join(dir, OffsetIndexFileName(1))
	orphanIdxPath := filepath.Join(dir, OffsetIndexFileName(2))
	idx4Path := filepath.Join(dir, OffsetIndexFileName(4))
	for _, path := range []string{idx1Path, orphanIdxPath, idx4Path} {
		idx, err := CreateOffsetIndex(path, 4)
		if err != nil {
			t.Fatalf("CreateOffsetIndex(%s): %v", path, err)
		}
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		return nil
	})

	if err := RunCompaction(dir, 0); err != nil {
		t.Fatalf("RunCompaction without snapshot: %v", err)
	}
	if syncCalls != 0 {
		t.Fatalf("syncDir calls = %d, want 0 without snapshot cleanup", syncCalls)
	}
	assertFileExists(t, seg1)
	assertFileExists(t, seg2)
	assertFileExists(t, idx1Path)
	assertFileExists(t, orphanIdxPath)
	assertFileExists(t, idx4Path)
}

func TestRunCompactionRemovesOrphanedCoveredSidecarOnRetry(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5, 6)

	idx1Path := filepath.Join(dir, OffsetIndexFileName(1))
	idx2Path := filepath.Join(dir, OffsetIndexFileName(4))
	for _, p := range []string{idx1Path, idx2Path} {
		idx, err := CreateOffsetIndex(p, 4)
		if err != nil {
			t.Fatalf("CreateOffsetIndex(%s): %v", p, err)
		}
		_ = idx.Close()
	}

	// Simulate a crash after the covered segment was deleted but before its
	// sidecar index was cleaned up.
	if err := os.Remove(seg1); err != nil {
		t.Fatal(err)
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return nil
	})

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, idx1Path)
	assertFileExists(t, seg2)
	assertFileExists(t, idx2Path)
}

func TestRunCompactionRemovesOrphanedCoveredSidecarSymlinkOnRetry(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5, 6)
	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "external.idx")
	before := []byte("external orphan sidecar target")
	if err := os.WriteFile(targetPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, targetPath, idxPath)
	if err := os.Remove(seg1); err != nil {
		t.Fatal(err)
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return nil
	})

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, idxPath)
	assertFileExists(t, seg2)
	after, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("orphan sidecar symlink target changed: got %q want %q", after, before)
	}
}

func TestRunCompactionRemovesOrphanedCoveredSidecarDirectoryOnRetry(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5, 6)

	idx1Path := filepath.Join(dir, OffsetIndexFileName(1))
	if err := os.Mkdir(idx1Path, 0o755); err != nil {
		t.Fatal(err)
	}
	idx2Path := filepath.Join(dir, OffsetIndexFileName(4))
	idx, err := CreateOffsetIndex(idx2Path, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash after the covered segment was deleted but before an
	// accidentally directory-shaped sidecar artifact was cleaned up.
	if err := os.Remove(seg1); err != nil {
		t.Fatal(err)
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return nil
	})

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, idx1Path)
	assertFileExists(t, seg2)
	assertFileExists(t, idx2Path)
}

func TestRunCompactionRemovesCorruptOrphanedCoveredSidecarOnRetry(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5, 6)

	idx1Path := filepath.Join(dir, OffsetIndexFileName(1))
	if err := os.WriteFile(idx1Path, []byte("not an offset index"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx2Path := filepath.Join(dir, OffsetIndexFileName(4))
	idx, err := CreateOffsetIndex(idx2Path, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(seg1); err != nil {
		t.Fatal(err)
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return nil
	})

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, idx1Path)
	assertFileExists(t, seg2)
	assertFileExists(t, idx2Path)
}

func TestRunCompactionRemovesCoveredOrphansButRetainsUncoveredOrphans(t *testing.T) {
	dir := t.TempDir()
	covered := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	active := makeScanTestSegment(t, dir, 4, 4, 5)

	coveredIdx := filepath.Join(dir, OffsetIndexFileName(1))
	coveredOrphan := filepath.Join(dir, OffsetIndexFileName(2))
	activeIdx := filepath.Join(dir, OffsetIndexFileName(4))
	futureOrphan := filepath.Join(dir, OffsetIndexFileName(6))
	for _, p := range []string{coveredIdx, coveredOrphan, activeIdx, futureOrphan} {
		idx, err := CreateOffsetIndex(p, 4)
		if err != nil {
			t.Fatalf("CreateOffsetIndex(%s): %v", p, err)
		}
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}

	ignoreCompactionSyncDir(t)

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction: %v", err)
	}
	assertFileMissing(t, covered)
	assertFileMissing(t, coveredIdx)
	assertFileMissing(t, coveredOrphan)
	assertFileExists(t, active)
	assertFileExists(t, activeIdx)
	assertFileExists(t, futureOrphan)
}

func TestRunCompactionRemovesCoveredSymlinkOrphanButRetainsFutureSymlinkOrphan(t *testing.T) {
	dir := t.TempDir()
	covered := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	active := makeScanTestSegment(t, dir, 4, 4, 5)
	targetDir := t.TempDir()
	coveredTarget := filepath.Join(targetDir, "covered.idx")
	futureTarget := filepath.Join(targetDir, "future.idx")
	coveredBefore := []byte("covered symlink target")
	futureBefore := []byte("future symlink target")
	if err := os.WriteFile(coveredTarget, coveredBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(futureTarget, futureBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	coveredOrphan := filepath.Join(dir, OffsetIndexFileName(2))
	futureOrphan := filepath.Join(dir, OffsetIndexFileName(6))
	symlinkOrSkip(t, coveredTarget, coveredOrphan)
	symlinkOrSkip(t, futureTarget, futureOrphan)

	ignoreCompactionSyncDir(t)

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction: %v", err)
	}
	assertFileMissing(t, covered)
	assertFileMissing(t, coveredOrphan)
	assertFileExists(t, active)
	assertSymlinkExists(t, futureOrphan)
	assertFileBytes(t, coveredTarget, coveredBefore)
	assertFileBytes(t, futureTarget, futureBefore)
}

func TestRunCompactionRemovesCoveredOrphansAfterEntirePrefixGone(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5, 6)
	active := makeScanTestSegment(t, dir, 7, 7, 8)

	idx1Path := filepath.Join(dir, OffsetIndexFileName(1))
	idx4Path := filepath.Join(dir, OffsetIndexFileName(4))
	idx7Path := filepath.Join(dir, OffsetIndexFileName(7))
	for _, p := range []string{idx1Path, idx4Path, idx7Path} {
		idx, err := CreateOffsetIndex(p, 4)
		if err != nil {
			t.Fatalf("CreateOffsetIndex(%s): %v", p, err)
		}
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate a crash after the covered prefix segments were removed but
	// before either covered sidecar was cleaned.
	if err := os.Remove(seg1); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(seg2); err != nil {
		t.Fatal(err)
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	})

	if err := RunCompaction(dir, 6); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, idx1Path)
	assertFileMissing(t, idx4Path)
	assertFileExists(t, active)
	assertFileExists(t, idx7Path)
}

func TestRunCompactionRemovesCoveredOrphansWhenNoLogSegmentsRemain(t *testing.T) {
	dir := t.TempDir()
	coveredIdx1 := filepath.Join(dir, OffsetIndexFileName(1))
	coveredIdx4 := filepath.Join(dir, OffsetIndexFileName(4))
	futureIdx := filepath.Join(dir, OffsetIndexFileName(7))
	for _, p := range []string{coveredIdx1, coveredIdx4, futureIdx} {
		idx, err := CreateOffsetIndex(p, 4)
		if err != nil {
			t.Fatalf("CreateOffsetIndex(%s): %v", p, err)
		}
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	})

	if err := RunCompaction(dir, 6); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, coveredIdx1)
	assertFileMissing(t, coveredIdx4)
	assertFileExists(t, futureIdx)
}

func TestRunCompactionSegmentRemovalFailureIncludesOperationPathAndWraps(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5)
	removeErr := errors.New("remove segment failed")

	stubCompactionRemoveFile(t, func(original func(string) error, path string) error {
		if path == seg1 {
			return removeErr
		}
		return original(path)
	})

	err := RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, removeErr, "remove covered segment", seg1)
	assertFileExists(t, seg1)
}

func TestRunCompactionSidecarRemovalFailureIncludesOperationPathAndWraps(t *testing.T) {
	dir := t.TempDir()
	makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5)
	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	idx, err := CreateOffsetIndex(idxPath, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	removeErr := errors.New("remove sidecar failed")

	stubCompactionRemoveFile(t, func(original func(string) error, path string) error {
		if path == idxPath {
			return removeErr
		}
		return original(path)
	})

	err = RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, removeErr, "remove covered offset index", idxPath)
	assertFileExists(t, idxPath)
}

func TestRunCompactionOrphanSidecarRemovalFailureIncludesOperationPathAndWraps(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5)
	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	idx, err := CreateOffsetIndex(idxPath, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(seg1); err != nil {
		t.Fatal(err)
	}
	removeErr := errors.New("remove orphan sidecar failed")

	stubCompactionRemoveFile(t, func(original func(string) error, path string) error {
		if path == idxPath {
			return removeErr
		}
		return original(path)
	})

	err = RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, removeErr, "remove orphaned offset index", idxPath)
	assertFileExists(t, idxPath)
}

func TestRunCompactionSyncFailureIncludesOperationPathAndWraps(t *testing.T) {
	dir := t.TempDir()
	makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5)
	syncErr := errors.New("sync failed")

	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return syncErr
	})

	err := RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, syncErr, "sync directory", dir)
}

func TestRunCompactionRetriesDirectorySyncAfterPriorSyncFailure(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5)
	syncErr := errors.New("sync failed")

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		if syncCalls == 1 {
			return syncErr
		}
		return nil
	})

	err := RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, syncErr, "sync directory", dir)
	assertFileMissing(t, seg1)
	assertFileExists(t, seg2)

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 2 {
		t.Fatalf("syncDir calls = %d, want 2", syncCalls)
	}
}

func TestRunCompactionRetriesDirectorySyncAfterOrphanSidecarCleanup(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5)
	idx1Path := filepath.Join(dir, OffsetIndexFileName(1))
	idx2Path := filepath.Join(dir, OffsetIndexFileName(4))
	for _, path := range []string{idx1Path, idx2Path} {
		idx, err := CreateOffsetIndex(path, 4)
		if err != nil {
			t.Fatal(err)
		}
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(seg1); err != nil {
		t.Fatal(err)
	}
	syncErr := errors.New("sync failed")

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		if syncCalls == 1 {
			return syncErr
		}
		return nil
	})

	err := RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, syncErr, "sync directory", dir)
	assertFileMissing(t, idx1Path)
	assertFileExists(t, seg2)
	assertFileExists(t, idx2Path)

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 2 {
		t.Fatalf("syncDir calls = %d, want 2", syncCalls)
	}
}

func TestRunCompactionRetriesDirectorySyncAfterOnlyOrphanSidecarCleanup(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	idx, err := CreateOffsetIndex(idxPath, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	syncErr := errors.New("sync failed")

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		if syncCalls == 1 {
			return syncErr
		}
		return nil
	})

	err = RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, syncErr, "sync directory", dir)
	assertFileMissing(t, idxPath)

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction retry: %v", err)
	}
	if syncCalls != 2 {
		t.Fatalf("syncDir calls = %d, want 2", syncCalls)
	}
}

func TestRunCompactionRejectsSnapshotBeyondDurableHorizon(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5)

	err := RunCompaction(dir, 99)
	if err == nil {
		t.Fatal("expected durable horizon rejection")
	}
	text := err.Error()
	if !strings.Contains(text, "beyond durable log horizon") || !strings.Contains(text, "99") || !strings.Contains(text, "5") {
		t.Fatalf("error %q should include snapshot tx and durable horizon", text)
	}
	assertFileExists(t, seg1)
}

func TestRunCompactionRejectsCorruptCoveredSealedSegmentBeforeDeleting(t *testing.T) {
	dir := t.TempDir()
	corruptCovered := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	tail := makeScanTestSegment(t, dir, 4, 4, 5)
	corruptScanTestRecordPayloadByte(t, corruptCovered, 0, 0)

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		return nil
	})

	err := RunCompaction(dir, 5)
	if err == nil {
		t.Fatal("expected corrupt covered sealed segment to abort compaction")
	}
	var checksumErr *ChecksumMismatchError
	if !errors.As(err, &checksumErr) {
		t.Fatalf("RunCompaction error = %T %v, want ChecksumMismatchError", err, err)
	}
	if syncCalls != 0 {
		t.Fatalf("syncDir calls = %d, want 0 after scan failure", syncCalls)
	}
	assertFileExists(t, corruptCovered)
	assertFileExists(t, tail)
}

func TestRunCompactionRejectsDamagedCoveredSealedTailBeforeDeleting(t *testing.T) {
	dir := t.TempDir()
	damagedCovered := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	tail := makeScanTestSegment(t, dir, 4, 4, 5)
	corruptScanTestRecordPayloadByte(t, damagedCovered, 2, 0)

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		return nil
	})

	err := RunCompaction(dir, 5)
	assertHistoryGap(t, err, 3, 4)
	if syncCalls != 0 {
		t.Fatalf("syncDir calls = %d, want 0 after scan failure", syncCalls)
	}
	assertFileExists(t, damagedCovered)
	assertFileExists(t, tail)
}

func TestRunCompactionRejectsSymlinkSegmentBeforeDeletingCoveredSegments(t *testing.T) {
	dir := t.TempDir()
	covered := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	targetDir := t.TempDir()
	targetPath := makeScanTestSegment(t, targetDir, 4, 4, 5)
	symlinkSegment := filepath.Join(dir, SegmentFileName(4))
	symlinkOrSkip(t, targetPath, symlinkSegment)

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		syncCalls++
		return nil
	})

	err := RunCompaction(dir, 3)
	if err == nil {
		t.Fatal("expected symlink segment to abort compaction")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("RunCompaction error = %v, want ErrOpen category", err)
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("RunCompaction error = %v, want regular-file rejection detail", err)
	}
	if syncCalls != 0 {
		t.Fatalf("syncDir calls = %d, want 0 after scan failure", syncCalls)
	}
	assertFileExists(t, covered)
	assertSymlinkExists(t, symlinkSegment)
	assertFileExists(t, targetPath)
}

func TestRunCompactionDoesNotDeleteBoundarySegment(t *testing.T) {
	dir := t.TempDir()
	boundary := makeScanTestSegment(t, dir, 900, contiguousTxs(900, 1100)...)
	active := makeScanTestSegment(t, dir, 1101, 1101, 1102)

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	})

	if err := RunCompaction(dir, 1000); err != nil {
		t.Fatalf("RunCompaction() error = %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1 conservative retry sync", syncCalls)
	}
	assertFileExists(t, boundary)
	assertFileExists(t, active)
}

func TestRunCompactionRetainsEmptyDamagedActiveTail(t *testing.T) {
	dir := t.TempDir()
	covered := makeScanTestSegment(t, dir, 1, 1, 2)
	active := makeScanTestSegment(t, dir, 3, 3)
	truncateScanTestFileToOffset(t, active, int64(SegmentHeaderSize+RecordHeaderSize-1))

	coveredIdx := filepath.Join(dir, OffsetIndexFileName(1))
	activeIdx := filepath.Join(dir, OffsetIndexFileName(3))
	for _, path := range []string{coveredIdx, activeIdx} {
		idx, err := CreateOffsetIndex(path, 4)
		if err != nil {
			t.Fatal(err)
		}
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	})

	if err := RunCompaction(dir, 2); err != nil {
		t.Fatalf("RunCompaction() error = %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, covered)
	assertFileMissing(t, coveredIdx)
	assertFileExists(t, active)
	assertFileExists(t, activeIdx)
}

func TestRunCompactionRetainsZeroLengthActiveTail(t *testing.T) {
	dir := t.TempDir()
	covered := makeScanTestSegment(t, dir, 1, 1, 2)
	active := createZeroLengthSegment(t, dir, 3)

	coveredIdx := filepath.Join(dir, OffsetIndexFileName(1))
	activeIdx := filepath.Join(dir, OffsetIndexFileName(3))
	for _, path := range []string{coveredIdx, activeIdx} {
		idx, err := CreateOffsetIndex(path, 4)
		if err != nil {
			t.Fatal(err)
		}
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}

	syncCalls := 0
	stubCompactionSyncDir(t, func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	})

	if err := RunCompaction(dir, 2); err != nil {
		t.Fatalf("RunCompaction() error = %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("syncDir calls = %d, want 1", syncCalls)
	}
	assertFileMissing(t, covered)
	assertFileMissing(t, coveredIdx)
	assertFileExists(t, active)
	assertFileExists(t, activeIdx)
}

func TestRunCompactionRejectsSnapshotBeyondZeroLengthActiveTailHorizon(t *testing.T) {
	dir := t.TempDir()
	covered := makeScanTestSegment(t, dir, 1, 1, 2)
	active := createZeroLengthSegment(t, dir, 3)

	err := RunCompaction(dir, 3)
	if err == nil {
		t.Fatal("expected durable horizon rejection")
	}
	text := err.Error()
	if !strings.Contains(text, "beyond durable log horizon") || !strings.Contains(text, "3") || !strings.Contains(text, "2") {
		t.Fatalf("error %q should include snapshot tx and durable horizon", text)
	}
	assertFileExists(t, covered)
	assertFileExists(t, active)
}

func TestRunCompactionMalformedSegmentFilenameFailsBeforeDeletingCoveredSegments(t *testing.T) {
	for _, tc := range []struct {
		name       string
		fileName   string
		wantDetail string
	}{
		{
			name:       "invalid",
			fileName:   "not-a-segment.log",
			wantDetail: `invalid segment filename "not-a-segment.log"`,
		},
		{
			name:       "non-canonical",
			fileName:   "1.log",
			wantDetail: `non-canonical segment filename "1.log"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			covered := makeScanTestSegment(t, dir, 1, 1, 2)
			active := makeScanTestSegment(t, dir, 3, 3, 4)
			malformed := filepath.Join(dir, tc.fileName)
			if err := os.WriteFile(malformed, []byte("not used"), 0o644); err != nil {
				t.Fatal(err)
			}

			err := RunCompaction(dir, 2)
			if err == nil {
				t.Fatal("expected malformed segment filename to abort compaction")
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("RunCompaction error = %v, want detail %q", err, tc.wantDetail)
			}
			assertFileExists(t, covered)
			assertFileExists(t, active)
			assertFileExists(t, malformed)
		})
	}
}

func assertCompactionFailureContext(t *testing.T, err, cause error, operation, path string) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error %v should wrap %v", err, cause)
	}
	text := err.Error()
	if !strings.Contains(text, operation) || !strings.Contains(text, path) {
		t.Fatalf("error %q should include operation %q and path %q", text, operation, path)
	}
}

func stubCompactionSyncDir(t *testing.T, hook func(string) error) {
	t.Helper()
	original := syncDir
	syncDir = hook
	t.Cleanup(func() { syncDir = original })
}

func ignoreCompactionSyncDir(t *testing.T) {
	t.Helper()
	stubCompactionSyncDir(t, func(string) error { return nil })
}

func stubCompactionRemoveFile(t *testing.T, hook func(original func(string) error, path string) error) {
	t.Helper()
	original := removeFile
	removeFile = func(path string) error {
		return hook(original, path)
	}
	t.Cleanup(func() { removeFile = original })
}

func assertSegmentRangesEqual(t *testing.T, got, want []SegmentRange) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("range count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].MinTxID != want[i].MinTxID || got[i].MaxTxID != want[i].MaxTxID || got[i].Active != want[i].Active {
			t.Fatalf("range[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item[%d] = %q, want %q; got=%v want=%v", i, got[i], want[i], got, want)
		}
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err=%v", filepath.Base(path), err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", filepath.Base(path), err)
	}
}

func assertSymlinkExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("expected %s symlink to exist: %v", filepath.Base(path), err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink, mode=%s", filepath.Base(path), info.Mode())
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s bytes = %q, want %q", filepath.Base(path), got, want)
	}
}

func contiguousTxs(start, end uint64) []uint64 {
	txs := make([]uint64, 0, end-start+1)
	for tx := start; tx <= end; tx++ {
		txs = append(txs, tx)
	}
	return txs
}

func (r *fuzzByteReader) segmentRanges(maxSegments int) []SegmentRange {
	n := int(r.byte() % byte(maxSegments+1))
	out := make([]SegmentRange, n)
	for i := range out {
		start := r.txID(64)
		width := r.txID(16)
		maxTxID := start + width
		if r.byte()%7 == 0 && start > 0 {
			maxTxID = start - 1
		}
		out[i] = SegmentRange{
			Path:    "fuzz-seg-" + string(rune('a'+i)),
			MinTxID: start,
			MaxTxID: maxTxID,
			Active:  r.byte()%5 == 0,
		}
	}
	return out
}

func compactFuzzLabel(data []byte, snapshotTxID types.TxID, segments []SegmentRange) string {
	if len(data) <= 80 {
		return fmt.Sprintf("seed_len=%d seed=%s snapshot=%d segments=%s", len(data), fmtBytes(data), snapshotTxID, fmtSegments(segments))
	}
	return fmt.Sprintf("seed_len=%d seed_prefix=%s snapshot=%d segments=%s", len(data), fmtBytes(data[:80]), snapshotTxID, fmtSegments(segments))
}

func fmtBytes(data []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, len(data)*2)
	for _, b := range data {
		out = append(out, hex[b>>4], hex[b&0x0f])
	}
	return string(out)
}

func fmtSegments(segments []SegmentRange) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, seg := range segments {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(seg.Path)
		b.WriteByte(':')
		b.WriteString(fmt.Sprintf("%d", seg.MinTxID))
		b.WriteString("..")
		b.WriteString(fmt.Sprintf("%d", seg.MaxTxID))
		if seg.Active {
			b.WriteString(":active")
		}
	}
	b.WriteByte(']')
	return b.String()
}

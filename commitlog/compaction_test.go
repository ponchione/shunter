package commitlog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRunCompactionDeletesCoveredSegmentsAndFsyncsDirectory(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5, 6)
	seg3 := makeScanTestSegment(t, dir, 7, 7, 8)

	originalSyncDir := syncDir
	syncCalls := 0
	syncDir = func(path string) error {
		syncCalls++
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

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

	originalSyncDir := syncDir
	syncDir = func(string) error { return nil }
	defer func() { syncDir = originalSyncDir }()

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

// TestCompactionToleratesMissingSidecar covers the backwards-compat path:
// segments compacted on old deployments have no paired .idx file. Compaction
// must treat os.IsNotExist as non-fatal.
func TestCompactionToleratesMissingSidecar(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5, 6)

	originalSyncDir := syncDir
	syncDir = func(string) error { return nil }
	defer func() { syncDir = originalSyncDir }()

	if err := RunCompaction(dir, 3); err != nil {
		t.Fatalf("RunCompaction with no sidecars: %v", err)
	}
	assertFileMissing(t, seg1)
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

	originalSyncDir := syncDir
	syncCalls := 0
	syncDir = func(path string) error {
		syncCalls++
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

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

	originalSyncDir := syncDir
	syncDir = func(string) error { return nil }
	defer func() { syncDir = originalSyncDir }()

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

	originalSyncDir := syncDir
	syncCalls := 0
	syncDir = func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

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

func TestRunCompactionSegmentRemovalFailureIncludesOperationPathAndWraps(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5)
	removeErr := errors.New("remove segment failed")

	originalRemoveFile := removeFile
	removeFile = func(path string) error {
		if path == seg1 {
			return removeErr
		}
		return originalRemoveFile(path)
	}
	defer func() { removeFile = originalRemoveFile }()

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

	originalRemoveFile := removeFile
	removeFile = func(path string) error {
		if path == idxPath {
			return removeErr
		}
		return originalRemoveFile(path)
	}
	defer func() { removeFile = originalRemoveFile }()

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

	originalRemoveFile := removeFile
	removeFile = func(path string) error {
		if path == idxPath {
			return removeErr
		}
		return originalRemoveFile(path)
	}
	defer func() { removeFile = originalRemoveFile }()

	err = RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, removeErr, "remove orphaned offset index", idxPath)
	assertFileExists(t, idxPath)
}

func TestRunCompactionSyncFailureIncludesOperationPathAndWraps(t *testing.T) {
	dir := t.TempDir()
	makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 4, 4, 5)
	syncErr := errors.New("sync failed")

	originalSyncDir := syncDir
	syncDir = func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		return syncErr
	}
	defer func() { syncDir = originalSyncDir }()

	err := RunCompaction(dir, 3)
	assertCompactionFailureContext(t, err, syncErr, "sync directory", dir)
}

func TestRunCompactionRetriesDirectorySyncAfterPriorSyncFailure(t *testing.T) {
	dir := t.TempDir()
	seg1 := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	seg2 := makeScanTestSegment(t, dir, 4, 4, 5)
	syncErr := errors.New("sync failed")

	originalSyncDir := syncDir
	syncCalls := 0
	syncDir = func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		if syncCalls == 1 {
			return syncErr
		}
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

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

	originalSyncDir := syncDir
	syncCalls := 0
	syncDir = func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		if syncCalls == 1 {
			return syncErr
		}
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

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

func TestRunCompactionDoesNotDeleteBoundarySegment(t *testing.T) {
	dir := t.TempDir()
	boundary := makeScanTestSegment(t, dir, 900, contiguousTxs(900, 1100)...)
	active := makeScanTestSegment(t, dir, 1101, 1101, 1102)

	originalSyncDir := syncDir
	syncCalls := 0
	syncDir = func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

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

	originalSyncDir := syncDir
	syncCalls := 0
	syncDir = func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

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

	originalSyncDir := syncDir
	syncCalls := 0
	syncDir = func(path string) error {
		if path != dir {
			t.Fatalf("syncDir path = %q, want %q", path, dir)
		}
		syncCalls++
		return nil
	}
	defer func() { syncDir = originalSyncDir }()

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

func contiguousTxs(start, end uint64) []uint64 {
	txs := make([]uint64, 0, end-start+1)
	for tx := start; tx <= end; tx++ {
		txs = append(txs, tx)
	}
	return txs
}

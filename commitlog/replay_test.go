package commitlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestReplayLogReplaysAcrossSegmentsFromZeroAndReturnsMaxTxID(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)

	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)
	writeReplaySegment(t, root, 3,
		replayRecord{txID: 3, deletes: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)

	segments := []SegmentInfo{
		{Path: filepath.Join(root, SegmentFileName(1)), StartTx: 1, LastTx: 2, Valid: true},
		{Path: filepath.Join(root, SegmentFileName(3)), StartTx: 3, LastTx: 4, Valid: true},
	}

	maxTxID, err := ReplayLog(committed, segments, 0, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 4 {
		t.Fatalf("ReplayLog max tx = %d, want 4", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{2: "bob", 3: "carol"})
}

func TestReplayLogSkipsRecordsAtOrBelowFromTxID(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	seedReplayState(t, committed, map[uint64]string{1: "alice", 2: "bob"})

	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)
	writeReplaySegment(t, root, 3,
		replayRecord{txID: 3, deletes: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)

	segments := []SegmentInfo{
		{Path: filepath.Join(root, SegmentFileName(1)), StartTx: 1, LastTx: 2, Valid: true},
		{Path: filepath.Join(root, SegmentFileName(3)), StartTx: 3, LastTx: 4, Valid: true},
	}

	maxTxID, err := ReplayLog(committed, segments, 2, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 4 {
		t.Fatalf("ReplayLog max tx = %d, want 4", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{2: "bob", 3: "carol"})
}

func TestReplayLogEmptyReplayReturnsFromTxID(t *testing.T) {
	committed, reg := buildReplayCommittedState(t)

	maxTxID, err := ReplayLog(committed, nil, 7, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 7 {
		t.Fatalf("ReplayLog max tx = %d, want 7", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{})
}

func TestReplayLogSkipAllRecordsReturnsFromTxID(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	seedReplayState(t, committed, map[uint64]string{1: "alice", 2: "bob"})

	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)
	segments := []SegmentInfo{{Path: filepath.Join(root, SegmentFileName(1)), StartTx: 1, LastTx: 2, Valid: true}}

	maxTxID, err := ReplayLog(committed, segments, 2, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("ReplayLog max tx = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{1: "alice", 2: "bob"})
}

func TestReplayLogDamagedTailStopsAtValidatedPrefix(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)

	path := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)
	truncateScanTestFileToOffset(t, path, int64(scanTestRecordPayloadOffset(t, path, 2, 10)))

	segments, _, err := ScanSegments(root)
	if err != nil {
		t.Fatal(err)
	}

	maxTxID, err := ReplayLog(committed, segments, 0, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("ReplayLog max tx = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{1: "alice", 2: "bob"})
}

func TestReplayLogSkipsDamagedTailSegmentWhenFromTxIDAlreadyAtValidatedPrefix(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	seedReplayState(t, committed, map[uint64]string{1: "alice", 2: "bob"})

	path := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)
	truncateScanTestFileToOffset(t, path, int64(scanTestRecordPayloadOffset(t, path, 2, 10)))

	segments, _, err := ScanSegments(root)
	if err != nil {
		t.Fatal(err)
	}

	maxTxID, err := ReplayLog(committed, segments, 2, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("ReplayLog max tx = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{1: "alice", 2: "bob"})
}

func TestReplayLogPreallocatedZeroTailStopsAtLastRecord(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	path := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(make([]byte, RecordOverhead)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	segments, horizon, err := ScanSegments(root)
	if err != nil {
		t.Fatal(err)
	}
	if horizon != 2 {
		t.Fatalf("horizon = %d, want 2", horizon)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	if segments[0].AppendMode != AppendInPlace {
		t.Fatalf("append mode = %d, want %d", segments[0].AppendMode, AppendInPlace)
	}

	maxTxID, err := ReplayLog(committed, segments, 0, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("ReplayLog max tx = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{1: "alice", 2: "bob"})
}

// writeDenseReplaySegment writes n monotonically-tx'd inserts and returns the
// segment path plus the (txID, segment byte offset) pair for every record.
// Useful for populating a sparse offset index on top of a real segment.
func writeDenseReplaySegment(t *testing.T, root string, startTx, n uint64) (string, []OffsetIndexEntry) {
	t.Helper()
	seg, err := CreateSegment(root, startTx)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]OffsetIndexEntry, 0, n)
	for i := uint64(0); i < n; i++ {
		tx := startTx + i
		payload, encErr := EncodeChangeset(&store.Changeset{
			TxID: types.TxID(tx),
			Tables: map[schema.TableID]*store.TableChangeset{
				0: {
					TableID:   0,
					TableName: "players",
					Inserts:   []types.ProductValue{{types.NewUint64(tx), types.NewString("p")}},
				},
			},
		})
		if encErr != nil {
			_ = seg.Close()
			t.Fatal(encErr)
		}
		if err := seg.Append(&Record{TxID: tx, RecordType: RecordTypeChangeset, Payload: payload}); err != nil {
			_ = seg.Close()
			t.Fatal(err)
		}
		off, ok := seg.LastRecordByteOffset()
		if !ok {
			_ = seg.Close()
			t.Fatal("LastRecordByteOffset not set after Append")
		}
		entries = append(entries, OffsetIndexEntry{TxID: types.TxID(tx), ByteOffset: uint64(off)})
	}
	if err := seg.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, SegmentFileName(startTx)), entries
}

// countingReplayHook installs replayDecodeHook for the lifetime of a single
// ReplayLog invocation and returns a function that restores the prior hook
// and yields the decoded count.
func countingReplayHook(t *testing.T) (restore func() int64, counter *int64) {
	t.Helper()
	var n int64
	prev := replayDecodeHook
	replayDecodeHook = func(*Record) { n++ }
	return func() int64 {
		replayDecodeHook = prev
		return n
	}, &n
}

// Pin 17.
func TestReplayLogUsesIndexToSkipPastHorizon(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(1024)
	const horizon = types.TxID(512)

	segPath, entries := writeDenseReplaySegment(t, root, startTx, n)

	// Populate a sparse index at every 64th record.
	sparse := make([]OffsetIndexEntry, 0, n/64)
	for i := uint64(0); i < uint64(len(entries)); i += 64 {
		sparse = append(sparse, entries[i])
	}
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	idx := populateSparseIndex(t, idxPath, 64, sparse)
	_ = idx.Close()

	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}

	// Indexed replay.
	committedIdx, reg := buildReplayCommittedState(t)
	restore, counter := countingReplayHook(t)
	if _, err := ReplayLog(committedIdx, segments, horizon, reg); err != nil {
		restore()
		t.Fatalf("indexed replay: %v", err)
	}
	indexedCount := restore()
	_ = counter

	// Linear baseline: remove sidecar, rerun.
	if err := os.Remove(idxPath); err != nil {
		t.Fatalf("remove sidecar: %v", err)
	}
	committedLin, regLin := buildReplayCommittedState(t)
	restore, counter = countingReplayHook(t)
	if _, err := ReplayLog(committedLin, segments, horizon, regLin); err != nil {
		restore()
		t.Fatalf("linear replay: %v", err)
	}
	linearCount := restore()
	_ = counter

	t.Logf("replay decode counts: indexed=%d linear=%d (horizon=%d, n=%d)", indexedCount, linearCount, horizon, n)
	if indexedCount >= linearCount {
		t.Fatalf("expected indexed replay to decode strictly fewer records than linear: indexed=%d linear=%d", indexedCount, linearCount)
	}
	assertReplayStatesEqual(t, committedIdx, committedLin)
}

// Pin 18.
func TestReplayLogCorrectWhenIndexMissing(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(1024)
	const horizon = types.TxID(512)

	segPath, entries := writeDenseReplaySegment(t, root, startTx, n)

	// Populate index first, then delete so the on-disk artifact never exists
	// during ReplayLog's pass. Baseline run with index present.
	sparse := make([]OffsetIndexEntry, 0, n/64)
	for i := uint64(0); i < uint64(len(entries)); i += 64 {
		sparse = append(sparse, entries[i])
	}
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	idx := populateSparseIndex(t, idxPath, 64, sparse)
	_ = idx.Close()

	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}

	committedWith, reg := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedWith, segments, horizon, reg); err != nil {
		t.Fatalf("with-index replay: %v", err)
	}

	if err := os.Remove(idxPath); err != nil {
		t.Fatalf("remove sidecar: %v", err)
	}
	committedWithout, regWithout := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedWithout, segments, horizon, regWithout); err != nil {
		t.Fatalf("without-index replay: %v", err)
	}

	assertReplayStatesEqual(t, committedWith, committedWithout)
}

func TestReplayLogFallsBackWhenOffsetIndexPathIsUnopenable(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(16)
	const horizon = types.TxID(8)

	segPath, _ := writeDenseReplaySegment(t, root, startTx, n)
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	if err := os.Mkdir(idxPath, 0o755); err != nil {
		t.Fatal(err)
	}

	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}
	committed, reg := buildReplayCommittedState(t)
	maxTxID, err := ReplayLog(committed, segments, horizon, reg)
	if err != nil {
		t.Fatalf("replay with unopenable advisory index: %v", err)
	}
	if maxTxID != types.TxID(n) {
		t.Fatalf("ReplayLog max tx = %d, want %d", maxTxID, n)
	}

	wantRows := map[uint64]string{}
	for tx := uint64(horizon + 1); tx <= n; tx++ {
		wantRows[tx] = "p"
	}
	assertReplayPlayerRows(t, committed, wantRows)
}

func TestReplayLogFallsBackWhenOffsetIndexPathIsSymlinkWithoutMutatingTarget(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(16)
	const horizon = types.TxID(8)

	segPath, _ := writeDenseReplaySegment(t, root, startTx, n)
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "external.idx")
	before := []byte("external replay index target")
	if err := os.WriteFile(targetPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	symlinkOrSkip(t, targetPath, idxPath)

	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}
	committed, reg := buildReplayCommittedState(t)
	maxTxID, err := ReplayLog(committed, segments, horizon, reg)
	if err != nil {
		t.Fatalf("replay with symlink advisory index: %v", err)
	}
	if maxTxID != types.TxID(n) {
		t.Fatalf("ReplayLog max tx = %d, want %d", maxTxID, n)
	}
	after, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("symlink index target changed: got %q want %q", after, before)
	}

	wantRows := map[uint64]string{}
	for tx := uint64(horizon + 1); tx <= n; tx++ {
		wantRows[tx] = "p"
	}
	assertReplayPlayerRows(t, committed, wantRows)
}

func TestReplayLogFallsBackWhenOffsetIndexPathIsDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(16)
	const horizon = types.TxID(8)

	segPath, _ := writeDenseReplaySegment(t, root, startTx, n)
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	symlinkOrSkip(t, filepath.Join(root, "missing-index-target"), idxPath)

	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}
	committed, reg := buildReplayCommittedState(t)
	maxTxID, err := ReplayLog(committed, segments, horizon, reg)
	if err != nil {
		t.Fatalf("replay with dangling advisory index: %v", err)
	}
	if maxTxID != types.TxID(n) {
		t.Fatalf("ReplayLog max tx = %d, want %d", maxTxID, n)
	}
	assertSymlinkExists(t, idxPath)

	wantRows := map[uint64]string{}
	for tx := uint64(horizon + 1); tx <= n; tx++ {
		wantRows[tx] = "p"
	}
	assertReplayPlayerRows(t, committed, wantRows)
}

// Pin 23.
func TestReplayCorrectAfterPartialIndexTail(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(1024)
	const horizon = types.TxID(512)

	segPath, entries := writeDenseReplaySegment(t, root, startTx, n)

	sparse := make([]OffsetIndexEntry, 0, n/64)
	for i := uint64(0); i < uint64(len(entries)); i += 64 {
		sparse = append(sparse, entries[i])
	}
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	idx := populateSparseIndex(t, idxPath, 64, sparse)
	_ = idx.Close()

	// Clean-index replay as the reference state.
	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}
	committedClean, reg := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedClean, segments, horizon, reg); err != nil {
		t.Fatalf("clean-index replay: %v", err)
	}

	// Corrupt the sidecar: zero out the value half of the last valid entry,
	// simulating "key half landed, value half did not" mid-entry crash.
	lastValidEntryOffset := int64(uint64(len(sparse)-1) * OffsetIndexEntrySize)
	f, err := os.OpenFile(idxPath, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	var zeros [8]byte
	if _, err := f.WriteAt(zeros[:], lastValidEntryOffset+8); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Replay with the corrupted sidecar. The key-only partial entry must be
	// treated as absent; earlier valid entries may still assist the seek.
	committedPartial, regPartial := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedPartial, segments, horizon, regPartial); err != nil {
		t.Fatalf("partial-tail replay: %v", err)
	}

	assertReplayStatesEqual(t, committedClean, committedPartial)
}

func TestReplayCorrectAfterIndexOffsetPastEOF(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(1024)
	const horizon = types.TxID(512)

	segPath, entries := writeDenseReplaySegment(t, root, startTx, n)

	sparse := make([]OffsetIndexEntry, 0, n/64)
	var corruptEntry int
	for i := uint64(0); i < uint64(len(entries)); i += 64 {
		if entries[i].TxID == horizon+1 {
			corruptEntry = len(sparse)
		}
		sparse = append(sparse, entries[i])
	}
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	idx := populateSparseIndex(t, idxPath, 64, sparse)
	_ = idx.Close()

	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}
	committedClean, reg := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedClean, segments, horizon, reg); err != nil {
		t.Fatalf("clean-index replay: %v", err)
	}

	segInfo, err := os.Stat(segPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(idxPath, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	var bogus [8]byte
	binary.LittleEndian.PutUint64(bogus[:], uint64(segInfo.Size())+1024)
	if _, err := f.WriteAt(bogus[:], int64(corruptEntry*OffsetIndexEntrySize+offsetIndexValOff)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	committedCorrupt, regCorrupt := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedCorrupt, segments, horizon, regCorrupt); err != nil {
		t.Fatalf("past-EOF-index replay: %v", err)
	}

	assertReplayStatesEqual(t, committedClean, committedCorrupt)
}

func TestReplayCorrectAfterIndexOffsetInsideRecord(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(1024)
	const horizon = types.TxID(512)

	segPath, entries := writeDenseReplaySegment(t, root, startTx, n)

	sparse := make([]OffsetIndexEntry, 0, n/64)
	corruptEntry := -1
	var corruptOffset uint64
	for i := uint64(0); i < uint64(len(entries)); i += 64 {
		if entries[i].TxID == horizon+1 {
			corruptEntry = len(sparse)
			corruptOffset = entries[i].ByteOffset + 1
		}
		sparse = append(sparse, entries[i])
	}
	if corruptEntry < 0 {
		t.Fatal("test setup did not find indexed horizon+1 entry")
	}
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	idx := populateSparseIndex(t, idxPath, 64, sparse)
	_ = idx.Close()

	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}
	committedClean, reg := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedClean, segments, horizon, reg); err != nil {
		t.Fatalf("clean-index replay: %v", err)
	}

	f, err := os.OpenFile(idxPath, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	var bogus [8]byte
	binary.LittleEndian.PutUint64(bogus[:], corruptOffset)
	if _, err := f.WriteAt(bogus[:], int64(corruptEntry*OffsetIndexEntrySize+offsetIndexValOff)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	committedCorrupt, regCorrupt := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedCorrupt, segments, horizon, regCorrupt); err != nil {
		t.Fatalf("inside-record-index replay: %v", err)
	}

	assertReplayStatesEqual(t, committedClean, committedCorrupt)
}

func TestReplayCorrectAfterIndexKeyOffsetMismatch(t *testing.T) {
	root := t.TempDir()
	const startTx = uint64(1)
	const n = uint64(1024)
	const horizon = types.TxID(512)

	segPath, entries := writeDenseReplaySegment(t, root, startTx, n)

	sparse := make([]OffsetIndexEntry, 0, n/64)
	corruptEntry := -1
	var laterRecordOffset uint64
	for i := uint64(0); i < uint64(len(entries)); i += 64 {
		if entries[i].TxID == horizon+1 {
			corruptEntry = len(sparse)
			laterRecordOffset = entries[i+64].ByteOffset
		}
		sparse = append(sparse, entries[i])
	}
	if corruptEntry < 0 {
		t.Fatal("test setup did not find indexed horizon+1 entry")
	}
	idxPath := filepath.Join(root, OffsetIndexFileName(startTx))
	idx := populateSparseIndex(t, idxPath, 64, sparse)
	_ = idx.Close()

	segments := []SegmentInfo{{Path: segPath, StartTx: types.TxID(startTx), LastTx: types.TxID(startTx + n - 1), Valid: true}}
	committedClean, reg := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedClean, segments, horizon, reg); err != nil {
		t.Fatalf("clean-index replay: %v", err)
	}

	f, err := os.OpenFile(idxPath, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	var bogus [8]byte
	binary.LittleEndian.PutUint64(bogus[:], laterRecordOffset)
	if _, err := f.WriteAt(bogus[:], int64(corruptEntry*OffsetIndexEntrySize+offsetIndexValOff)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	committedCorrupt, regCorrupt := buildReplayCommittedState(t)
	if _, err := ReplayLog(committedCorrupt, segments, horizon, regCorrupt); err != nil {
		t.Fatalf("key-offset-mismatch replay: %v", err)
	}

	assertReplayStatesEqual(t, committedClean, committedCorrupt)
}

func assertReplayStatesEqual(t *testing.T, a, b *store.CommittedState) {
	t.Helper()
	ta, okA := a.Table(0)
	tb, okB := b.Table(0)
	if !okA || !okB {
		t.Fatal("players table missing")
	}
	if ta.RowCount() != tb.RowCount() {
		t.Fatalf("row count: a=%d b=%d", ta.RowCount(), tb.RowCount())
	}
	aRows := map[uint64]string{}
	for _, row := range ta.Scan() {
		aRows[row[0].AsUint64()] = row[1].AsString()
	}
	bRows := map[uint64]string{}
	for _, row := range tb.Scan() {
		bRows[row[0].AsUint64()] = row[1].AsString()
	}
	if len(aRows) != len(bRows) {
		t.Fatalf("distinct row maps differ: a=%d b=%d", len(aRows), len(bRows))
	}
	for id, av := range aRows {
		if bv, ok := bRows[id]; !ok || bv != av {
			t.Fatalf("row %d: a=%q b=%q (ok=%v)", id, av, bv, ok)
		}
	}
}

func TestReplayLogDecodeErrorIncludesTxAndSegmentContext(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	segmentPath := writeReplaySegment(t, root, 5, replayRecord{txID: 5, rawPayload: []byte{0xFF}})
	segments := []SegmentInfo{{Path: segmentPath, StartTx: 5, LastTx: 5, Valid: true}}

	_, err := ReplayLog(committed, segments, 0, reg)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "tx 5") {
		t.Fatalf("decode error %q missing tx context", err)
	}
	if !strings.Contains(err.Error(), segmentPath) {
		t.Fatalf("decode error %q missing segment path", err)
	}
}

func TestReplayLogApplyErrorIncludesTxAndSegmentContext(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	segmentPath := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice-again")}}},
	)
	segments := []SegmentInfo{{Path: segmentPath, StartTx: 1, LastTx: 2, Valid: true}}

	_, err := ReplayLog(committed, segments, 0, reg)
	if err == nil {
		t.Fatal("expected apply error")
	}
	var pkErr *store.PrimaryKeyViolationError
	if !errors.As(err, &pkErr) {
		t.Fatalf("expected wrapped PrimaryKeyViolationError, got %v", err)
	}
	if !strings.Contains(err.Error(), "tx 2") {
		t.Fatalf("apply error %q missing tx context", err)
	}
	if !strings.Contains(err.Error(), segmentPath) {
		t.Fatalf("apply error %q missing segment path", err)
	}
}

type replayRecord struct {
	txID       uint64
	inserts    []types.ProductValue
	deletes    []types.ProductValue
	rawPayload []byte
}

func buildReplayCommittedState(t testing.TB) (*store.CommittedState, schema.SchemaRegistry) {
	t.Helper()
	_, reg := testSchema()
	return newReplayCommittedState(t, reg), reg
}

func newReplayCommittedState(t testing.TB, reg schema.SchemaRegistry) *store.CommittedState {
	t.Helper()
	committed := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		tableSchema, _ := reg.Table(tableID)
		committed.RegisterTable(tableID, store.NewTable(tableSchema))
	}
	return committed
}

func seedReplayState(t testing.TB, committed *store.CommittedState, rows map[uint64]string) {
	t.Helper()
	table, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	ids := make([]uint64, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(id), types.NewString(rows[id])}); err != nil {
			t.Fatal(err)
		}
	}
}

func writeReplaySegment(t testing.TB, root string, startTx uint64, records ...replayRecord) string {
	t.Helper()
	seg, err := CreateSegment(root, startTx)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range records {
		payload := rec.rawPayload
		if payload == nil {
			payload, err = EncodeChangeset(&store.Changeset{
				TxID: types.TxID(rec.txID),
				Tables: map[schema.TableID]*store.TableChangeset{
					0: {
						TableID:   0,
						TableName: "players",
						Inserts:   rec.inserts,
						Deletes:   rec.deletes,
					},
				},
			})
			if err != nil {
				_ = seg.Close()
				t.Fatal(err)
			}
		}
		if err := seg.Append(&Record{TxID: rec.txID, RecordType: RecordTypeChangeset, Payload: payload}); err != nil {
			_ = seg.Close()
			t.Fatal(err)
		}
	}
	if err := seg.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, SegmentFileName(startTx))
}

func assertReplayPlayerRows(t *testing.T, committed *store.CommittedState, want map[uint64]string) {
	t.Helper()
	got := collectReplayPlayerRows(t, committed)
	if len(got) != len(want) {
		t.Fatalf("players row count = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for id, wantName := range want {
		if gotName, ok := got[id]; !ok || gotName != wantName {
			t.Fatalf("players rows = %v, want %v", got, want)
		}
	}
}

func collectReplayPlayerRows(t *testing.T, committed *store.CommittedState) map[uint64]string {
	t.Helper()
	table, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	got := make(map[uint64]string, table.RowCount())
	for _, row := range table.Scan() {
		got[row[0].AsUint64()] = row[1].AsString()
	}
	return got
}

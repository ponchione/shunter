package commitlog

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func makeDurabilityTestChangeset(txID uint64) *store.Changeset {
	return &store.Changeset{
		TxID: types.TxID(txID),
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts: []types.ProductValue{
					{types.NewUint64(txID), types.NewString("p")},
				},
			},
		},
	}
}

func TestNewDurabilityWorkerRejectsNegativeChannelCapacity(t *testing.T) {
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = -1

	_, err := NewDurabilityWorker(t.TempDir(), 1, opts)
	if err == nil {
		t.Fatal("NewDurabilityWorker accepted negative channel capacity")
	}
	if !strings.Contains(err.Error(), "channel capacity must be non-negative") {
		t.Fatalf("error = %v, want channel capacity validation", err)
	}
}

func TestDurabilityWorkerRejectsRowsLargerThanRecoveryLimitBeforeAppend(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.MaxRowBytes = 16
	opts.OffsetIndexIntervalBytes = 0
	opts.OffsetIndexCap = 0

	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts: []types.ProductValue{
					{types.NewUint64(1), types.NewString(strings.Repeat("x", int(opts.MaxRowBytes)+1))},
				},
			},
		},
	})

	finalTx, err := dw.Close()
	if finalTx != 0 {
		t.Fatalf("final durable tx = %d, want 0", finalTx)
	}
	var rowErr *RowTooLargeError
	if !errors.As(err, &rowErr) {
		t.Fatalf("Close error = %v, want RowTooLargeError", err)
	}
	if rowErr.Max != opts.MaxRowBytes {
		t.Fatalf("row max = %d, want %d", rowErr.Max, opts.MaxRowBytes)
	}
	info, statErr := os.Stat(filepath.Join(dir, SegmentFileName(1)))
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Size() > SegmentHeaderSize {
		t.Fatalf("segment size after rejected row = %d, want no appended record beyond header %d", info.Size(), SegmentHeaderSize)
	}
}

func TestDurabilityWorkerRejectsRecordPayloadLargerThanRecoveryLimitBeforeAppend(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.MaxRecordPayloadBytes = 32
	opts.MaxRowBytes = 1024
	opts.OffsetIndexIntervalBytes = 0
	opts.OffsetIndexCap = 0

	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts: []types.ProductValue{
					{types.NewUint64(1), types.NewString("payload-a")},
					{types.NewUint64(2), types.NewString("payload-b")},
					{types.NewUint64(3), types.NewString("payload-c")},
				},
			},
		},
	})

	finalTx, err := dw.Close()
	if finalTx != 0 {
		t.Fatalf("final durable tx = %d, want 0", finalTx)
	}
	var recordErr *RecordTooLargeError
	if !errors.As(err, &recordErr) {
		t.Fatalf("Close error = %v, want RecordTooLargeError", err)
	}
	if recordErr.Max != opts.MaxRecordPayloadBytes {
		t.Fatalf("record max = %d, want %d", recordErr.Max, opts.MaxRecordPayloadBytes)
	}
	info, statErr := os.Stat(filepath.Join(dir, SegmentFileName(1)))
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Size() > SegmentHeaderSize {
		t.Fatalf("segment size after rejected payload = %d, want no appended record beyond header %d", info.Size(), SegmentHeaderSize)
	}
}

func TestDurabilityWorkerCloseReturnsFinalSegmentCloseError(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 0
	opts.OffsetIndexCap = 0
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	wait := dw.WaitUntilDurable(1)
	dw.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	select {
	case txID := <-wait:
		if txID != 1 {
			t.Fatalf("waiter tx = %d, want 1", txID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for durable tx 1")
	}
	if err := dw.seg.file.Close(); err != nil {
		t.Fatal(err)
	}

	finalTx, err := dw.Close()
	if finalTx != 1 {
		t.Fatalf("final durable tx = %d, want 1", finalTx)
	}
	if err == nil {
		t.Fatal("Close error = nil, want final segment close/sync error")
	}
}

// Pin 19.
func TestDurabilityWorkerCreatesAndPopulatesIndexPerSegment(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 16
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 1
	opts.OffsetIndexCap = 16

	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}

	const n = 5
	for i := uint64(1); i <= n; i++ {
		dw.EnqueueCommitted(i, makeDurabilityTestChangeset(i))
	}
	finalTx, fatal := dw.Close()
	if fatal != nil {
		t.Fatalf("close fatal: %v", fatal)
	}
	if finalTx != n {
		t.Fatalf("finalTx=%d want %d", finalTx, n)
	}

	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	idx, err := OpenOffsetIndex(idxPath)
	if err != nil {
		t.Fatalf("OpenOffsetIndex: %v", err)
	}
	defer idx.Close()

	ents, err := idx.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(ents) == 0 {
		t.Fatal("expected at least one index entry")
	}
	for i, e := range ents {
		if e.TxID == 0 {
			t.Fatalf("entry %d: zero txID", i)
		}
		if e.ByteOffset < uint64(SegmentHeaderSize) {
			t.Fatalf("entry %d: byteOffset %d < SegmentHeaderSize %d", i, e.ByteOffset, SegmentHeaderSize)
		}
		if i > 0 && ents[i-1].TxID >= e.TxID {
			t.Fatalf("entries not monotonic at %d: prev=%d cur=%d", i, ents[i-1].TxID, e.TxID)
		}
	}
}

// Pin 20.
func TestDurabilityWorkerRotatesIndexOnSegmentRotation(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 16
	opts.DrainBatchSize = 1
	opts.MaxSegmentSize = 10 // force rotation after each commit
	opts.OffsetIndexIntervalBytes = 1
	opts.OffsetIndexCap = 16

	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}

	const n = 3
	for i := uint64(1); i <= n; i++ {
		dw.EnqueueCommitted(i, makeDurabilityTestChangeset(i))
	}
	finalTx, fatal := dw.Close()
	if fatal != nil {
		t.Fatalf("close fatal: %v", fatal)
	}
	if finalTx != n {
		t.Fatalf("finalTx=%d want %d", finalTx, n)
	}

	for _, tx := range []uint64{1, 2, 3} {
		logPath := filepath.Join(dir, SegmentFileName(tx))
		if _, err := os.Stat(logPath); err != nil {
			t.Fatalf("missing segment %d: %v", tx, err)
		}
		idxPath := filepath.Join(dir, OffsetIndexFileName(tx))
		if _, err := os.Stat(idxPath); err != nil {
			t.Fatalf("missing idx %d: %v", tx, err)
		}
		idx, err := OpenOffsetIndex(idxPath)
		if err != nil {
			t.Fatalf("OpenOffsetIndex(%d): %v", tx, err)
		}
		ents, err := idx.Entries()
		_ = idx.Close()
		if err != nil {
			t.Fatalf("Entries(%d): %v", tx, err)
		}
		if len(ents) == 0 {
			t.Fatalf("segment %d: index empty", tx)
		}
		if uint64(ents[0].TxID) != tx {
			t.Fatalf("segment %d: first entry tx=%d want %d", tx, ents[0].TxID, tx)
		}
		if ents[0].ByteOffset != uint64(SegmentHeaderSize) {
			t.Fatalf("segment %d: first entry byteOffset=%d want %d (segment-local coord space)",
				tx, ents[0].ByteOffset, SegmentHeaderSize)
		}
	}
}

func TestDurabilityWorkerRolloverRecoveryMetamorphicEquivalence(t *testing.T) {
	const n = uint64(6)
	_, reg := testSchema()
	cases := []struct {
		name             string
		maxSegmentSize   int64
		wantSegmentStart types.TxID
		wantNextTxID     types.TxID
	}{
		{
			name:             "single-segment",
			maxSegmentSize:   DefaultCommitLogOptions().MaxSegmentSize,
			wantSegmentStart: 1,
			wantNextTxID:     types.TxID(n + 1),
		},
		{
			name:             "rollover-each-tx",
			maxSegmentSize:   1,
			wantSegmentStart: types.TxID(n + 1),
			wantNextTxID:     types.TxID(n + 1),
		},
	}

	var baselineRows map[uint64]string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			opts := DefaultCommitLogOptions()
			opts.ChannelCapacity = int(n)
			opts.DrainBatchSize = 1
			opts.MaxSegmentSize = tc.maxSegmentSize
			opts.OffsetIndexIntervalBytes = 1
			opts.OffsetIndexCap = 16

			dw, err := NewDurabilityWorker(dir, 1, opts)
			if err != nil {
				t.Fatal(err)
			}
			for txID := uint64(1); txID <= n; txID++ {
				dw.EnqueueCommitted(txID, makeDurabilityTestChangeset(txID))
			}
			finalTx, fatalErr := dw.Close()
			if fatalErr != nil {
				t.Fatalf("%s Close fatal: %v", tc.name, fatalErr)
			}
			if finalTx != n {
				t.Fatalf("%s final durable tx = %d, want %d", tc.name, finalTx, n)
			}

			recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
			if err != nil {
				t.Fatalf("%s recovery: %v", tc.name, err)
			}
			if maxTxID != types.TxID(n) {
				t.Fatalf("%s maxTxID = %d, want %d", tc.name, maxTxID, n)
			}
			if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: types.TxID(n)}) {
				t.Fatalf("%s replayed range = %+v, want 1..%d", tc.name, report.ReplayedTxRange, n)
			}
			if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != tc.wantSegmentStart || plan.NextTxID != tc.wantNextTxID {
				t.Fatalf("%s resume plan = %+v, want append-in-place on segment %d at tx %d",
					tc.name, plan, tc.wantSegmentStart, tc.wantNextTxID)
			}
			rows := map[uint64]string{}
			for txID := uint64(1); txID <= n; txID++ {
				rows[txID] = "p"
			}
			assertReplayPlayerRows(t, recovered, rows)
			recoveredRows := collectReplayPlayerRows(t, recovered)
			if baselineRows == nil {
				baselineRows = recoveredRows
				return
			}
			if !maps.Equal(recoveredRows, baselineRows) {
				t.Fatalf("%s recovered rows = %+v, want baseline %+v", tc.name, recoveredRows, baselineRows)
			}
		})
	}
}

func TestDurabilityWorkerSegmentWriteFailureFailsClosedWithoutAdvancingDurable(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 0
	opts.OffsetIndexCap = 0

	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	wait := dw.WaitUntilDurable(1)
	if err := dw.seg.file.Close(); err != nil {
		t.Fatal(err)
	}

	dw.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	finalTx, fatalErr := dw.Close()
	if fatalErr == nil {
		t.Fatal("expected segment write failure to be returned on close")
	}
	if finalTx != 0 {
		t.Fatalf("final durable tx = %d, want 0 after failed write", finalTx)
	}
	if got := dw.DurableTxID(); got != 0 {
		t.Fatalf("DurableTxID = %d, want 0 after failed write", got)
	}
	select {
	case txID := <-wait:
		t.Fatalf("waiter released for tx %d despite failed write", txID)
	default:
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected enqueue after fatal durability failure to panic")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("panic = %T(%v), want error wrapping ErrDurabilityFailed", r, r)
		}
		if !errors.Is(err, ErrDurabilityFailed) {
			t.Fatalf("panic error = %v, want ErrDurabilityFailed", err)
		}
	}()
	dw.EnqueueCommitted(2, makeDurabilityTestChangeset(2))
}

func TestDurabilityWorkerResumeSyncFailureLeavesRecoverableDurablePrefix(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 0
	opts.OffsetIndexCap = 0

	initial, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	initial.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	initial.EnqueueCommitted(2, makeDurabilityTestChangeset(2))
	if finalTx, fatalErr := initial.Close(); fatalErr != nil {
		t.Fatal(fatalErr)
	} else if finalTx != 2 {
		t.Fatalf("initial final durable tx = %d, want 2", finalTx)
	}

	dw, err := NewDurabilityWorkerWithResumePlan(dir, RecoveryResumePlan{
		SegmentStartTx: 1,
		NextTxID:       3,
		AppendMode:     AppendInPlace,
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	wait := dw.WaitUntilDurable(3)
	if err := dw.seg.file.Close(); err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(3, makeDurabilityTestChangeset(3))
	finalTx, fatalErr := dw.Close()
	if fatalErr == nil {
		t.Fatal("expected resumed segment sync failure")
	}
	if finalTx != 2 || dw.DurableTxID() != 2 {
		t.Fatalf("durable tx after resumed sync failure = (%d, %d), want (2, 2)", finalTx, dw.DurableTxID())
	}
	select {
	case txID := <-wait:
		t.Fatalf("waiter released for tx %d despite sync failure", txID)
	default:
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 2}) {
		t.Fatalf("replayed range = %+v, want 1..2", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 3", plan)
	}

	resumed, err := NewDurabilityWorkerWithResumePlan(dir, plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	resumed.EnqueueCommitted(3, makeDurabilityTestChangeset(3))
	if finalTx, fatalErr := resumed.Close(); fatalErr != nil {
		t.Fatal(fatalErr)
	} else if finalTx != 3 {
		t.Fatalf("resumed final durable tx = %d, want 3", finalTx)
	}

	recovered, maxTxID, plan, report, err = OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("second maxTxID = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p", 3: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
		t.Fatalf("second replayed range = %+v, want 1..3", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
		t.Fatalf("second resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
	}
}

func TestDurabilityWorkerResumeFreshSegmentReplacesRolloverArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dir string)
	}{
		{
			name: "zero-length-rollover",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				createZeroLengthSegment(t, dir, 3)
			},
		},
		{
			name: "truncated-first-record-rollover",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				path := writeReplaySegment(t, dir, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("partial")}}},
				)
				truncateScanTestFileToOffset(t, path, int64(SegmentHeaderSize+RecordHeaderSize-1))
			},
		},
		{
			name: "directory-rollover",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				createRolloverDirectoryArtifact(t, dir, 3)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, reg := testSchema()
			writeFaultSnapshot(t, dir, reg, 2, map[uint64]string{1: "p", 2: "p"})
			tc.setup(t, dir)

			recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
			if err != nil {
				t.Fatal(err)
			}
			if maxTxID != 2 {
				t.Fatalf("maxTxID before resume = %d, want 2", maxTxID)
			}
			assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p"})
			if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
				t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
			}
			if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
				t.Fatalf("resume plan = %+v, want fresh segment at tx 3", plan)
			}

			opts := DefaultCommitLogOptions()
			opts.OffsetIndexIntervalBytes = 0
			opts.OffsetIndexCap = 0
			dw, err := NewDurabilityWorkerWithResumePlan(dir, plan, opts)
			if err != nil {
				t.Fatal(err)
			}
			dw.EnqueueCommitted(3, makeDurabilityTestChangeset(3))
			if finalTx, fatalErr := dw.Close(); fatalErr != nil {
				t.Fatal(fatalErr)
			} else if finalTx != 3 {
				t.Fatalf("final durable tx = %d, want 3", finalTx)
			}

			if info, err := os.Stat(filepath.Join(dir, SegmentFileName(3))); err != nil {
				t.Fatalf("replacement segment missing: %v", err)
			} else if info.IsDir() {
				t.Fatal("replacement segment path is still a directory artifact")
			}

			recovered, maxTxID, plan, report, err = OpenAndRecoverWithReport(dir, reg)
			if err != nil {
				t.Fatal(err)
			}
			if maxTxID != 3 {
				t.Fatalf("maxTxID after resume = %d, want 3", maxTxID)
			}
			assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p", 3: "p"})
			if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
				t.Fatalf("selected snapshot report after resume = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
			}
			if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 3, End: 3}) {
				t.Fatalf("replayed range after resume = %+v, want 3..3", report.ReplayedTxRange)
			}
			if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 4 {
				t.Fatalf("post-resume plan = %+v, want append-in-place on segment 3 at tx 4", plan)
			}
		})
	}
}

func TestDurabilityWorkerResumeFreshSegmentTruncatesStaleOffsetIndexSidecar(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, dir, reg, 2, map[uint64]string{1: "p", 2: "p"})
	path := writeReplaySegment(t, dir, 3,
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("discarded")}}},
	)
	truncateScanTestFileToOffset(t, path, int64(SegmentHeaderSize+RecordHeaderSize-1))
	createOrphanOffsetIndex(t, dir, 3,
		OffsetIndexEntry{TxID: 3, ByteOffset: 1 << 32},
	)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID before resume = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p"})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want fresh segment at tx 3", plan)
	}

	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 1
	opts.OffsetIndexCap = 8
	dw, err := NewDurabilityWorkerWithResumePlan(dir, plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(3, makeDurabilityTestChangeset(3))
	if finalTx, fatalErr := dw.Close(); fatalErr != nil {
		t.Fatal(fatalErr)
	} else if finalTx != 3 {
		t.Fatalf("final durable tx = %d, want 3", finalTx)
	}

	idx, err := OpenOffsetIndex(filepath.Join(dir, OffsetIndexFileName(3)))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := idx.Entries()
	if closeErr := idx.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("offset index entries after fresh resume = %+v, want exactly tx 3", entries)
	}
	if entries[0].TxID != 3 || entries[0].ByteOffset != uint64(SegmentHeaderSize) {
		t.Fatalf("fresh segment offset index entry = %+v, want tx 3 at segment header boundary", entries[0])
	}

	recovered, maxTxID, plan, report, err = OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID after resume = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p", 3: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 3, End: 3}) {
		t.Fatalf("replayed range after resume = %+v, want 3..3", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 4 {
		t.Fatalf("post-resume plan = %+v, want append-in-place on segment 3 at tx 4", plan)
	}
}

func TestDurabilityWorkerResumeFreshSegmentRejectsNonEmptyRolloverDirectoryArtifact(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, dir, reg, 2, map[uint64]string{1: "p", 2: "p"})
	artifactDir := filepath.Join(dir, SegmentFileName(3))
	if err := os.Mkdir(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactFile := filepath.Join(artifactDir, "leftover")
	if err := os.WriteFile(artifactFile, []byte("unsafe artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, maxTxID, plan, _, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID before resume = %d, want 2", maxTxID)
	}
	if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want fresh segment at tx 3", plan)
	}

	_, err = NewDurabilityWorkerWithResumePlan(dir, plan, DefaultCommitLogOptions())
	if err == nil {
		t.Fatal("expected non-empty rollover directory artifact to block resume")
	}
	if !strings.Contains(err.Error(), "remove rollover segment directory artifact") {
		t.Fatalf("resume error = %v, want artifact removal context", err)
	}
	if _, statErr := os.Stat(artifactFile); statErr != nil {
		t.Fatalf("non-empty artifact contents should be preserved: %v", statErr)
	}
}

func TestDurabilityWorkerResumeIgnoresSymlinkOffsetIndexWithoutMutatingTarget(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, dir, reg, 2, map[uint64]string{1: "p", 2: "p"})
	writeReplaySegment(t, dir, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("p")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("p")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("p")}}},
	)
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "external.idx")
	before := []byte("external offset index target")
	if err := os.WriteFile(targetPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	symlinkOrSkip(t, targetPath, idxPath)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID before resume = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p", 3: "p"})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 3, End: 3}) {
		t.Fatalf("replayed range = %+v, want 3..3", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
	}

	dw, err := NewDurabilityWorkerWithResumePlan(dir, plan, DefaultCommitLogOptions())
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(4, makeDurabilityTestChangeset(4))
	if finalTx, fatalErr := dw.Close(); fatalErr != nil {
		t.Fatal(fatalErr)
	} else if finalTx != 4 {
		t.Fatalf("final durable tx = %d, want 4", finalTx)
	}
	after, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("symlink target changed: got %q want %q", after, before)
	}

	recovered, maxTxID, plan, report, err = OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 4 {
		t.Fatalf("maxTxID after resume = %d, want 4", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p", 3: "p", 4: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 3, End: 4}) {
		t.Fatalf("replayed range after resume = %+v, want 3..4", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 5 {
		t.Fatalf("post-resume plan = %+v, want append-in-place on segment 1 at tx 5", plan)
	}
}

func TestDurabilityWorkerRolloverCreateFailureLeavesRecoverableDurablePrefix(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	blockedNextSegment := filepath.Join(dir, SegmentFileName(2))
	if err := os.Mkdir(blockedNextSegment, 0o755); err != nil {
		t.Fatal(err)
	}

	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.MaxSegmentSize = 1
	opts.OffsetIndexIntervalBytes = 0
	opts.OffsetIndexCap = 0
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	wait := dw.WaitUntilDurable(1)
	dw.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	finalTx, fatalErr := dw.Close()
	if fatalErr == nil {
		t.Fatal("expected rollover create failure")
	}
	if !strings.Contains(fatalErr.Error(), SegmentFileName(2)) {
		t.Fatalf("rollover failure = %v, want blocked successor segment path", fatalErr)
	}
	if finalTx != 1 || dw.DurableTxID() != 1 {
		t.Fatalf("durable tx after rollover failure = (%d, %d), want (1, 1)", finalTx, dw.DurableTxID())
	}
	select {
	case txID := <-wait:
		if txID != 1 {
			t.Fatalf("waiter tx = %d, want 1", txID)
		}
	default:
		t.Fatal("waiter for synced prefix should be released before rollover failure")
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 1 {
		t.Fatalf("maxTxID = %d, want 1", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 1}) {
		t.Fatalf("replayed range = %+v, want 1..1", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 2 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 2", plan)
	}
}

func TestDurabilityWorkerAppendInPlaceResumeTruncatesStaleOffsetIndexTail(t *testing.T) {
	dir := t.TempDir()
	makeScanTestSegment(t, dir, 1, 1, 2)

	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	idx, err := CreateOffsetIndex(idxPath, 8)
	if err != nil {
		t.Fatal(err)
	}
	staleFutureOffset := uint64(1 << 32)
	for _, entry := range []OffsetIndexEntry{
		{TxID: 1, ByteOffset: uint64(SegmentHeaderSize)},
		{TxID: 2, ByteOffset: uint64(SegmentHeaderSize + RecordOverhead + 1)},
		{TxID: 3, ByteOffset: staleFutureOffset},
	} {
		if err := idx.Append(entry.TxID, entry.ByteOffset); err != nil {
			_ = idx.Close()
			t.Fatal(err)
		}
	}
	if err := idx.Sync(); err != nil {
		_ = idx.Close()
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 1
	opts.OffsetIndexCap = 8
	dw, err := NewDurabilityWorkerWithResumePlan(dir, RecoveryResumePlan{
		SegmentStartTx: 1,
		NextTxID:       3,
		AppendMode:     AppendInPlace,
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := dw.DurableTxID(); got != 2 {
		t.Fatalf("resumed durable tx = %d, want 2", got)
	}
	dw.EnqueueCommitted(3, makeDurabilityTestChangeset(3))
	finalTx, fatalErr := dw.Close()
	if fatalErr != nil {
		t.Fatal(fatalErr)
	}
	if finalTx != 3 {
		t.Fatalf("final durable tx = %d, want 3", finalTx)
	}

	reopened, err := OpenOffsetIndex(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("offset index entries = %+v, want tx 1,2 plus rewritten tx 3", entries)
	}
	for i, wantTx := range []types.TxID{1, 2, 3} {
		if entries[i].TxID != wantTx {
			t.Fatalf("offset index entries = %+v, want tx sequence [1 2 3]", entries)
		}
	}
	if entries[2].ByteOffset == staleFutureOffset {
		t.Fatalf("stale future index offset survived resume: %+v", entries)
	}
	if entries[2].ByteOffset <= entries[1].ByteOffset {
		t.Fatalf("rewritten tx 3 offset = %d, want beyond tx 2 offset %d", entries[2].ByteOffset, entries[1].ByteOffset)
	}
}

func TestDurabilityWorkerResumeWithUnopenableOffsetIndexDisablesIndexAndRecovers(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()

	initialOpts := DefaultCommitLogOptions()
	initialOpts.ChannelCapacity = 2
	initialOpts.DrainBatchSize = 1
	initialOpts.OffsetIndexIntervalBytes = 0
	initialOpts.OffsetIndexCap = 0
	initial, err := NewDurabilityWorker(dir, 1, initialOpts)
	if err != nil {
		t.Fatal(err)
	}
	initial.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	initial.EnqueueCommitted(2, makeDurabilityTestChangeset(2))
	if finalTx, fatalErr := initial.Close(); fatalErr != nil {
		t.Fatal(fatalErr)
	} else if finalTx != 2 {
		t.Fatalf("initial final durable tx = %d, want 2", finalTx)
	}

	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	if err := os.Mkdir(idxPath, 0o755); err != nil {
		t.Fatal(err)
	}

	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 1
	opts.OffsetIndexCap = 8
	dw, err := NewDurabilityWorkerWithResumePlan(dir, RecoveryResumePlan{
		SegmentStartTx: 1,
		NextTxID:       3,
		AppendMode:     AppendInPlace,
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := dw.DurableTxID(); got != 2 {
		t.Fatalf("resumed durable tx = %d, want 2", got)
	}
	if dw.idx != nil {
		t.Fatal("unopenable advisory offset index should disable indexing")
	}
	dw.EnqueueCommitted(3, makeDurabilityTestChangeset(3))
	finalTx, fatalErr := dw.Close()
	if fatalErr != nil {
		t.Fatal(fatalErr)
	}
	if finalTx != 3 {
		t.Fatalf("final durable tx = %d, want 3", finalTx)
	}
	if info, err := os.Stat(idxPath); err != nil || !info.IsDir() {
		t.Fatalf("unopenable index artifact stat = (%v, %v), want directory still present", info, err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p", 3: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
		t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
	}
}

func TestDurabilityWorkerInitialUnopenableOffsetIndexDoesNotBlockRecovery(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	idxPath := filepath.Join(dir, OffsetIndexFileName(1))
	if err := os.Mkdir(idxPath, 0o755); err != nil {
		t.Fatal(err)
	}

	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 2
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 1
	opts.OffsetIndexCap = 8
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	dw.EnqueueCommitted(2, makeDurabilityTestChangeset(2))
	finalTx, fatalErr := dw.Close()
	if fatalErr != nil {
		t.Fatalf("initial unopenable offset index should not fail durability: %v", fatalErr)
	}
	if finalTx != 2 || dw.DurableTxID() != 2 {
		t.Fatalf("durable tx after unopenable initial index = (%d, %d), want (2, 2)", finalTx, dw.DurableTxID())
	}
	if info, err := os.Stat(idxPath); err != nil || !info.IsDir() {
		t.Fatalf("unopenable initial index artifact stat = (%v, %v), want directory still present", info, err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 2}) {
		t.Fatalf("replayed range = %+v, want 1..2", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 3", plan)
	}
}

func TestDurabilityWorkerOffsetIndexFailureDoesNotBlockDurablePrefix(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()

	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	opts.OffsetIndexIntervalBytes = 1
	opts.OffsetIndexCap = 8
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dw.idx == nil || dw.idx.head == nil || dw.idx.head.f == nil {
		t.Fatal("expected enabled offset index writer")
	}
	if err := dw.idx.head.f.Close(); err != nil {
		t.Fatal(err)
	}

	wait := dw.WaitUntilDurable(1)
	dw.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	finalTx, fatalErr := dw.Close()
	if fatalErr != nil {
		t.Fatalf("offset index failure should not fail durability: %v", fatalErr)
	}
	if finalTx != 1 || dw.DurableTxID() != 1 {
		t.Fatalf("durable tx after offset index failure = (%d, %d), want (1, 1)", finalTx, dw.DurableTxID())
	}
	select {
	case txID := <-wait:
		if txID != 1 {
			t.Fatalf("waiter tx = %d, want 1", txID)
		}
	default:
		t.Fatal("waiter for segment-synced prefix should be released despite index failure")
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 1 {
		t.Fatalf("maxTxID = %d, want 1", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 1}) {
		t.Fatalf("replayed range = %+v, want 1..1", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 2 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 2", plan)
	}

	idx, err := OpenOffsetIndex(filepath.Join(dir, OffsetIndexFileName(1)))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	entries, err := idx.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed advisory index entries = %+v, want empty safe sidecar", entries)
	}
}

func TestDurabilityWorkerRolloverUnopenableOffsetIndexDoesNotBlockRecovery(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	idxPath := filepath.Join(dir, OffsetIndexFileName(2))
	if err := os.Mkdir(idxPath, 0o755); err != nil {
		t.Fatal(err)
	}

	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 2
	opts.DrainBatchSize = 1
	opts.MaxSegmentSize = 1
	opts.OffsetIndexIntervalBytes = 1
	opts.OffsetIndexCap = 8
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	dw.EnqueueCommitted(2, makeDurabilityTestChangeset(2))
	finalTx, fatalErr := dw.Close()
	if fatalErr != nil {
		t.Fatalf("rollover unopenable offset index should not fail durability: %v", fatalErr)
	}
	if finalTx != 2 || dw.DurableTxID() != 2 {
		t.Fatalf("durable tx after unopenable rollover index = (%d, %d), want (2, 2)", finalTx, dw.DurableTxID())
	}
	if info, err := os.Stat(idxPath); err != nil || !info.IsDir() {
		t.Fatalf("unopenable rollover index artifact stat = (%v, %v), want directory still present", info, err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "p", 2: "p"})
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 2}) {
		t.Fatalf("replayed range = %+v, want 1..2", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want append-in-place on header-only segment 3 at tx 3", plan)
	}
}

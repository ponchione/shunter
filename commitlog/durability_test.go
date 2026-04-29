package commitlog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

package commitlog

import (
	"os"
	"path/filepath"
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

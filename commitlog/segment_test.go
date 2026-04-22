package commitlog

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/types"
)

// buildSegmentWithTxIDs writes records at the given TxIDs into a fresh
// segment under dir. Returns the set of (txID, byte offset) pairs suitable
// for populating a sparse offset index. Segment start TxID = txs[0].
func buildSegmentWithTxIDs(t *testing.T, dir string, txs []uint64) []OffsetIndexEntry {
	t.Helper()
	if len(txs) == 0 {
		t.Fatal("buildSegmentWithTxIDs: empty txs")
	}
	sw, err := CreateSegment(dir, txs[0])
	if err != nil {
		t.Fatalf("CreateSegment: %v", err)
	}
	entries := make([]OffsetIndexEntry, 0, len(txs))
	for _, tx := range txs {
		rec := &Record{TxID: tx, RecordType: RecordTypeChangeset, Payload: []byte{byte(tx)}}
		if err := sw.Append(rec); err != nil {
			t.Fatalf("Append(%d): %v", tx, err)
		}
		off, ok := sw.LastRecordByteOffset()
		if !ok {
			t.Fatalf("LastRecordByteOffset not set after Append(%d)", tx)
		}
		entries = append(entries, OffsetIndexEntry{TxID: types.TxID(tx), ByteOffset: uint64(off)})
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return entries
}

func populateSparseIndex(t *testing.T, path string, cap uint64, subset []OffsetIndexEntry) *OffsetIndex {
	t.Helper()
	mut, err := CreateOffsetIndex(path, cap)
	if err != nil {
		t.Fatalf("CreateOffsetIndex: %v", err)
	}
	for _, e := range subset {
		if err := mut.Append(e.TxID, e.ByteOffset); err != nil {
			t.Fatalf("idx.Append(%d,%d): %v", e.TxID, e.ByteOffset, err)
		}
	}
	if err := mut.Sync(); err != nil {
		t.Fatalf("idx.Sync: %v", err)
	}
	if err := mut.Close(); err != nil {
		t.Fatalf("idx.Close: %v", err)
	}
	ro, err := OpenOffsetIndex(path)
	if err != nil {
		t.Fatalf("OpenOffsetIndex: %v", err)
	}
	return ro
}

func openSegmentReader(t *testing.T, dir string, startTx uint64) *SegmentReader {
	t.Helper()
	sr, err := OpenSegment(filepath.Join(dir, SegmentFileName(startTx)))
	if err != nil {
		t.Fatalf("OpenSegment: %v", err)
	}
	return sr
}

// Pin 14.
func TestSegmentReaderSeekToTxIDUsesIndex(t *testing.T) {
	dir := t.TempDir()
	entries := buildSegmentWithTxIDs(t, dir, []uint64{10, 20, 30, 40, 50})

	sparse := []OffsetIndexEntry{entries[0], entries[2], entries[4]} // {10, 30, 50}
	idx := populateSparseIndex(t, filepath.Join(dir, "00000000000000000010.idx"), 16, sparse)
	defer idx.Close()

	sr := openSegmentReader(t, dir, 10)
	defer sr.Close()

	if err := sr.SeekToTxID(35, idx); err != nil {
		t.Fatalf("SeekToTxID(35): %v", err)
	}
	rec, err := sr.Next()
	if err != nil {
		t.Fatalf("Next after seek: %v", err)
	}
	if rec.TxID != 40 {
		t.Fatalf("landed on TxID %d, want 40", rec.TxID)
	}
}

// Pin 15.
func TestSegmentReaderSeekToTxIDFallsBackWithoutIndex(t *testing.T) {
	dir := t.TempDir()
	buildSegmentWithTxIDs(t, dir, []uint64{10, 20, 30, 40, 50})

	sr := openSegmentReader(t, dir, 10)
	defer sr.Close()

	if err := sr.SeekToTxID(35, nil); err != nil {
		t.Fatalf("SeekToTxID(35, nil): %v", err)
	}
	rec, err := sr.Next()
	if err != nil {
		t.Fatalf("Next after seek: %v", err)
	}
	if rec.TxID != 40 {
		t.Fatalf("landed on TxID %d, want 40", rec.TxID)
	}

	// Exact hit: target present in segment.
	sr2 := openSegmentReader(t, dir, 10)
	defer sr2.Close()
	if err := sr2.SeekToTxID(30, nil); err != nil {
		t.Fatalf("SeekToTxID(30, nil): %v", err)
	}
	rec, err = sr2.Next()
	if err != nil {
		t.Fatalf("Next after seek: %v", err)
	}
	if rec.TxID != 30 {
		t.Fatalf("landed on TxID %d, want 30", rec.TxID)
	}

	// Target past end: EOF on Next.
	sr3 := openSegmentReader(t, dir, 10)
	defer sr3.Close()
	if err := sr3.SeekToTxID(99, nil); err != nil {
		t.Fatalf("SeekToTxID(99, nil): %v", err)
	}
	if _, err := sr3.Next(); err != io.EOF {
		t.Fatalf("Next after past-end seek: got %v, want io.EOF", err)
	}
}

// Pin 16.
func TestSegmentReaderSeekToTxIDFallsBackOnMissingKey(t *testing.T) {
	dir := t.TempDir()
	entries := buildSegmentWithTxIDs(t, dir, []uint64{10, 20, 30, 40, 50})

	// Populate the index starting at TxID 30 so KeyLookup(15) returns
	// ErrOffsetIndexKeyNotFound (15 is below the first indexed key).
	sparse := []OffsetIndexEntry{entries[2], entries[4]} // {30, 50}
	idx := populateSparseIndex(t, filepath.Join(dir, "00000000000000000010.idx"), 16, sparse)
	defer idx.Close()

	if _, _, err := idx.KeyLookup(15); !errors.Is(err, ErrOffsetIndexKeyNotFound) {
		t.Fatalf("precondition: KeyLookup(15) = %v, want ErrOffsetIndexKeyNotFound", err)
	}

	sr := openSegmentReader(t, dir, 10)
	defer sr.Close()

	if err := sr.SeekToTxID(15, idx); err != nil {
		t.Fatalf("SeekToTxID(15): %v", err)
	}
	rec, err := sr.Next()
	if err != nil {
		t.Fatalf("Next after seek: %v", err)
	}
	if rec.TxID != 20 {
		t.Fatalf("landed on TxID %d, want 20", rec.TxID)
	}
}

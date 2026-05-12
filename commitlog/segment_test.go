package commitlog

import (
	"errors"
	"io"
	"os"
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

func TestSegmentWriterRejectsBootstrapStartTx(t *testing.T) {
	dir := t.TempDir()

	if writer, err := CreateSegment(dir, 0); err == nil {
		_ = writer.Close()
		t.Fatal("CreateSegment accepted bootstrap tx 0")
	} else if !errors.Is(err, ErrOpen) {
		t.Fatalf("CreateSegment error = %v, want ErrOpen category", err)
	}

	if writer, err := OpenSegmentForAppend(dir, 0); err == nil {
		_ = writer.Close()
		t.Fatal("OpenSegmentForAppend accepted bootstrap tx 0")
	} else if !errors.Is(err, ErrOpen) {
		t.Fatalf("OpenSegmentForAppend error = %v, want ErrOpen category", err)
	}
}

// Pin 14.
func TestSegmentReaderSeekToTxIDUsesIndex(t *testing.T) {
	dir := t.TempDir()
	entries := buildSegmentWithTxIDs(t, dir, []uint64{10, 11, 12, 13, 14})

	sparse := []OffsetIndexEntry{entries[0], entries[2], entries[4]} // {10, 12, 14}
	idx := populateSparseIndex(t, filepath.Join(dir, "00000000000000000010.idx"), 16, sparse)
	defer idx.Close()

	sr := openSegmentReader(t, dir, 10)
	defer sr.Close()

	if err := sr.SeekToTxID(13, idx); err != nil {
		t.Fatalf("SeekToTxID(13): %v", err)
	}
	rec, err := sr.Next()
	if err != nil {
		t.Fatalf("Next after seek: %v", err)
	}
	if rec.TxID != 13 {
		t.Fatalf("landed on TxID %d, want 13", rec.TxID)
	}
}

// Pin 15.
func TestSegmentReaderSeekToTxIDFallsBackWithoutIndex(t *testing.T) {
	dir := t.TempDir()
	buildSegmentWithTxIDs(t, dir, []uint64{10, 11, 12, 13, 14})

	sr := openSegmentReader(t, dir, 10)
	defer sr.Close()

	if err := sr.SeekToTxID(13, nil); err != nil {
		t.Fatalf("SeekToTxID(13, nil): %v", err)
	}
	rec, err := sr.Next()
	if err != nil {
		t.Fatalf("Next after seek: %v", err)
	}
	if rec.TxID != 13 {
		t.Fatalf("landed on TxID %d, want 13", rec.TxID)
	}

	// Exact hit: target present in segment.
	sr2 := openSegmentReader(t, dir, 10)
	defer sr2.Close()
	if err := sr2.SeekToTxID(12, nil); err != nil {
		t.Fatalf("SeekToTxID(12, nil): %v", err)
	}
	rec, err = sr2.Next()
	if err != nil {
		t.Fatalf("Next after seek: %v", err)
	}
	if rec.TxID != 12 {
		t.Fatalf("landed on TxID %d, want 12", rec.TxID)
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
	entries := buildSegmentWithTxIDs(t, dir, []uint64{10, 11, 12, 13, 14})

	// Populate the index starting at TxID 12 so KeyLookup(11) returns
	// ErrOffsetIndexKeyNotFound (11 is below the first indexed key).
	sparse := []OffsetIndexEntry{entries[2], entries[4]} // {12, 14}
	idx := populateSparseIndex(t, filepath.Join(dir, "00000000000000000010.idx"), 16, sparse)
	defer idx.Close()

	if _, _, err := idx.KeyLookup(11); !errors.Is(err, ErrOffsetIndexKeyNotFound) {
		t.Fatalf("precondition: KeyLookup(11) = %v, want ErrOffsetIndexKeyNotFound", err)
	}

	sr := openSegmentReader(t, dir, 10)
	defer sr.Close()

	if err := sr.SeekToTxID(11, idx); err != nil {
		t.Fatalf("SeekToTxID(11): %v", err)
	}
	rec, err := sr.Next()
	if err != nil {
		t.Fatalf("Next after seek: %v", err)
	}
	if rec.TxID != 11 {
		t.Fatalf("landed on TxID %d, want 11", rec.TxID)
	}
}

func TestSegmentReaderSeekToTxIDFallsBackOnInvalidIndexOffset(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset func(t *testing.T, dir string, entries []OffsetIndexEntry) uint64
	}{
		{
			name: "offset-points-at-different-record",
			offset: func(t *testing.T, dir string, entries []OffsetIndexEntry) uint64 {
				t.Helper()
				return entries[3].ByteOffset
			},
		},
		{
			name: "offset-before-segment-header",
			offset: func(t *testing.T, dir string, entries []OffsetIndexEntry) uint64 {
				t.Helper()
				return SegmentHeaderSize - 1
			},
		},
		{
			name: "offset-past-segment-eof",
			offset: func(t *testing.T, dir string, entries []OffsetIndexEntry) uint64 {
				t.Helper()
				info, err := os.Stat(filepath.Join(dir, SegmentFileName(10)))
				if err != nil {
					t.Fatal(err)
				}
				return uint64(info.Size()) + 1024
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			entries := buildSegmentWithTxIDs(t, dir, []uint64{10, 11, 12, 13, 14})

			idxPath := filepath.Join(dir, OffsetIndexFileName(10))
			mut, err := CreateOffsetIndex(idxPath, 4)
			if err != nil {
				t.Fatal(err)
			}
			if err := mut.Append(entries[1].TxID, tc.offset(t, dir, entries)); err != nil {
				_ = mut.Close()
				t.Fatal(err)
			}
			if err := mut.Close(); err != nil {
				t.Fatal(err)
			}
			idx, err := OpenOffsetIndex(idxPath)
			if err != nil {
				t.Fatal(err)
			}
			defer idx.Close()

			sr := openSegmentReader(t, dir, 10)
			defer sr.Close()

			if err := sr.SeekToTxID(12, idx); err != nil {
				t.Fatalf("SeekToTxID(12): %v", err)
			}
			rec, err := sr.Next()
			if err != nil {
				t.Fatalf("Next after seek: %v", err)
			}
			if rec.TxID != 12 {
				t.Fatalf("landed on TxID %d, want 12 after linear fallback", rec.TxID)
			}
		})
	}
}

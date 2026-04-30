package commitlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/shunter/types"
)

func mustCreate(t *testing.T, cap uint64) (*OffsetIndexMut, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")
	idx, err := CreateOffsetIndex(path, cap)
	if err != nil {
		t.Fatalf("CreateOffsetIndex: %v", err)
	}
	return idx, path
}

func TestCreateOffsetIndexRejectsOversizedCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")

	idx, err := CreateOffsetIndex(path, maxOffsetIndexCap+1)
	if err == nil {
		if idx != nil {
			_ = idx.Close()
		}
		t.Fatal("CreateOffsetIndex with oversized cap succeeded")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("CreateOffsetIndex error = %v, want too large", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("CreateOffsetIndex left file after oversized cap: stat error = %v", statErr)
	}
}

func TestOpenOffsetIndexMutRejectsOversizedCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := OpenOffsetIndexMut(path, maxOffsetIndexCap+1)
	if err == nil {
		if idx != nil {
			_ = idx.Close()
		}
		t.Fatal("OpenOffsetIndexMut with oversized cap succeeded")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("OpenOffsetIndexMut error = %v, want too large", err)
	}
}

func TestCreateOffsetIndexRejectsSymlinkWithoutTruncatingTarget(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "external.idx")
	before := []byte("external offset index bytes")
	if err := os.WriteFile(targetPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	linkPath := filepath.Join(dir, OffsetIndexFileName(1))
	symlinkOrSkip(t, targetPath, linkPath)

	idx, err := CreateOffsetIndex(linkPath, 4)
	if err == nil {
		_ = idx.Close()
		t.Fatal("expected symlink offset index path to fail creation")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("CreateOffsetIndex error = %v, want ErrOpen category", err)
	}
	after, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("symlink target changed: got %q want %q", after, before)
	}
}

func TestCreateOffsetIndexRejectsDirectoryArtifactWithoutRemoving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OffsetIndexFileName(1))
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	idx, err := CreateOffsetIndex(path, 4)
	if err == nil {
		_ = idx.Close()
		t.Fatal("expected directory offset index artifact to fail creation")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("CreateOffsetIndex error = %v, want ErrOpen category", err)
	}
	assertDirectoryArtifactExists(t, path)
}

func TestOpenOffsetIndexRejectsSymlink(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "external.idx")
	idx, err := CreateOffsetIndex(targetPath, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	linkPath := filepath.Join(dir, OffsetIndexFileName(1))
	symlinkOrSkip(t, targetPath, linkPath)

	ro, err := OpenOffsetIndex(linkPath)
	if err == nil {
		_ = ro.Close()
		t.Fatal("expected symlink offset index to fail read-only open")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("OpenOffsetIndex error = %v, want ErrOpen category", err)
	}
}

func TestOpenOffsetIndexRejectsDirectoryArtifactWithoutRemoving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OffsetIndexFileName(1))
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	idx, err := OpenOffsetIndex(path)
	if err == nil {
		_ = idx.Close()
		t.Fatal("expected directory offset index artifact to fail read-only open")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("OpenOffsetIndex error = %v, want ErrOpen category", err)
	}
	assertDirectoryArtifactExists(t, path)
}

func TestOpenOffsetIndexMutRejectsSymlinkWithoutExtendingTarget(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "external.idx")
	before := []byte("external")
	if err := os.WriteFile(targetPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	linkPath := filepath.Join(dir, OffsetIndexFileName(1))
	symlinkOrSkip(t, targetPath, linkPath)

	idx, err := OpenOffsetIndexMut(linkPath, 4)
	if err == nil {
		_ = idx.Close()
		t.Fatal("expected symlink offset index to fail writable open")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("OpenOffsetIndexMut error = %v, want ErrOpen category", err)
	}
	after, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("symlink target changed: got %q want %q", after, before)
	}
}

func TestOpenOffsetIndexMutRejectsDirectoryArtifactWithoutRemoving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OffsetIndexFileName(1))
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	idx, err := OpenOffsetIndexMut(path, 4)
	if err == nil {
		_ = idx.Close()
		t.Fatal("expected directory offset index artifact to fail writable open")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("OpenOffsetIndexMut error = %v, want ErrOpen category", err)
	}
	assertDirectoryArtifactExists(t, path)
}

// Pin 1.
func TestOffsetIndexAppendAndLookupExact(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	entries := []OffsetIndexEntry{
		{TxID: 10, ByteOffset: 100},
		{TxID: 20, ByteOffset: 200},
		{TxID: 30, ByteOffset: 300},
	}
	for _, e := range entries {
		if err := idx.Append(e.TxID, e.ByteOffset); err != nil {
			t.Fatalf("Append(%d,%d): %v", e.TxID, e.ByteOffset, err)
		}
	}
	for _, e := range entries {
		gotKey, gotVal, err := idx.KeyLookup(e.TxID)
		if err != nil {
			t.Fatalf("KeyLookup(%d): %v", e.TxID, err)
		}
		if gotKey != e.TxID || gotVal != e.ByteOffset {
			t.Fatalf("KeyLookup(%d): got (%d,%d) want (%d,%d)", e.TxID, gotKey, gotVal, e.TxID, e.ByteOffset)
		}
	}
}

// Pin 2.
func TestOffsetIndexLookupLargestLessOrEqual(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	must := func(txID types.TxID, bo uint64) {
		if err := idx.Append(txID, bo); err != nil {
			t.Fatalf("Append(%d,%d): %v", txID, bo, err)
		}
	}
	must(10, 100)
	must(30, 300)
	must(50, 500)

	for _, tc := range []struct {
		query            types.TxID
		wantKey, wantVal uint64
	}{
		{11, 10, 100},
		{29, 10, 100},
		{30, 30, 300},
		{31, 30, 300},
		{49, 30, 300},
		{50, 50, 500},
		{9999, 50, 500},
	} {
		gotKey, gotVal, err := idx.KeyLookup(tc.query)
		if err != nil {
			t.Fatalf("KeyLookup(%d): %v", tc.query, err)
		}
		if uint64(gotKey) != tc.wantKey || gotVal != tc.wantVal {
			t.Fatalf("KeyLookup(%d): got (%d,%d) want (%d,%d)", tc.query, gotKey, gotVal, tc.wantKey, tc.wantVal)
		}
	}
}

// Pin 3.
func TestOffsetIndexKeyNotFoundBelowFirst(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	if err := idx.Append(10, 100); err != nil {
		t.Fatalf("Append: %v", err)
	}
	_, _, err := idx.KeyLookup(9)
	if !errors.Is(err, ErrOffsetIndexKeyNotFound) {
		t.Fatalf("KeyLookup(9): want ErrOffsetIndexKeyNotFound, got %v", err)
	}
	_, _, err = idx.KeyLookup(1)
	if !errors.Is(err, ErrOffsetIndexKeyNotFound) {
		t.Fatalf("KeyLookup(1): want ErrOffsetIndexKeyNotFound, got %v", err)
	}
}

// Pin 4.
func TestOffsetIndexEmpty(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	for _, q := range []types.TxID{0, 1, 100, ^types.TxID(0)} {
		_, _, err := idx.KeyLookup(q)
		if !errors.Is(err, ErrOffsetIndexKeyNotFound) {
			t.Fatalf("KeyLookup(%d) on empty: want ErrOffsetIndexKeyNotFound, got %v", q, err)
		}
	}
	if got := idx.NumEntries(); got != 0 {
		t.Fatalf("NumEntries: got %d want 0", got)
	}
	entries, err := idx.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Entries: got %d want 0", len(entries))
	}
}

// Pin 5.
func TestOffsetIndexNonMonotonicAppendRejected(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	if err := idx.Append(10, 100); err != nil {
		t.Fatalf("Append(10): %v", err)
	}

	// equal key
	var nm *OffsetIndexNonMonotonicError
	err := idx.Append(10, 200)
	if !errors.As(err, &nm) {
		t.Fatalf("Append(10) dup: want OffsetIndexNonMonotonicError, got %v", err)
	}
	if nm.Last != 10 || nm.Got != 10 {
		t.Fatalf("Append(10) dup: Last/Got = %d/%d want 10/10", nm.Last, nm.Got)
	}

	// smaller key
	err = idx.Append(5, 50)
	if !errors.As(err, &nm) {
		t.Fatalf("Append(5): want OffsetIndexNonMonotonicError, got %v", err)
	}
	if nm.Last != 10 || nm.Got != 5 {
		t.Fatalf("Append(5): Last/Got = %d/%d want 10/5", nm.Last, nm.Got)
	}

	// zero key (reserved sentinel)
	err = idx.Append(0, 0)
	if !errors.As(err, &nm) {
		t.Fatalf("Append(0): want OffsetIndexNonMonotonicError, got %v", err)
	}
	if nm.Got != 0 {
		t.Fatalf("Append(0): Got = %d want 0", nm.Got)
	}

	// zero key on fresh index also rejected
	idx2, _ := mustCreate(t, 4)
	defer idx2.Close()
	err = idx2.Append(0, 0)
	if !errors.As(err, &nm) {
		t.Fatalf("Append(0) on empty: want OffsetIndexNonMonotonicError, got %v", err)
	}
}

// Pin 6.
func TestOffsetIndexAppendBeyondCap(t *testing.T) {
	idx, _ := mustCreate(t, 3)
	defer idx.Close()

	if err := idx.Append(1, 10); err != nil {
		t.Fatal(err)
	}
	if err := idx.Append(2, 20); err != nil {
		t.Fatal(err)
	}
	if err := idx.Append(3, 30); err != nil {
		t.Fatal(err)
	}
	if err := idx.Append(4, 40); !errors.Is(err, ErrOffsetIndexFull) {
		t.Fatalf("Append past cap: want ErrOffsetIndexFull, got %v", err)
	}
	if got := idx.NumEntries(); got != 3 {
		t.Fatalf("NumEntries: got %d want 3", got)
	}
}

// Pin 7. Reference semantics: truncate(target) drops every entry with
// key >= target. The target entry is absent afterwards; any entry strictly
// below target is retained and still reachable via KeyLookup.
func TestOffsetIndexTruncateAtExistingKey(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	for _, e := range []OffsetIndexEntry{
		{TxID: 10, ByteOffset: 100},
		{TxID: 20, ByteOffset: 200},
		{TxID: 30, ByteOffset: 300},
		{TxID: 40, ByteOffset: 400},
	} {
		if err := idx.Append(e.TxID, e.ByteOffset); err != nil {
			t.Fatal(err)
		}
	}

	if err := idx.Truncate(30); err != nil {
		t.Fatalf("Truncate(30): %v", err)
	}

	if got := idx.NumEntries(); got != 2 {
		t.Fatalf("NumEntries after truncate: got %d want 2", got)
	}
	entries, err := idx.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].TxID != 10 || entries[1].TxID != 20 {
		t.Fatalf("Entries after truncate: got %+v want [{10,100},{20,200}]", entries)
	}
	for _, e := range entries {
		if e.TxID == 30 || e.TxID == 40 {
			t.Fatalf("target or tail still present: %+v", entries)
		}
	}

	// KeyLookup(target-1 = 29) still succeeds on the surviving prefix.
	gotKey, gotVal, err := idx.KeyLookup(29)
	if err != nil {
		t.Fatalf("KeyLookup(29): %v", err)
	}
	if gotKey != 20 || gotVal != 200 {
		t.Fatalf("KeyLookup(29): got (%d,%d) want (20,200)", gotKey, gotVal)
	}
}

// Pin 8.
func TestOffsetIndexTruncateBelowFirstEmptiesIndex(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	for _, e := range []OffsetIndexEntry{
		{TxID: 10, ByteOffset: 100},
		{TxID: 20, ByteOffset: 200},
		{TxID: 30, ByteOffset: 300},
	} {
		if err := idx.Append(e.TxID, e.ByteOffset); err != nil {
			t.Fatal(err)
		}
	}

	if err := idx.Truncate(5); err != nil {
		t.Fatalf("Truncate(5): %v", err)
	}
	if got := idx.NumEntries(); got != 0 {
		t.Fatalf("NumEntries: got %d want 0", got)
	}
	entries, err := idx.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Entries: got %+v want []", entries)
	}
	// After emptying, append must accept any positive key again.
	if err := idx.Append(100, 1000); err != nil {
		t.Fatalf("Append(100) post-empty: %v", err)
	}
}

// Pin 9.
func TestOffsetIndexReopenRecoversNumEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")

	idx, err := CreateOffsetIndex(path, 32)
	if err != nil {
		t.Fatal(err)
	}
	entries := []OffsetIndexEntry{
		{TxID: 7, ByteOffset: 70},
		{TxID: 14, ByteOffset: 140},
		{TxID: 21, ByteOffset: 210},
		{TxID: 28, ByteOffset: 280},
		{TxID: 35, ByteOffset: 350},
	}
	for _, e := range entries {
		if err := idx.Append(e.TxID, e.ByteOffset); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOffsetIndexMut(path, 32)
	if err != nil {
		t.Fatalf("OpenOffsetIndexMut: %v", err)
	}
	defer reopened.Close()

	if got := reopened.NumEntries(); got != uint64(len(entries)) {
		t.Fatalf("NumEntries after reopen: got %d want %d", got, len(entries))
	}
	got, err := reopened.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(entries) {
		t.Fatalf("Entries len: got %d want %d", len(got), len(entries))
	}
	for i, e := range entries {
		if got[i].TxID != e.TxID || got[i].ByteOffset != e.ByteOffset {
			t.Fatalf("Entries[%d]: got %+v want %+v", i, got[i], e)
		}
	}

	// Read-only reopen yields the same set.
	ro, err := OpenOffsetIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if got := ro.NumEntries(); got != uint64(len(entries)) {
		t.Fatalf("OffsetIndex.NumEntries: got %d want %d", got, len(entries))
	}
}

// Pin 10. A partial trailing entry — key bytes zero, value bytes possibly
// non-zero — must stop the scan and leave the valid prefix intact.
func TestOffsetIndexPartialTailIsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")

	idx, err := CreateOffsetIndex(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	valid := []OffsetIndexEntry{
		{TxID: 2, ByteOffset: 20},
		{TxID: 4, ByteOffset: 40},
		{TxID: 6, ByteOffset: 60},
	}
	for _, e := range valid {
		if err := idx.Append(e.TxID, e.ByteOffset); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a partial write at entry index len(valid): write only the
	// value half (last 8 bytes), leaving the key half as zero sentinel.
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	var partial [8]byte
	binary.LittleEndian.PutUint64(partial[:], 0xDEADBEEF)
	partialOff := int64(uint64(len(valid))*OffsetIndexEntrySize + 8)
	if _, err := f.WriteAt(partial[:], partialOff); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOffsetIndexMut(path, 16)
	if err != nil {
		t.Fatalf("OpenOffsetIndexMut: %v", err)
	}
	defer reopened.Close()

	if got := reopened.NumEntries(); got != uint64(len(valid)) {
		t.Fatalf("NumEntries: got %d want %d", got, len(valid))
	}
	entries, err := reopened.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(valid) {
		t.Fatalf("Entries len: got %d want %d", len(entries), len(valid))
	}
	for i, e := range valid {
		if entries[i].TxID != e.TxID || entries[i].ByteOffset != e.ByteOffset {
			t.Fatalf("Entries[%d]: got %+v want %+v", i, entries[i], e)
		}
	}
}

func TestOffsetIndexKeyOnlyPartialTailIsIgnoredAndOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")

	idx, err := CreateOffsetIndex(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []OffsetIndexEntry{
		{TxID: 2, ByteOffset: 20},
		{TxID: 4, ByteOffset: 40},
	} {
		if err := idx.Append(e.TxID, e.ByteOffset); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	writeRawOffsetIndexEntry(t, path, 2, 6, 0)
	reopened, err := OpenOffsetIndexMut(path, 8)
	if err != nil {
		t.Fatalf("OpenOffsetIndexMut: %v", err)
	}
	if got := reopened.NumEntries(); got != 2 {
		t.Fatalf("NumEntries after partial key tail = %d, want 2", got)
	}
	if err := reopened.Append(6, 60); err != nil {
		t.Fatalf("Append over partial key tail: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	ro, err := OpenOffsetIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	entries, err := ro.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[2].TxID != 6 || entries[2].ByteOffset != 60 {
		t.Fatalf("entries after overwriting partial key tail = %+v, want tail {6,60}", entries)
	}
}

func TestOffsetIndexNonMonotonicTailIsIgnoredAndOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")

	idx, err := CreateOffsetIndex(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []OffsetIndexEntry{
		{TxID: 10, ByteOffset: 100},
		{TxID: 20, ByteOffset: 200},
	} {
		if err := idx.Append(e.TxID, e.ByteOffset); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	writeRawOffsetIndexEntry(t, path, 2, 15, 150)
	reopened, err := OpenOffsetIndexMut(path, 8)
	if err != nil {
		t.Fatalf("OpenOffsetIndexMut: %v", err)
	}
	if got := reopened.NumEntries(); got != 2 {
		t.Fatalf("NumEntries after non-monotonic tail = %d, want 2", got)
	}
	if err := reopened.Append(30, 300); err != nil {
		t.Fatalf("Append over non-monotonic tail: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	ro, err := OpenOffsetIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	entries, err := ro.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].TxID != 10 || entries[1].TxID != 20 || entries[2].TxID != 30 {
		t.Fatalf("entries after overwriting non-monotonic tail = %+v, want [10 20 30]", entries)
	}
	if entries[2].ByteOffset != 300 {
		t.Fatalf("overwritten tail byte offset = %d, want 300", entries[2].ByteOffset)
	}
}

func TestOffsetIndexFixedSeedAppendTruncateReopenModel(t *testing.T) {
	const capEntries = uint64(16)
	seeds := []uint64{0x1d0ff51de, 0x51deca11, 0xc0ffee17}

	for _, seed := range seeds {
		dir := t.TempDir()
		path := filepath.Join(dir, OffsetIndexFileName(1))
		idx, err := CreateOffsetIndex(path, capEntries)
		if err != nil {
			t.Fatalf("seed=%#x CreateOffsetIndex: %v", seed, err)
		}

		rng := offsetIndexSoakRand{state: seed}
		model := []OffsetIndexEntry{}
		trace := []string{}
		for op := 0; op < 96; op++ {
			switch rng.next(5) {
			case 0, 1, 2:
				txID := offsetIndexModelLastTxID(model) + types.TxID(rng.next(5)+1)
				byteOffset := uint64(SegmentHeaderSize) + rng.next(8192) + 1
				trace = appendOffsetIndexTrace(trace, fmt.Sprintf("append(%d,%d)", txID, byteOffset))
				err := idx.Append(txID, byteOffset)
				if uint64(len(model)) == capEntries {
					if !errors.Is(err, ErrOffsetIndexFull) {
						t.Fatalf("seed=%#x op=%d trace=%s Append full error = %v, want ErrOffsetIndexFull", seed, op, strings.Join(trace, " "), err)
					}
					break
				}
				if err != nil {
					t.Fatalf("seed=%#x op=%d trace=%s Append(%d,%d): %v", seed, op, strings.Join(trace, " "), txID, byteOffset, err)
				}
				model = append(model, OffsetIndexEntry{TxID: txID, ByteOffset: byteOffset})
			case 3:
				target := types.TxID(rng.next(uint64(offsetIndexModelLastTxID(model)) + 8))
				trace = appendOffsetIndexTrace(trace, fmt.Sprintf("truncate(%d)", target))
				if err := idx.Truncate(target); err != nil {
					t.Fatalf("seed=%#x op=%d trace=%s Truncate(%d): %v", seed, op, strings.Join(trace, " "), target, err)
				}
				model = truncateOffsetIndexModel(model, target)
			case 4:
				trace = appendOffsetIndexTrace(trace, "reopen")
				if err := idx.Close(); err != nil {
					t.Fatalf("seed=%#x op=%d trace=%s Close: %v", seed, op, strings.Join(trace, " "), err)
				}
				idx, err = OpenOffsetIndexMut(path, capEntries)
				if err != nil {
					t.Fatalf("seed=%#x op=%d trace=%s OpenOffsetIndexMut: %v", seed, op, strings.Join(trace, " "), err)
				}
			}
			assertOffsetIndexViewMatchesModel(t, idx, model, seed, op, trace)
		}
		if err := idx.Close(); err != nil {
			t.Fatalf("seed=%#x final Close: %v", seed, err)
		}

		ro, err := OpenOffsetIndex(path)
		if err != nil {
			t.Fatalf("seed=%#x OpenOffsetIndex final: %v", seed, err)
		}
		assertOffsetIndexViewMatchesModel(t, ro, model, seed, 96, appendOffsetIndexTrace(trace, "readonly-reopen"))
		if err := ro.Close(); err != nil {
			t.Fatalf("seed=%#x read-only Close: %v", seed, err)
		}
	}
}

func TestOffsetIndexEntriesDoesNotPreallocateClaimedCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := readOffsetIndexEntries(f, ^uint64(0)); err == nil {
		t.Fatal("expected read failure for impossible claimed entry count")
	}
}

func TestOffsetIndexWriteAtFullRejectsShortWrite(t *testing.T) {
	err := writeAtFull(shortWriteAtSink{}, make([]byte, OffsetIndexEntrySize), 0)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeAtFull short write error = %v, want io.ErrShortWrite", err)
	}
}

// Pin 11.
func TestOffsetIndexWriterCadenceHoldsCandidate(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	const interval uint64 = 1000
	w := NewOffsetIndexWriter(idx, interval)

	// Three commits of 200 bytes each — running total 600 < interval.
	for tx, bo := uint64(1), uint64(0); tx <= 3; tx++ {
		if err := w.AppendAfterCommit(types.TxID(tx), bo, 200); err != nil {
			t.Fatalf("AppendAfterCommit(%d): %v", tx, err)
		}
		bo += 200
	}

	if got := idx.NumEntries(); got != 0 {
		t.Fatalf("NumEntries: got %d want 0 (candidate retained, not flushed)", got)
	}
}

// Pin 12.
func TestOffsetIndexWriterCadenceFlushesOnSync(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	const interval uint64 = 1 << 20
	w := NewOffsetIndexWriter(idx, interval)

	if err := w.AppendAfterCommit(5, 500, 100); err != nil {
		t.Fatal(err)
	}
	if err := w.AppendAfterCommit(6, 600, 100); err != nil {
		t.Fatal(err)
	}

	if got := idx.NumEntries(); got != 0 {
		t.Fatalf("NumEntries pre-Sync: got %d want 0", got)
	}

	if err := w.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := idx.NumEntries(); got != 1 {
		t.Fatalf("NumEntries post-Sync: got %d want 1", got)
	}
	got, err := idx.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TxID != 5 || got[0].ByteOffset != 500 {
		t.Fatalf("post-Sync entries: got %+v want [{5,500}]", got)
	}

	// A second Sync with no pending candidate is a no-op on the count.
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync (no-op): %v", err)
	}
	if got := idx.NumEntries(); got != 1 {
		t.Fatalf("NumEntries after idempotent Sync: got %d want 1", got)
	}
}

// Pin 22. Entries written via AppendAfterCommit that crossed the cadence
// threshold and reached the backing file survive a writer Close without
// Sync. Simulates "crash before fsync": reader must yield the landed prefix
// and never error on partial-tail residue.
func TestOffsetIndexWriterSurvivesCrashBeforeFsync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00000000000000000001.idx")

	head, err := CreateOffsetIndex(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	const interval uint64 = 100
	w := NewOffsetIndexWriter(head, interval)

	// Feed six commits, each with recordLen=100 so every other commit
	// crosses the cadence threshold and flushes the prior candidate.
	commits := []struct {
		tx types.TxID
		bo uint64
	}{
		{10, 1000},
		{20, 2000},
		{30, 3000},
		{40, 4000},
		{50, 5000},
		{60, 6000},
	}
	for _, c := range commits {
		if err := w.AppendAfterCommit(c.tx, c.bo, interval); err != nil {
			t.Fatalf("AppendAfterCommit(%d): %v", c.tx, err)
		}
	}

	// Close the writer WITHOUT Sync. Any entries that physically reached
	// the backing file via WriteAt survive; the pending candidate does not.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ro, err := OpenOffsetIndex(path)
	if err != nil {
		t.Fatalf("OpenOffsetIndex after no-sync close: %v", err)
	}
	defer ro.Close()

	ents, err := ro.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(ents) == 0 {
		t.Fatal("expected at least one landed entry after partial writer lifecycle")
	}
	// Every landed entry must be monotonic and correspond to one of the
	// commits we fed in.
	seen := map[uint64]bool{}
	for _, c := range commits {
		seen[uint64(c.tx)] = true
	}
	var lastKey uint64
	for i, e := range ents {
		if !seen[uint64(e.TxID)] {
			t.Fatalf("entries[%d]: unexpected txID %d (not in commit set)", i, e.TxID)
		}
		if i > 0 && uint64(e.TxID) <= lastKey {
			t.Fatalf("entries not monotonic at %d: prev=%d cur=%d", i, lastKey, e.TxID)
		}
		lastKey = uint64(e.TxID)
	}
}

// Pin 13.
func TestOffsetIndexWriterCadenceAdvancesEarliestInWindow(t *testing.T) {
	idx, _ := mustCreate(t, 16)
	defer idx.Close()

	const interval uint64 = 1000
	w := NewOffsetIndexWriter(idx, interval)

	// Four sub-interval commits within one cadence window. Earliest
	// (tx=10) must win the candidate slot.
	commits := []struct {
		tx   types.TxID
		bo   uint64
		rlen uint64
	}{
		{10, 100, 200},
		{11, 300, 200},
		{12, 500, 200},
		{13, 700, 200},
	}
	for _, c := range commits {
		if err := w.AppendAfterCommit(c.tx, c.bo, c.rlen); err != nil {
			t.Fatalf("AppendAfterCommit(%d): %v", c.tx, err)
		}
	}

	if got := idx.NumEntries(); got != 0 {
		t.Fatalf("NumEntries mid-window: got %d want 0", got)
	}

	// Flush and confirm the earliest commit is the one persisted.
	if err := w.Sync(); err != nil {
		t.Fatal(err)
	}
	got, err := idx.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TxID != 10 || got[0].ByteOffset != 100 {
		t.Fatalf("post-Sync earliest-in-window: got %+v want [{10,100}]", got)
	}

	// Advance past threshold: next commit with large recordLen must flush
	// the then-current candidate (a new one since last Sync) and stash the
	// incoming as the next candidate.
	if err := w.AppendAfterCommit(20, 2000, 100); err != nil {
		t.Fatal(err)
	}
	if got := idx.NumEntries(); got != 1 {
		t.Fatalf("NumEntries after first post-Sync commit: got %d want 1", got)
	}
	if err := w.AppendAfterCommit(21, 2100, interval); err != nil {
		t.Fatal(err)
	}
	// After this call, running total 100 + 1000 >= interval, so candidate
	// (20, 2000) should have been flushed and (21, 2100) stashed.
	if got := idx.NumEntries(); got != 2 {
		t.Fatalf("NumEntries after interval-crossing commit: got %d want 2", got)
	}
	entries, err := idx.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if entries[1].TxID != 20 || entries[1].ByteOffset != 2000 {
		t.Fatalf("second flushed entry: got %+v want {20,2000}", entries[1])
	}
	if err := w.Sync(); err != nil {
		t.Fatal(err)
	}
	entries, _ = idx.Entries()
	if len(entries) != 3 || entries[2].TxID != 21 {
		t.Fatalf("post-final-Sync: got %+v want tail {21,2100}", entries)
	}
}

type shortWriteAtSink struct{}

func (shortWriteAtSink) WriteAt(p []byte, _ int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

type offsetIndexView interface {
	Entries() ([]OffsetIndexEntry, error)
	KeyLookup(types.TxID) (types.TxID, uint64, error)
	NumEntries() uint64
}

type offsetIndexSoakRand struct {
	state uint64
}

func (r *offsetIndexSoakRand) next(n uint64) uint64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	if n == 0 {
		return r.state
	}
	return r.state % n
}

func appendOffsetIndexTrace(trace []string, op string) []string {
	trace = append(trace, op)
	if len(trace) <= 32 {
		return trace
	}
	return trace[len(trace)-32:]
}

func offsetIndexModelLastTxID(model []OffsetIndexEntry) types.TxID {
	if len(model) == 0 {
		return 0
	}
	return model[len(model)-1].TxID
}

func truncateOffsetIndexModel(model []OffsetIndexEntry, target types.TxID) []OffsetIndexEntry {
	for i, entry := range model {
		if entry.TxID >= target {
			return model[:i]
		}
	}
	return model
}

func assertOffsetIndexViewMatchesModel(t *testing.T, view offsetIndexView, model []OffsetIndexEntry, seed uint64, op int, trace []string) {
	t.Helper()
	if got := view.NumEntries(); got != uint64(len(model)) {
		t.Fatalf("seed=%#x op=%d trace=%s NumEntries = %d, want %d", seed, op, strings.Join(trace, " "), got, len(model))
	}
	got, err := view.Entries()
	if err != nil {
		t.Fatalf("seed=%#x op=%d trace=%s Entries: %v", seed, op, strings.Join(trace, " "), err)
	}
	if len(got) != len(model) {
		t.Fatalf("seed=%#x op=%d trace=%s Entries len = %d, want %d; got=%+v want=%+v", seed, op, strings.Join(trace, " "), len(got), len(model), got, model)
	}
	for i, want := range model {
		if got[i] != want {
			t.Fatalf("seed=%#x op=%d trace=%s Entries[%d] = %+v, want %+v; got=%+v want=%+v", seed, op, strings.Join(trace, " "), i, got[i], want, got, model)
		}
	}

	queries := []types.TxID{0, 1, offsetIndexModelLastTxID(model) + 3}
	if len(model) > 0 {
		queries = append(queries, model[0].TxID-1, model[0].TxID, model[len(model)/2].TxID, model[len(model)-1].TxID)
	}
	for _, query := range queries {
		want, ok := offsetIndexModelLookup(model, query)
		gotTxID, gotByteOffset, err := view.KeyLookup(query)
		if !ok {
			if !errors.Is(err, ErrOffsetIndexKeyNotFound) {
				t.Fatalf("seed=%#x op=%d trace=%s KeyLookup(%d) error = %v, want ErrOffsetIndexKeyNotFound", seed, op, strings.Join(trace, " "), query, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("seed=%#x op=%d trace=%s KeyLookup(%d): %v", seed, op, strings.Join(trace, " "), query, err)
		}
		if gotTxID != want.TxID || gotByteOffset != want.ByteOffset {
			t.Fatalf("seed=%#x op=%d trace=%s KeyLookup(%d) = (%d,%d), want (%d,%d)", seed, op, strings.Join(trace, " "), query, gotTxID, gotByteOffset, want.TxID, want.ByteOffset)
		}
	}
}

func offsetIndexModelLookup(model []OffsetIndexEntry, target types.TxID) (OffsetIndexEntry, bool) {
	var out OffsetIndexEntry
	ok := false
	for _, entry := range model {
		if entry.TxID > target {
			break
		}
		out = entry
		ok = true
	}
	return out, ok
}

func writeRawOffsetIndexEntry(t testing.TB, path string, entryIdx uint64, txID uint64, byteOffset uint64) {
	t.Helper()
	var buf [OffsetIndexEntrySize]byte
	binary.LittleEndian.PutUint64(buf[offsetIndexKeyOff:], txID)
	binary.LittleEndian.PutUint64(buf[offsetIndexValOff:], byteOffset)
	f, err := os.OpenFile(path, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(buf[:], int64(entryIdx*OffsetIndexEntrySize)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

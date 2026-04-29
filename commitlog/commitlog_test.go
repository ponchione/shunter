package commitlog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// --- Segment header tests ---

func TestSegmentHeaderRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSegmentHeader(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != SegmentHeaderSize {
		t.Fatalf("header size = %d, want %d", buf.Len(), SegmentHeaderSize)
	}
	if err := ReadSegmentHeader(&buf); err != nil {
		t.Fatal(err)
	}
}

func TestSegmentHeaderBadMagic(t *testing.T) {
	err := ReadSegmentHeader(bytes.NewReader([]byte{0, 0, 0, 0, 1, 0, 0, 0}))
	if !errors.Is(err, ErrBadMagic) {
		t.Fatalf("expected ErrBadMagic, got %v", err)
	}
}

// --- Record encode/decode tests ---

func TestRecordRoundTrip(t *testing.T) {
	rec := &Record{TxID: 42, RecordType: RecordTypeChangeset, Payload: []byte("hello")}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRecord(&buf, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.TxID != 42 || string(got.Payload) != "hello" {
		t.Fatal("record round-trip mismatch")
	}
}

func TestRecordCRCDetectsCorruption(t *testing.T) {
	rec := &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("data")}
	var buf bytes.Buffer
	EncodeRecord(&buf, rec)
	data := buf.Bytes()
	data[len(data)-1] ^= 0xFF // flip last CRC byte
	_, err := DecodeRecord(bytes.NewReader(data), 0)
	if err == nil {
		t.Fatal("expected CRC error")
	}
	var crcErr *ChecksumMismatchError
	if !errors.As(err, &crcErr) {
		t.Fatalf("expected ChecksumMismatchError, got %T", err)
	}
}

// --- Segment file tests ---

func TestSegmentWriterReader(t *testing.T) {
	dir := t.TempDir()
	sw, err := CreateSegment(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		sw.Append(&Record{TxID: uint64(i + 1), RecordType: RecordTypeChangeset, Payload: []byte("x")})
	}
	sw.Close()

	sr, err := OpenSegment(filepath.Join(dir, SegmentFileName(1)))
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()

	count := 0
	for {
		_, err := sr.Next()
		if err != nil {
			break
		}
		count++
	}
	if count != 5 {
		t.Fatalf("read %d records, want 5", count)
	}
}

func TestOpenSegmentRejectsMalformedFilename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-segment.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSegmentHeader(f); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = OpenSegment(path)
	if err == nil {
		t.Fatal("expected malformed segment filename to fail")
	}
}

func TestOpenSegmentRejectsNonCanonicalFilename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSegmentHeader(f); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = OpenSegment(path)
	if err == nil {
		t.Fatal("expected non-canonical segment filename to fail")
	}
}

func TestOpenSegmentRejectsSymlink(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := makeScanTestSegment(t, targetDir, 1, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, SegmentFileName(1))
	symlinkOrSkip(t, targetPath, path)

	_, err := OpenSegment(path)
	if err == nil {
		t.Fatal("expected symlink segment to fail")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("OpenSegment error = %v, want ErrOpen category", err)
	}
}

func TestSegmentFileName(t *testing.T) {
	got := SegmentFileName(1)
	if got != "00000000000000000001.log" {
		t.Fatalf("SegmentFileName(1) = %q", got)
	}
}

func TestCreateSegmentRejectsSymlinkWithoutTruncatingTarget(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "external.log")
	before := []byte("external segment bytes")
	if err := os.WriteFile(targetPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	linkPath := filepath.Join(dir, SegmentFileName(1))
	symlinkOrSkip(t, targetPath, linkPath)

	sw, err := CreateSegment(dir, 1)
	if err == nil {
		_ = sw.Close()
		t.Fatal("expected symlink segment path to fail creation")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("CreateSegment error = %v, want ErrOpen category", err)
	}
	after, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("symlink target changed: got %q want %q", after, before)
	}
}

func TestCreateSegmentRejectsDirectoryArtifactWithoutRemoving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SegmentFileName(1))
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	sw, err := CreateSegment(dir, 1)
	if err == nil {
		_ = sw.Close()
		t.Fatal("expected directory segment artifact to fail creation")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("CreateSegment error = %v, want ErrOpen category", err)
	}
	assertDirectoryArtifactExists(t, path)
}

func TestOpenSegmentForAppendCorruptFirstRecordFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := makeScanTestSegment(t, dir, 1, 1)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	corruptScanTestRecordCRCByte(t, path, 0, 0)

	_, err = OpenSegmentForAppend(dir, 1)
	if err == nil {
		t.Fatal("expected corrupt-first-record reopen to fail")
	}
	var checksumErr *ChecksumMismatchError
	if !errors.As(err, &checksumErr) {
		t.Fatalf("expected checksum mismatch error, got %T (%v)", err, err)
	}
	after, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if after.Size() != before.Size() {
		t.Fatalf("segment size changed after failed reopen: before=%d after=%d", before.Size(), after.Size())
	}
}

func TestOpenSegmentForAppendRejectsSymlink(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := makeScanTestSegment(t, targetDir, 1, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, SegmentFileName(1))
	symlinkOrSkip(t, targetPath, path)

	sw, err := OpenSegmentForAppend(dir, 1)
	if err == nil {
		_ = sw.Close()
		t.Fatal("expected symlink segment reopen to fail")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("OpenSegmentForAppend error = %v, want ErrOpen category", err)
	}
}

func TestOpenSegmentForAppendRejectsDirectoryArtifactWithoutRemoving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SegmentFileName(1))
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	sw, err := OpenSegmentForAppend(dir, 1)
	if err == nil {
		_ = sw.Close()
		t.Fatal("expected directory segment artifact to fail append reopen")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("OpenSegmentForAppend error = %v, want ErrOpen category", err)
	}
	assertDirectoryArtifactExists(t, path)
}

func TestOpenSegmentForAppendFirstTxMismatchFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := makeManualScanTestSegment(t, dir, 1, 2)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = OpenSegmentForAppend(dir, 1)
	assertHistoryGap(t, err, 1, 2)
	after, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if after.Size() != before.Size() {
		t.Fatalf("segment size changed after failed reopen: before=%d after=%d", before.Size(), after.Size())
	}
}

func symlinkOrSkip(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
}

func assertDirectoryArtifactExists(t testing.TB, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("directory artifact missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("artifact mode = %s, want directory", info.Mode())
	}
}

func TestOpenSegmentForAppendInteriorHistoryGapFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := makeManualScanTestSegment(t, dir, 1, 1, 3)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = OpenSegmentForAppend(dir, 1)
	assertHistoryGap(t, err, 2, 3)
	after, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if after.Size() != before.Size() {
		t.Fatalf("segment size changed after failed reopen: before=%d after=%d", before.Size(), after.Size())
	}
}

func TestOpenSegmentForAppendStructuredRecordFaultAfterValidPrefixFailsClosed(t *testing.T) {
	cases := []struct {
		name      string
		record    Record
		assertErr func(*testing.T, error)
	}{
		{
			name:   "unknown-record-type",
			record: Record{TxID: 2, RecordType: RecordTypeChangeset + 1, Payload: []byte{0x02}},
			assertErr: func(t *testing.T, err error) {
				t.Helper()
				var typeErr *UnknownRecordTypeError
				if !errors.As(err, &typeErr) {
					t.Fatalf("OpenSegmentForAppend error = %T (%v), want UnknownRecordTypeError", err, err)
				}
				if typeErr.Type != RecordTypeChangeset+1 {
					t.Fatalf("unknown record type = %d, want %d", typeErr.Type, RecordTypeChangeset+1)
				}
			},
		},
		{
			name:   "bad-record-flags",
			record: Record{TxID: 2, RecordType: RecordTypeChangeset, Flags: 1, Payload: []byte{0x02}},
			assertErr: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, ErrBadFlags) {
					t.Fatalf("OpenSegmentForAppend error = %v, want ErrBadFlags", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := makeManualScanTestRecords(t, dir, 1,
				Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte{0x01}},
				tc.record,
			)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}

			_, err = OpenSegmentForAppend(dir, 1)
			if err == nil {
				t.Fatal("expected structured record fault to fail reopen")
			}
			tc.assertErr(t, err)
			after, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if after.Size() != before.Size() {
				t.Fatalf("segment size changed after failed reopen: before=%d after=%d", before.Size(), after.Size())
			}
		})
	}
}

func TestOpenSegmentForAppendTruncatesDamagedTailAfterValidPrefix(t *testing.T) {
	dir := t.TempDir()
	path := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	wantSize := int64(scanTestRecordOffset(t, path, 2))
	corruptScanTestRecordCRCByte(t, path, 2, 0)

	sw, err := OpenSegmentForAppend(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sw.lastTx != 2 {
		t.Fatalf("lastTx = %d, want 2", sw.lastTx)
	}
	if sw.size != wantSize {
		t.Fatalf("size = %d, want %d", sw.size, wantSize)
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != wantSize {
		t.Fatalf("truncated size = %d, want %d", info.Size(), wantSize)
	}
}

func TestOpenSegmentForAppendTruncatesZeroHeaderWithNonZeroTail(t *testing.T) {
	dir := t.TempDir()
	path := makeScanTestSegment(t, dir, 1, 1, 2)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	wantSize := before.Size()
	appendZeroHeaderNonZeroTail(t, path)

	sw, err := OpenSegmentForAppend(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sw.lastTx != 2 {
		t.Fatalf("lastTx = %d, want 2", sw.lastTx)
	}
	if sw.size != wantSize {
		t.Fatalf("size = %d, want %d", sw.size, wantSize)
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != wantSize {
		t.Fatalf("truncated size = %d, want %d", info.Size(), wantSize)
	}
}

// --- Changeset codec tests ---

func testSchema() (*schema.Engine, schema.SchemaRegistry) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "players",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "name", Type: types.KindString},
		},
	})
	e, _ := b.Build(schema.EngineOptions{})
	return e, e.Registry()
}

func TestChangesetCodecRoundTrip(t *testing.T) {
	_, reg := testSchema()
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts: []types.ProductValue{
					{types.NewUint64(1), types.NewString("alice")},
				},
			},
		},
	}

	data, err := EncodeChangeset(cs)
	if err != nil {
		t.Fatal(err)
	}

	got, err := DecodeChangeset(data, reg)
	if err != nil {
		t.Fatal(err)
	}

	tc := got.Tables[0]
	if tc == nil || len(tc.Inserts) != 1 {
		t.Fatal("expected 1 insert")
	}
	if tc.Inserts[0][1].AsString() != "alice" {
		t.Fatal("decoded row mismatch")
	}
}

// --- Durability worker tests ---

func TestDefaultCommitLogOptionsIncludesSnapshotInterval(t *testing.T) {
	opts := DefaultCommitLogOptions()
	if opts.SnapshotInterval != 0 {
		t.Fatalf("SnapshotInterval = %d, want 0", opts.SnapshotInterval)
	}
}

func TestDurabilityWorkerBasic(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	_ = reg

	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 16

	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}

	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts: []types.ProductValue{
					{types.NewUint64(1), types.NewString("alice")},
				},
			},
		},
	}

	dw.EnqueueCommitted(1, cs)
	finalTx, fatalErr := dw.Close()
	if fatalErr != nil {
		t.Fatalf("fatal error: %v", fatalErr)
	}
	if finalTx != 1 {
		t.Fatalf("final durable TxID = %d, want 1", finalTx)
	}

	// Verify segment file exists.
	entries, _ := os.ReadDir(dir)
	logs := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".log" {
			logs++
		}
	}
	if logs != 1 {
		t.Fatalf("expected 1 segment file, got %d (dir=%v)", logs, entries)
	}
}

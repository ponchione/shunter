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

func TestSegmentFileName(t *testing.T) {
	got := SegmentFileName(1)
	if got != "00000000000000000001.log" {
		t.Fatalf("SegmentFileName(1) = %q", got)
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
	if len(entries) != 1 {
		t.Fatalf("expected 1 segment file, got %d", len(entries))
	}
}

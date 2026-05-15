package commitlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
)

func TestCommitlogPublicAPIContractCompiles(t *testing.T) {
	var _ *ErrBadVersion
	var _ *ErrUnknownRecordType
	var _ *ErrChecksumMismatch
	var _ *ErrRecordTooLarge
	_ = (*SegmentReader).Next
	var _ func([]byte, schema.SchemaRegistry) (*store.Changeset, error) = DecodeChangeset
}

func TestSegmentReaderNextUsesDefaultMaxPayload(t *testing.T) {
	dir := t.TempDir()
	sw, err := CreateSegment(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, SegmentFileName(1))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tooLarge := DefaultCommitLogOptions().MaxRecordPayloadBytes + 1
	rec := &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: make([]byte, tooLarge)}
	if err := EncodeRecord(f, rec); err != nil {
		t.Fatal(err)
	}

	sr, err := OpenSegment(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	if _, err := sr.Next(); err == nil {
		t.Fatal("expected ErrRecordTooLarge from no-arg Next")
	}
}

func TestDecodeRecordUsesDefaultMaxPayloadBeforeReadingBody(t *testing.T) {
	tooLarge := DefaultCommitLogOptions().MaxRecordPayloadBytes + 1
	var header [RecordHeaderSize]byte
	binary.LittleEndian.PutUint64(header[:8], 1)
	header[8] = RecordTypeChangeset
	binary.LittleEndian.PutUint32(header[10:14], tooLarge)

	_, err := DecodeRecord(bytes.NewReader(header[:]), 0)
	var tooLargeErr *RecordTooLargeError
	if !errors.As(err, &tooLargeErr) {
		t.Fatalf("DecodeRecord err = %v, want RecordTooLargeError", err)
	}
	if tooLargeErr.Max != DefaultCommitLogOptions().MaxRecordPayloadBytes {
		t.Fatalf("max = %d, want %d", tooLargeErr.Max, DefaultCommitLogOptions().MaxRecordPayloadBytes)
	}
}

func TestSegmentScanUsesDefaultMaxPayloadBeforeReadingBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SegmentFileName(1))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := WriteSegmentHeader(f); err != nil {
		t.Fatal(err)
	}

	tooLarge := DefaultCommitLogOptions().MaxRecordPayloadBytes + 1
	var header [RecordHeaderSize]byte
	binary.LittleEndian.PutUint64(header[:8], 1)
	header[8] = RecordTypeChangeset
	binary.LittleEndian.PutUint32(header[10:14], tooLarge)
	if err := writeFull(f, header[:]); err != nil {
		t.Fatal(err)
	}
	end := int64(SegmentHeaderSize+RecordHeaderSize) + int64(tooLarge) + RecordCRCSize
	if err := f.Truncate(end); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(SegmentHeaderSize, io.SeekStart); err != nil {
		t.Fatal(err)
	}

	sr := &SegmentReader{file: f, startTx: 1}
	_, err = scanNextRecord(sr)
	var tooLargeErr *RecordTooLargeError
	if !errors.As(err, &tooLargeErr) {
		t.Fatalf("scanNextRecord err = %v, want RecordTooLargeError", err)
	}
	if tooLargeErr.Max != DefaultCommitLogOptions().MaxRecordPayloadBytes {
		t.Fatalf("max = %d, want %d", tooLargeErr.Max, DefaultCommitLogOptions().MaxRecordPayloadBytes)
	}
}

func TestDecodeChangesetUsesDefaultMaxRowBytes(t *testing.T) {
	_, reg := testSchema()
	tooLarge := DefaultCommitLogOptions().MaxRowBytes + 1
	data := []byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, byte(tooLarge), byte(tooLarge >> 8), byte(tooLarge >> 16), byte(tooLarge >> 24)}

	_, err := DecodeChangeset(data, reg)
	if err == nil {
		t.Fatal("expected ErrRowTooLarge from public DecodeChangeset")
	}
	var rowErr *RowTooLargeError
	if !errors.As(err, &rowErr) {
		t.Fatalf("expected RowTooLargeError, got %v", err)
	}
	if rowErr.Max != DefaultCommitLogOptions().MaxRowBytes {
		t.Fatalf("row max = %d, want %d", rowErr.Max, DefaultCommitLogOptions().MaxRowBytes)
	}
}

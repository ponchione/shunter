package commitlog

import (
	"errors"
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

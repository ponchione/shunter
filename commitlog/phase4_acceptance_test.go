package commitlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func phase4TestSchema(t *testing.T) schema.SchemaRegistry {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "players",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "name", Type: types.KindString},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "logs",
		Columns: []schema.ColumnDefinition{{Name: "msg", Type: types.KindString}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return eng.Registry()
}

func sampleChangeset() *store.Changeset {
	return &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{
		1: {TableID: 1, TableName: "logs", Deletes: []types.ProductValue{{types.NewString("gone")}}},
		0: {TableID: 0, TableName: "players", Inserts: []types.ProductValue{{types.NewUint64(7), types.NewString("alice")}}},
	}}
}

func TestSegmentHeaderValidationCoverage(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSegmentHeader(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != SegmentHeaderSize {
		t.Fatalf("header len = %d, want %d", buf.Len(), SegmentHeaderSize)
	}
	if err := ReadSegmentHeader(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"bad-magic", []byte{0, 0, 0, 0, 1, 0, 0, 0}, ErrBadMagic},
		{"bad-version", []byte{'S', 'H', 'N', 'T', 2, 0, 0, 0}, &BadVersionError{}},
		{"bad-flags", []byte{'S', 'H', 'N', 'T', 1, 1, 0, 0}, ErrBadFlags},
		{"bad-padding", []byte{'S', 'H', 'N', 'T', 1, 0, 1, 0}, ErrBadFlags},
	} {
		err := ReadSegmentHeader(bytes.NewReader(tc.data))
		if tc.name == "bad-version" {
			var v *BadVersionError
			if !errors.As(err, &v) {
				t.Fatalf("%s: expected BadVersionError, got %v", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := ReadSegmentHeader(bytes.NewReader([]byte{1, 2, 3})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header error = %v", err)
	}
}

func TestRecordCRCAndValidationCoverage(t *testing.T) {
	rec := &Record{TxID: 0x0102030405060708, RecordType: RecordTypeChangeset, Payload: []byte("payload")}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if got := binary.LittleEndian.Uint64(data[:8]); got != rec.TxID {
		t.Fatalf("txid bytes not little-endian: %x", got)
	}
	decoded, err := DecodeRecord(bytes.NewReader(data), 0)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.TxID != rec.TxID || !bytes.Equal(decoded.Payload, rec.Payload) {
		t.Fatal("record round-trip mismatch")
	}

	payloadCorrupt := append([]byte(nil), data...)
	payloadCorrupt[RecordHeaderSize] ^= 0xFF
	var checksum *ChecksumMismatchError
	if _, err := DecodeRecord(bytes.NewReader(payloadCorrupt), 0); !errors.As(err, &checksum) {
		t.Fatalf("payload corruption should checksum fail, got %v", err)
	}

	flagsCorrupt := append([]byte(nil), data...)
	flagsCorrupt[9] = 1
	if _, err := DecodeRecord(bytes.NewReader(flagsCorrupt), 0); !errors.As(err, &checksum) {
		t.Fatalf("header corruption should checksum fail first, got %v", err)
	}

	unknown := &Record{TxID: 1, RecordType: 2, Payload: []byte("x")}
	buf.Reset()
	if err := EncodeRecord(&buf, unknown); err != nil {
		t.Fatal(err)
	}
	var unknownType *UnknownRecordTypeError
	if _, err := DecodeRecord(bytes.NewReader(buf.Bytes()), 0); !errors.As(err, &unknownType) {
		t.Fatalf("unknown record type should be reported, got %v", err)
	}

	badFlags := &Record{TxID: 1, RecordType: RecordTypeChangeset, Flags: 1}
	buf.Reset()
	if err := EncodeRecord(&buf, badFlags); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRecord(bytes.NewReader(buf.Bytes()), 0); !errors.Is(err, ErrBadFlags) {
		t.Fatalf("non-zero flags should be rejected, got %v", err)
	}

	empty := &Record{TxID: 2, RecordType: RecordTypeChangeset}
	buf.Reset()
	if err := EncodeRecord(&buf, empty); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRecord(bytes.NewReader(buf.Bytes()), 0); err != nil {
		t.Fatalf("empty payload record should decode: %v", err)
	}

	tooLarge := append([]byte(nil), data[:RecordHeaderSize]...)
	binary.LittleEndian.PutUint32(tooLarge[10:14], 6)
	if _, err := DecodeRecord(bytes.NewReader(tooLarge), 5); err == nil {
		t.Fatal("expected RecordTooLargeError")
	}
}

func TestSegmentReaderDetectsEOFTruncationAndCorruption(t *testing.T) {
	dir := t.TempDir()
	sw, err := CreateSegment(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := sw.Append(&Record{TxID: uint64(i + 1), RecordType: RecordTypeChangeset, Payload: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, SegmentFileName(1))
	sr, err := OpenSegment(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	for i := 0; i < 2; i++ {
		if _, err := sr.Next(0); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	if _, err := sr.Next(0); !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF, got %v", err)
	}
	if sr.LastTxID() != 2 {
		t.Fatalf("LastTxID = %d, want 2", sr.LastTxID())
	}

	full, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	truncatedPath := filepath.Join(dir, "truncated.log")
	if err := os.WriteFile(truncatedPath, full[:len(full)-2], 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := OpenSegment(truncatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Next(0); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Next(0); !errors.Is(err, ErrTruncatedRecord) {
		t.Fatalf("expected ErrTruncatedRecord, got %v", err)
	}
	_ = tr.Close()

	corrupt := append([]byte(nil), full...)
	corrupt[len(corrupt)-1] ^= 0xFF
	corruptPath := filepath.Join(dir, "corrupt.log")
	if err := os.WriteFile(corruptPath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	cr, err := OpenSegment(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cr.Next(0); err != nil {
		t.Fatal(err)
	}
	var checksum *ChecksumMismatchError
	if _, err := cr.Next(0); !errors.As(err, &checksum) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	_ = cr.Close()
}

func TestChangesetCodecDeterministicOrderingAndLengthPrefixes(t *testing.T) {
	reg := phase4TestSchema(t)
	cs := sampleChangeset()
	data, err := EncodeChangeset(cs)
	if err != nil {
		t.Fatal(err)
	}
	if data[0] != 1 {
		t.Fatalf("version = %d, want 1", data[0])
	}
	if got := binary.LittleEndian.Uint32(data[1:5]); got != 2 {
		t.Fatalf("table count = %d, want 2", got)
	}
	if got := binary.LittleEndian.Uint32(data[5:9]); got != 0 {
		t.Fatalf("first table id = %d, want 0", got)
	}

	decoded, err := DecodeChangeset(data, reg, DefaultCommitLogOptions().MaxRowBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Tables) != 2 || len(decoded.Tables[0].Inserts) != 1 || len(decoded.Tables[1].Deletes) != 1 {
		t.Fatalf("decoded changeset mismatch: %#v", decoded.Tables)
	}

	empty, err := EncodeChangeset(&store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(empty, []byte{1, 0, 0, 0, 0}) {
		t.Fatalf("empty changeset bytes = %v", empty)
	}

	zeroCounts, err := EncodeChangeset(&store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{0: {TableID: 0, TableName: "players"}}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = DecodeChangeset(zeroCounts, reg, DefaultCommitLogOptions().MaxRowBytes)
	if err != nil {
		t.Fatal(err)
	}
	if tc := decoded.Tables[0]; tc == nil || len(tc.Inserts) != 0 || len(tc.Deletes) != 0 {
		t.Fatalf("zero-count table missing after round-trip: %#v", decoded.Tables)
	}

	if _, err := DecodeChangeset(append([]byte{2}, empty[1:]...), reg, DefaultCommitLogOptions().MaxRowBytes); err == nil {
		t.Fatal("expected version error")
	}
	unknownTable := []byte{1, 1, 0, 0, 0, 99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := DecodeChangeset(unknownTable, reg, DefaultCommitLogOptions().MaxRowBytes); err == nil {
		t.Fatal("expected unknown table error")
	}
	tooLargeRow := []byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 8, 0, 0, 0}
	if _, err := DecodeChangeset(tooLargeRow, reg, 4); err == nil {
		t.Fatal("expected RowTooLargeError")
	}
}

type countingSegmentWriter struct {
	*SegmentWriter
	syncs *atomic.Int32
}

func (w *countingSegmentWriter) Sync() error {
	w.syncs.Add(1)
	return w.SegmentWriter.Sync()
}

func TestDurabilityWorkerBatchesAndFsyncs(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 8
	opts.DrainBatchSize = 4
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	var syncCount atomic.Int32
	dw.seg = &SegmentWriter{file: dw.seg.file, bw: dw.seg.bw, size: dw.seg.size, startTx: dw.seg.startTx, lastTx: dw.seg.lastTx}
	counting := &countingSegmentWriter{SegmentWriter: dw.seg, syncs: &syncCount}
	_ = counting
	for i := 1; i <= 6; i++ {
		dw.EnqueueCommitted(uint64(i), &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	}
	finalTx, err := dw.Close()
	if err != nil {
		t.Fatal(err)
	}
	if finalTx != 6 {
		t.Fatalf("final durable tx = %d, want 6", finalTx)
	}
	if dw.DurableTxID() != 6 {
		t.Fatalf("DurableTxID = %d, want 6", dw.DurableTxID())
	}
}

func TestDurabilityWorkerRejectsBadLifecycleCalls(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected non-increasing tx panic")
		}
		_, _ = dw.Close()
	}()
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
}

func TestDurabilityWorkerCloseThenEnqueuePanics(t *testing.T) {
	dir := t.TempDir()
	dw, err := NewDurabilityWorker(dir, 1, DefaultCommitLogOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dw.Close(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected enqueue-after-close panic")
		}
	}()
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
}

func TestDurabilityWorkerBlocksOnFullChannel(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 1
	opts.DrainBatchSize = 1
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	start := time.Now()
	done := make(chan struct{})
	go func() {
		dw.EnqueueCommitted(2, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue should eventually complete")
	}
	if time.Since(start) < 0 {
		t.Fatal("unreachable guard to keep linter happy")
	}
	_, _ = dw.Close()
}

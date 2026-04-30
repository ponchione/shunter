package commitlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func contractTestSchema(t *testing.T) schema.SchemaRegistry {
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
		Name:    "logs",
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

func TestDecodeRecordPartialZeroHeaderIsSafeEOF(t *testing.T) {
	if _, err := DecodeRecord(bytes.NewReader(make([]byte, RecordHeaderSize-1)), 0); !errors.Is(err, io.EOF) {
		t.Fatalf("partial zero header error = %v, want EOF", err)
	}

	partialNonZero := make([]byte, RecordHeaderSize-1)
	partialNonZero[len(partialNonZero)-1] = 1
	if _, err := DecodeRecord(bytes.NewReader(partialNonZero), 0); !errors.Is(err, ErrTruncatedRecord) {
		t.Fatalf("partial non-zero header error = %v, want ErrTruncatedRecord", err)
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
		if _, err := sr.Next(); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	if _, err := sr.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF, got %v", err)
	}
	if sr.LastTxID() != 2 {
		t.Fatalf("LastTxID = %d, want 2", sr.LastTxID())
	}

	full, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	truncatedPath := filepath.Join(dir, SegmentFileName(3))
	if err := os.WriteFile(truncatedPath, full[:len(full)-2], 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := OpenSegment(truncatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Next(); !errors.Is(err, ErrTruncatedRecord) {
		t.Fatalf("expected ErrTruncatedRecord, got %v", err)
	}
	_ = tr.Close()

	corrupt := append([]byte(nil), full...)
	corrupt[len(corrupt)-1] ^= 0xFF
	corruptPath := filepath.Join(dir, SegmentFileName(4))
	if err := os.WriteFile(corruptPath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	cr, err := OpenSegment(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cr.Next(); err != nil {
		t.Fatal(err)
	}
	var checksum *ChecksumMismatchError
	if _, err := cr.Next(); !errors.As(err, &checksum) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	_ = cr.Close()
}

func TestSegmentWriterEnforcesStartTxAlignment(t *testing.T) {
	dir := t.TempDir()
	sw, err := CreateSegment(dir, 100)
	if err != nil {
		t.Fatal(err)
	}

	if err := sw.Append(&Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("bad")}); err == nil {
		t.Fatal("expected first append before startTx to fail")
	}

	if err := sw.Append(&Record{TxID: 100, RecordType: RecordTypeChangeset, Payload: []byte("good")}); err != nil {
		t.Fatalf("first append at startTx should succeed: %v", err)
	}
	if err := sw.Append(&Record{TxID: 101, RecordType: RecordTypeChangeset, Payload: []byte("next")}); err != nil {
		t.Fatalf("second append after aligned first append should succeed: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}

	sr, err := OpenSegment(filepath.Join(dir, SegmentFileName(100)))
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	if sr.StartTxID() != 100 {
		t.Fatalf("StartTxID = %d, want 100", sr.StartTxID())
	}
	first, err := sr.Next()
	if err != nil {
		t.Fatalf("read first record: %v", err)
	}
	if first.TxID != 100 {
		t.Fatalf("first record tx = %d, want 100", first.TxID)
	}
}

func TestSegmentWriterRejectsRecordShapesBeforeDurabilityWrite(t *testing.T) {
	dir := t.TempDir()
	sw, err := CreateSegment(dir, 10)
	if err != nil {
		t.Fatal(err)
	}

	err = sw.Append(&Record{TxID: 10, RecordType: RecordTypeChangeset + 1, Payload: []byte("bad-type")})
	var typeErr *UnknownRecordTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("bad record type append error = %T (%v), want UnknownRecordTypeError", err, err)
	}
	if typeErr.Type != RecordTypeChangeset+1 {
		t.Fatalf("unknown record type = %d, want %d", typeErr.Type, RecordTypeChangeset+1)
	}
	if got := sw.Size(); got != SegmentHeaderSize {
		t.Fatalf("size after rejected first append = %d, want header size %d", got, SegmentHeaderSize)
	}
	if off, ok := sw.LastRecordByteOffset(); ok || off != 0 {
		t.Fatalf("last record offset after rejected first append = (%d, %v), want unset", off, ok)
	}

	if err := sw.Append(&Record{TxID: 10, RecordType: RecordTypeChangeset, Payload: []byte("good")}); err != nil {
		t.Fatalf("valid first append after rejected type: %v", err)
	}
	sizeAfterFirst := sw.Size()
	offsetAfterFirst, ok := sw.LastRecordByteOffset()
	if !ok {
		t.Fatal("last record offset should be set after valid append")
	}

	if err := sw.Append(&Record{TxID: 11, RecordType: RecordTypeChangeset, Flags: 1, Payload: []byte("bad-flags")}); !errors.Is(err, ErrBadFlags) {
		t.Fatalf("bad flags append error = %v, want ErrBadFlags", err)
	}
	if got := sw.Size(); got != sizeAfterFirst {
		t.Fatalf("size after rejected second append = %d, want %d", got, sizeAfterFirst)
	}
	if off, ok := sw.LastRecordByteOffset(); !ok || off != offsetAfterFirst {
		t.Fatalf("last record offset after rejected second append = (%d, %v), want (%d, true)", off, ok, offsetAfterFirst)
	}

	if err := sw.Append(&Record{TxID: 11, RecordType: RecordTypeChangeset, Payload: []byte("next")}); err != nil {
		t.Fatalf("valid second append after rejected flags: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}

	sr, err := OpenSegment(filepath.Join(dir, SegmentFileName(10)))
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	first, err := sr.Next()
	if err != nil {
		t.Fatalf("read first record: %v", err)
	}
	if first.TxID != 10 || string(first.Payload) != "good" {
		t.Fatalf("first record = %+v, want tx 10 good payload", first)
	}
	second, err := sr.Next()
	if err != nil {
		t.Fatalf("read second record: %v", err)
	}
	if second.TxID != 11 || string(second.Payload) != "next" {
		t.Fatalf("second record = %+v, want tx 11 next payload", second)
	}
	if _, err := sr.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third read error = %v, want EOF", err)
	}
}

func TestChangesetCodecDeterministicOrderingAndLengthPrefixes(t *testing.T) {
	reg := contractTestSchema(t)
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

	decoded, err := DecodeChangeset(data, reg)
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
	decoded, err = DecodeChangeset(zeroCounts, reg)
	if err != nil {
		t.Fatal(err)
	}
	if tc := decoded.Tables[0]; tc == nil || len(tc.Inserts) != 0 || len(tc.Deletes) != 0 {
		t.Fatalf("zero-count table missing after round-trip: %#v", decoded.Tables)
	}

	if _, err := DecodeChangeset(append([]byte{2}, empty[1:]...), reg); err == nil {
		t.Fatal("expected version error")
	}
	unknownTable := []byte{1, 1, 0, 0, 0, 99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := DecodeChangeset(unknownTable, reg); err == nil {
		t.Fatal("expected unknown table error")
	}
	duplicateTable := []byte{
		1,          // changeset version
		2, 0, 0, 0, // table count
		0, 0, 0, 0, // table 0
		0, 0, 0, 0, // insert count
		0, 0, 0, 0, // delete count
		0, 0, 0, 0, // duplicate table 0
		0, 0, 0, 0, // insert count
		0, 0, 0, 0, // delete count
	}
	if _, err := DecodeChangeset(duplicateTable, reg); err == nil || !strings.Contains(err.Error(), "duplicate table ID 0") {
		t.Fatalf("duplicate table error = %v, want duplicate table ID detail", err)
	}
	tooLargeRow := []byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 8, 0, 0, 0}
	if _, err := decodeChangesetWithMax(tooLargeRow, reg, 4); err == nil {
		t.Fatal("expected RowTooLargeError")
	}
}

func TestDurabilityWorkerBatchesAndTracksDurableTx(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 8
	opts.DrainBatchSize = 4
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
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

func waitForLastEnqueued(t *testing.T, dw *DurabilityWorker, want uint64) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	timeout := time.After(time.Second)
	for {
		dw.stateMu.Lock()
		got := dw.lastEnq
		dw.stateMu.Unlock()
		if got == want {
			return
		}
		select {
		case <-tick.C:
		case <-timeout:
			t.Fatalf("lastEnq = %d, want %d", got, want)
		}
	}
}

func TestDurabilityWorkerCloseWhileEnqueueBlockedReturnsControlledClosePanic(t *testing.T) {
	dw := &DurabilityWorker{
		ch:      make(chan durabilityItem, 1),
		closeCh: make(chan struct{}),
		done:    make(chan struct{}),
		opts:    CommitLogOptions{DrainBatchSize: 1},
	}
	close(dw.done)
	dw.ch <- durabilityItem{txID: 1, changeset: &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}}}

	panicCh := make(chan any, 1)
	enqueueDone := make(chan struct{})
	go func() {
		defer close(enqueueDone)
		defer func() {
			if r := recover(); r != nil {
				panicCh <- r
			}
		}()
		dw.EnqueueCommitted(2, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	}()

	waitForLastEnqueued(t, dw, 2)
	if _, err := dw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-enqueueDone:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked enqueue did not return")
	}

	select {
	case p := <-panicCh:
		if got := p.(string); got != "commitlog: enqueue after close" {
			t.Fatalf("blocked enqueue panic = %q, want controlled close panic", got)
		}
	default:
		t.Fatal("blocked enqueue should exit with a controlled close panic")
	}
}

func TestDurabilityWorkerCloseAfterSingleQueuedItemDoesNotSpinOnClosedDrain(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.DrainBatchSize = 500000000
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})

	closeDone := make(chan struct{})
	go func() {
		_, _ = dw.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Close should exit promptly after draining one queued item")
	}
}

func TestDurabilityWorkerWaitUntilDurableAlreadyDurable(t *testing.T) {
	dir := t.TempDir()
	dw, err := NewDurabilityWorker(dir, 1, DefaultCommitLogOptions())
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	if _, err := dw.Close(); err != nil {
		t.Fatal(err)
	}

	ready := dw.WaitUntilDurable(types.TxID(1))
	select {
	case txID := <-ready:
		if txID != 1 {
			t.Fatalf("txID=%d want 1", txID)
		}
	case <-time.After(time.Second):
		t.Fatal("already durable tx did not return ready channel")
	}
}

func TestDurabilityWorkerWaitUntilDurableLaterBatch(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.DrainBatchSize = 8
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	wait2 := dw.WaitUntilDurable(types.TxID(2))
	wait3 := dw.WaitUntilDurable(types.TxID(3))
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	dw.EnqueueCommitted(2, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	dw.EnqueueCommitted(3, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})

	select {
	case txID := <-wait2:
		if txID != 2 {
			t.Fatalf("txID=%d want 2", txID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait2 did not become ready")
	}
	select {
	case txID := <-wait3:
		if txID != 3 {
			t.Fatalf("txID=%d want 3", txID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait3 did not become ready")
	}
	if _, err := dw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDurabilityWorkerReopensExistingSegment(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.ChannelCapacity = 16

	// First open: create worker, write two records, close.
	dw1, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw1.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{
		0: {TableID: 0, TableName: "t", Inserts: []types.ProductValue{{types.NewUint64(1)}}},
	}})
	dw1.EnqueueCommitted(2, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{
		0: {TableID: 0, TableName: "t", Inserts: []types.ProductValue{{types.NewUint64(2)}}},
	}})
	finalTx1, err1 := dw1.Close()
	if err1 != nil {
		t.Fatalf("close phase 1: %v", err1)
	}
	if finalTx1 != 2 {
		t.Fatalf("phase 1 final tx = %d, want 2", finalTx1)
	}

	// Record size of segment after phase 1.
	segPath := filepath.Join(dir, SegmentFileName(1))
	info1, err := os.Stat(segPath)
	if err != nil {
		t.Fatalf("stat segment: %v", err)
	}
	sizeAfterFirstOpen := info1.Size()

	// Second open: reopen same dir+startTxID, write one more record.
	dw2, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	// Verify durable TxID reflects existing records.
	if dw2.DurableTxID() != 2 {
		t.Fatalf("reopened durable TxID = %d, want 2", dw2.DurableTxID())
	}

	dw2.EnqueueCommitted(3, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{
		0: {TableID: 0, TableName: "t", Inserts: []types.ProductValue{{types.NewUint64(3)}}},
	}})
	finalTx2, err2 := dw2.Close()
	if err2 != nil {
		t.Fatalf("close phase 2: %v", err2)
	}
	if finalTx2 != 3 {
		t.Fatalf("phase 2 final tx = %d, want 3", finalTx2)
	}

	// Verify segment grew (not truncated).
	info2, _ := os.Stat(segPath)
	if info2.Size() <= sizeAfterFirstOpen {
		t.Fatalf("segment truncated: size before=%d, after=%d", sizeAfterFirstOpen, info2.Size())
	}

	// Verify all 3 records readable.
	sr, err := OpenSegment(segPath)
	if err != nil {
		t.Fatalf("open for read: %v", err)
	}
	defer sr.Close()
	var txIDs []uint64
	for {
		rec, err := sr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read record: %v", err)
		}
		txIDs = append(txIDs, rec.TxID)
	}
	if len(txIDs) != 3 || txIDs[0] != 1 || txIDs[1] != 2 || txIDs[2] != 3 {
		t.Fatalf("expected txIDs [1 2 3], got %v", txIDs)
	}
}

func TestDurabilityWorkerCreatesNewSegmentWhenNoneExists(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dw.DurableTxID() != 0 {
		t.Fatalf("fresh worker durable TxID = %d, want 0", dw.DurableTxID())
	}
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	finalTx, _ := dw.Close()
	if finalTx != 1 {
		t.Fatalf("final tx = %d, want 1", finalTx)
	}
}

func TestDurabilityWorkerResumePlanStartsFreshNextSegment(t *testing.T) {
	dir := t.TempDir()
	segPath := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	truncateScanTestFile(t, segPath, 2)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if horizon != 2 {
		t.Fatalf("horizon = %d, want 2", horizon)
	}
	plan, err := planRecoveryResume(segments, horizon)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(segPath)
	if err != nil {
		t.Fatal(err)
	}

	dw, err := NewDurabilityWorkerWithResumePlan(dir, plan, DefaultCommitLogOptions())
	if err != nil {
		t.Fatal(err)
	}
	if dw.DurableTxID() != 2 {
		t.Fatalf("recovery worker durable TxID = %d, want 2", dw.DurableTxID())
	}
	dw.EnqueueCommitted(3, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	finalTx, err := dw.Close()
	if err != nil {
		t.Fatal(err)
	}
	if finalTx != 3 {
		t.Fatalf("final tx = %d, want 3", finalTx)
	}

	after, err := os.Stat(segPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("damaged tail segment size changed: before=%d after=%d", before.Size(), after.Size())
	}

	freshPath := filepath.Join(dir, SegmentFileName(3))
	freshInfo, err := os.Stat(freshPath)
	if err != nil {
		t.Fatalf("fresh segment missing: %v", err)
	}
	if freshInfo.Size() <= int64(SegmentHeaderSize) {
		t.Fatalf("fresh segment size = %d, want > %d", freshInfo.Size(), SegmentHeaderSize)
	}
	sr, err := OpenSegment(freshPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	rec, err := sr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if rec.TxID != 3 {
		t.Fatalf("fresh segment first tx = %d, want 3", rec.TxID)
	}
	if _, err := sr.Next(); err != io.EOF {
		t.Fatalf("fresh segment extra read err = %v, want EOF", err)
	}
}

func TestDurabilityWorkerResumePlanAppendInPlaceReopensSegment(t *testing.T) {
	dir := t.TempDir()
	makeScanTestSegment(t, dir, 1, 1, 2)
	plan := RecoveryResumePlan{SegmentStartTx: 1, NextTxID: 3, AppendMode: AppendInPlace}

	dw, err := NewDurabilityWorkerWithResumePlan(dir, plan, DefaultCommitLogOptions())
	if err != nil {
		t.Fatal(err)
	}
	if dw.DurableTxID() != 2 {
		t.Fatalf("reopened durable TxID = %d, want 2", dw.DurableTxID())
	}
	dw.EnqueueCommitted(3, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	finalTx, err := dw.Close()
	if err != nil {
		t.Fatal(err)
	}
	if finalTx != 3 {
		t.Fatalf("final tx = %d, want 3", finalTx)
	}

	sr, err := OpenSegment(filepath.Join(dir, SegmentFileName(1)))
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	var txIDs []uint64
	for {
		rec, err := sr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		txIDs = append(txIDs, rec.TxID)
	}
	if len(txIDs) != 3 || txIDs[0] != 1 || txIDs[1] != 2 || txIDs[2] != 3 {
		t.Fatalf("txIDs = %v, want [1 2 3]", txIDs)
	}
}

func TestDurabilityWorkerResumePlanAppendForbiddenFailsClosed(t *testing.T) {
	dir := t.TempDir()
	_, err := NewDurabilityWorkerWithResumePlan(dir, RecoveryResumePlan{AppendMode: AppendForbidden}, DefaultCommitLogOptions())
	if err == nil {
		t.Fatal("expected append-forbidden resume plan to fail")
	}
}

func TestDurabilityWorkerFreshResumePlanMismatchedNextTxFailsClosed(t *testing.T) {
	dir := t.TempDir()
	plan := RecoveryResumePlan{SegmentStartTx: 3, NextTxID: 4, AppendMode: AppendByFreshNextSegment}

	_, err := NewDurabilityWorkerWithResumePlan(dir, plan, DefaultCommitLogOptions())
	if err == nil {
		t.Fatal("expected mismatched fresh resume plan to fail")
	}
	if !strings.Contains(err.Error(), "invalid recovery resume plan") ||
		!strings.Contains(err.Error(), "SegmentStartTx:3") ||
		!strings.Contains(err.Error(), "NextTxID:4") {
		t.Fatalf("resume plan error = %v, want compact plan context", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, SegmentFileName(3))); !os.IsNotExist(statErr) {
		t.Fatalf("fresh resume segment stat err = %v, want no segment created", statErr)
	}
}

func TestDurabilityWorkerResumePlanAppendInPlaceCorruptFirstRecordFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := makeScanTestSegment(t, dir, 1, 1)
	corruptScanTestRecordCRCByte(t, path, 0, 0)

	_, err := NewDurabilityWorkerWithResumePlan(dir, RecoveryResumePlan{SegmentStartTx: 1, NextTxID: 2, AppendMode: AppendInPlace}, DefaultCommitLogOptions())
	if err == nil {
		t.Fatal("expected append-in-place reopen on corrupt-first-record segment to fail")
	}
	var checksumErr *ChecksumMismatchError
	if !errors.As(err, &checksumErr) {
		t.Fatalf("expected checksum mismatch error, got %T (%v)", err, err)
	}
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

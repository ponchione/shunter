package commitlog

// wire-shape — wire-shape canonical-contract pin suite.
//
// These 33 pins latch the current Shunter on-disk wire format as a
// canonical contract. Any accidental drift in byte layout, constant
// value, CRC algorithm, or framing invariant is intended to fail one
// of these pins loudly. See docs/shunter-design-decisions.md#commitlog-record-shape
// for the divergence audit against reference SpacetimeDB.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// --- Segment-header layout pins ---

// Pin 1.
func TestWireShapeSegmentHeaderLayoutBytes(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSegmentHeader(&buf); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	want := []byte{'S', 'H', 'N', 'T', 0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("segment header bytes = %v, want %v", got, want)
	}
	if len(got) != 8 {
		t.Fatalf("segment header length = %d, want 8", len(got))
	}
}

// Pin 2.
func TestWireShapeSegmentHeaderSizeConstant(t *testing.T) {
	if SegmentHeaderSize != 8 {
		t.Fatalf("SegmentHeaderSize = %d, want 8", SegmentHeaderSize)
	}
}

// Pin 3.
func TestWireShapeSegmentHeaderMagicConstant(t *testing.T) {
	want := [4]byte{'S', 'H', 'N', 'T'}
	if SegmentMagic != want {
		t.Fatalf("SegmentMagic = %v, want %v", SegmentMagic, want)
	}
}

// Pin 4.
func TestWireShapeSegmentHeaderVersionConstant(t *testing.T) {
	if SegmentVersion != 1 {
		t.Fatalf("SegmentVersion = %d, want 1", SegmentVersion)
	}
}

// Pin 5.
func TestWireShapeSegmentHeaderRejectsNonMagicPrefix(t *testing.T) {
	raw := []byte{'T', 'H', 'N', 'T', SegmentVersion, 0, 0, 0}
	err := ReadSegmentHeader(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrBadMagic) {
		t.Fatalf("errors.Is(err, ErrBadMagic) = false: %v", err)
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false: %v", err)
	}
}

// Pin 6.
func TestWireShapeSegmentHeaderRejectsVersionMismatch(t *testing.T) {
	raw := []byte{'S', 'H', 'N', 'T', 0x02, 0, 0, 0}
	err := ReadSegmentHeader(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected error")
	}
	var bv *BadVersionError
	if !errors.As(err, &bv) {
		t.Fatalf("errors.As(*BadVersionError) = false: %v", err)
	}
	if bv.Got != 2 {
		t.Fatalf("BadVersionError.Got = %d, want 2", bv.Got)
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false: %v", err)
	}
}

// Pin 7.
//
// Note: the realized record-shape error taxonomy carries `ErrBadFlags` as
// a single-category leaf mapped to `ErrTraversal`, rather than the
// call-site split originally proposed in the 2β decision doc. Pin 7
// reflects the realized shape; a future slice could reintroduce the
// split without touching this pin as long as `ErrTraversal` is still
// reachable from this site.
func TestWireShapeSegmentHeaderRejectsNonZeroFlags(t *testing.T) {
	raw := []byte{'S', 'H', 'N', 'T', SegmentVersion, 0x01, 0, 0}
	err := ReadSegmentHeader(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrBadFlags) {
		t.Fatalf("errors.Is(err, ErrBadFlags) = false: %v", err)
	}
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false: %v", err)
	}
}

// Pin 8.
func TestWireShapeSegmentHeaderRejectsNonZeroPadding(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"byte6", []byte{'S', 'H', 'N', 'T', SegmentVersion, 0, 0x01, 0}},
		{"byte7", []byte{'S', 'H', 'N', 'T', SegmentVersion, 0, 0, 0x01}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ReadSegmentHeader(bytes.NewReader(tc.raw))
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrBadFlags) {
				t.Fatalf("errors.Is(err, ErrBadFlags) = false: %v", err)
			}
			if !errors.Is(err, ErrTraversal) {
				t.Fatalf("errors.Is(err, ErrTraversal) = false: %v", err)
			}
		})
	}
}

// --- Record-header layout pins ---

// Pin 9.
func TestWireShapeRecordHeaderLayoutBytes(t *testing.T) {
	rec := &Record{
		TxID:       0x0102030405060708,
		RecordType: RecordTypeChangeset,
		Flags:      0,
		Payload:    []byte{0xAA},
	}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if len(got) < RecordHeaderSize {
		t.Fatalf("encoded length %d < header size %d", len(got), RecordHeaderSize)
	}
	wantHeader := []byte{
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, // TxID LE
		0x01,                   // RecordType
		0x00,                   // Flags
		0x01, 0x00, 0x00, 0x00, // data_len LE = 1
	}
	if !bytes.Equal(got[:RecordHeaderSize], wantHeader) {
		t.Fatalf("record header bytes = %v, want %v", got[:RecordHeaderSize], wantHeader)
	}
}

// Pin 10.
func TestWireShapeRecordHeaderSizeConstant(t *testing.T) {
	if RecordHeaderSize != 14 {
		t.Fatalf("RecordHeaderSize = %d, want 14", RecordHeaderSize)
	}
}

// Pin 11.
func TestWireShapeRecordCRCSizeConstant(t *testing.T) {
	if RecordCRCSize != 4 {
		t.Fatalf("RecordCRCSize = %d, want 4", RecordCRCSize)
	}
}

// Pin 12.
func TestWireShapeRecordOverheadConstant(t *testing.T) {
	if RecordOverhead != 18 {
		t.Fatalf("RecordOverhead = %d, want 18", RecordOverhead)
	}
	if RecordOverhead != RecordHeaderSize+RecordCRCSize {
		t.Fatalf("RecordOverhead = %d, want header+crc = %d", RecordOverhead, RecordHeaderSize+RecordCRCSize)
	}
}

// Pin 13.
func TestWireShapeRecordTypeChangesetConstant(t *testing.T) {
	if RecordTypeChangeset != 1 {
		t.Fatalf("RecordTypeChangeset = %d, want 1", RecordTypeChangeset)
	}
}

// Pin 14.
func TestWireShapeEncodeRecordLittleEndianTxID(t *testing.T) {
	rec := &Record{TxID: 0xDEADBEEFCAFEBABE, RecordType: RecordTypeChangeset}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	got := binary.LittleEndian.Uint64(buf.Bytes()[0:8])
	if got != rec.TxID {
		t.Fatalf("decoded TxID = %x, want %x", got, rec.TxID)
	}
	var explicit [8]byte
	binary.LittleEndian.PutUint64(explicit[:], rec.TxID)
	if !bytes.Equal(buf.Bytes()[0:8], explicit[:]) {
		t.Fatalf("TxID bytes = %x, want %x", buf.Bytes()[0:8], explicit)
	}
}

// Pin 15.
func TestWireShapeEncodeRecordLittleEndianDataLen(t *testing.T) {
	payload := make([]byte, 0x01020304&0xFFFF) // 0x0304 bytes to keep test fast
	rec := &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: payload}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	got := binary.LittleEndian.Uint32(buf.Bytes()[10:14])
	if got != uint32(len(payload)) {
		t.Fatalf("decoded data_len = %d, want %d", got, len(payload))
	}
}

// Pin 16.
func TestWireShapeEncodeRecordLittleEndianCRC(t *testing.T) {
	rec := &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("hello")}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	tail := raw[len(raw)-RecordCRCSize:]
	got := binary.LittleEndian.Uint32(tail)
	want := ComputeRecordCRC(rec)
	if got != want {
		t.Fatalf("trailing CRC = %x, want %x (LE decode)", got, want)
	}
}

// --- CRC algorithm pins ---

// Pin 17.
func TestWireShapeRecordCRCIsCastagnoli(t *testing.T) {
	rec := &Record{TxID: 0x1122334455667788, RecordType: RecordTypeChangeset, Flags: 0, Payload: []byte{0xAA, 0xBB, 0xCC}}

	var header [RecordHeaderSize]byte
	binary.LittleEndian.PutUint64(header[:8], rec.TxID)
	header[8] = rec.RecordType
	header[9] = rec.Flags
	binary.LittleEndian.PutUint32(header[10:14], uint32(len(rec.Payload)))

	h := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	h.Write(header[:])
	h.Write(rec.Payload)
	want := h.Sum32()

	got := ComputeRecordCRC(rec)
	if got != want {
		t.Fatalf("ComputeRecordCRC = %x, want Castagnoli %x", got, want)
	}
}

// Pin 18.
func TestWireShapeRecordCRCScopeCoversHeaderAndPayload(t *testing.T) {
	base := &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte{0x11, 0x22, 0x33}}
	baseCRC := ComputeRecordCRC(base)

	// Mutate a payload byte.
	payloadMutated := &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte{0x11, 0x22, 0x99}}
	if ComputeRecordCRC(payloadMutated) == baseCRC {
		t.Fatal("CRC did not change when payload mutated — scope excludes payload")
	}

	// Mutate the header's TxID (covered byte 0-7).
	headerMutated := &Record{TxID: 2, RecordType: RecordTypeChangeset, Payload: []byte{0x11, 0x22, 0x33}}
	if ComputeRecordCRC(headerMutated) == baseCRC {
		t.Fatal("CRC did not change when header TxID mutated — scope excludes header")
	}
}

// Pin 19.
func TestWireShapeRecordCRCExcludesTrailingCRC(t *testing.T) {
	rec := &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte{0xFF}}
	crc := ComputeRecordCRC(rec)

	// Recompute over exactly the 14-byte header + 1-byte payload using the
	// exported Castagnoli table; assert the stored trailer is not an input.
	var header [RecordHeaderSize]byte
	binary.LittleEndian.PutUint64(header[:8], rec.TxID)
	header[8] = rec.RecordType
	header[9] = rec.Flags
	binary.LittleEndian.PutUint32(header[10:14], uint32(len(rec.Payload)))

	h := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	h.Write(header[:])
	h.Write(rec.Payload)
	preCRCOnly := h.Sum32()

	if crc != preCRCOnly {
		t.Fatalf("CRC computed over header+payload only = %x, but ComputeRecordCRC returned %x — scope mismatch", preCRCOnly, crc)
	}

	// Extending the computation by 4 trailing bytes (simulating
	// circular inclusion of the stored CRC) must yield a different
	// value — proving the stored CRC is not self-referential.
	var trailer [4]byte
	binary.LittleEndian.PutUint32(trailer[:], crc)
	h2 := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	h2.Write(header[:])
	h2.Write(rec.Payload)
	h2.Write(trailer[:])
	if h2.Sum32() == crc {
		t.Fatal("CRC-including-trailer matched stored CRC — suggests circular inclusion")
	}
}

// Pin 20.
func TestWireShapeDecodeRecordRejectsCRCFlip(t *testing.T) {
	rec := &Record{TxID: 77, RecordType: RecordTypeChangeset, Payload: []byte{0x01, 0x02, 0x03}}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	raw[len(raw)-1] ^= 0x01 // flip one bit in stored CRC

	_, err := DecodeRecord(bytes.NewReader(raw), 0)
	if err == nil {
		t.Fatal("expected error")
	}
	var cm *ChecksumMismatchError
	if !errors.As(err, &cm) {
		t.Fatalf("errors.As(*ChecksumMismatchError) = false: %v", err)
	}
	if cm.TxID != 77 {
		t.Fatalf("ChecksumMismatchError.TxID = %d, want 77", cm.TxID)
	}
	expected := binary.LittleEndian.Uint32(raw[len(raw)-RecordCRCSize:])
	if cm.Expected != expected {
		t.Fatalf("ChecksumMismatchError.Expected = %x, want %x", cm.Expected, expected)
	}
	if cm.Got != ComputeRecordCRC(rec) {
		t.Fatalf("ChecksumMismatchError.Got = %x, want %x", cm.Got, ComputeRecordCRC(rec))
	}
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false: %v", err)
	}
}

// --- Changeset payload layout pins ---

// Pin 21.
func TestWireShapeChangesetVersionConstant(t *testing.T) {
	if changesetVersion != 1 {
		t.Fatalf("changesetVersion = %d, want 1", changesetVersion)
	}
}

// Pin 22.
func TestWireShapeChangesetEmptyLayoutBytes(t *testing.T) {
	cs := &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}}
	got, err := EncodeChangeset(cs)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x01, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("empty changeset bytes = %v, want %v", got, want)
	}
}

// Pin 23.
func TestWireShapeChangesetSingleTableLayoutBytes(t *testing.T) {
	row := types.ProductValue{types.NewUint8(0xAA)}
	cs := &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{
		0: {TableID: 0, TableName: "t", Inserts: []types.ProductValue{row}},
	}}
	got, err := EncodeChangeset(cs)
	if err != nil {
		t.Fatal(err)
	}
	// Row BSATN bytes: tag(TagUint8)=2 + value=0xAA → 2 bytes.
	wantRow := []byte{bsatn.TagUint8, 0xAA}
	want := []byte{
		0x01,                   // changeset_version = 1
		0x01, 0x00, 0x00, 0x00, // table_count = 1
		0x00, 0x00, 0x00, 0x00, // table_id = 0
		0x01, 0x00, 0x00, 0x00, // insert_count = 1
		0x02, 0x00, 0x00, 0x00, // row_len = 2
		wantRow[0], wantRow[1], // row bytes
		0x00, 0x00, 0x00, 0x00, // delete_count = 0
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("single-table changeset bytes:\n got  %v\n want %v", got, want)
	}
}

// Pin 24.
func TestWireShapeChangesetTableOrderDeterministic(t *testing.T) {
	cs := &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{
		5: {TableID: 5, TableName: "t5"},
		2: {TableID: 2, TableName: "t2"},
		9: {TableID: 9, TableName: "t9"},
	}}

	first, err := EncodeChangeset(cs)
	if err != nil {
		t.Fatal(err)
	}
	// Re-encode several times; every run must produce identical bytes.
	for i := 0; i < 8; i++ {
		got, err := EncodeChangeset(cs)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("encode run %d diverged from first encode", i)
		}
	}

	// Parse table_id ordering directly from the emitted bytes.
	// Layout: [version(1)][table_count(4)][per-table: table_id(4) insert_count(4) ... delete_count(4)]
	// For empty tables: each table block is 12 bytes (table_id + insert_count=0 + delete_count=0).
	pos := 5 // skip version + table_count
	var ids []schema.TableID
	for i := 0; i < 3; i++ {
		id := schema.TableID(binary.LittleEndian.Uint32(first[pos:]))
		ids = append(ids, id)
		pos += 12 // table_id(4) + insert_count(4) + delete_count(4)
	}
	want := []schema.TableID{2, 5, 9}
	for i, id := range ids {
		if id != want[i] {
			t.Fatalf("table_id[%d] = %d, want %d (full ordering %v)", i, id, want[i], ids)
		}
	}
}

// Pin 25.
func TestWireShapeChangesetDecodeRejectsUnknownVersion(t *testing.T) {
	raw := []byte{0x02, 0x00, 0x00, 0x00, 0x00}
	_, err := DecodeChangeset(raw, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("unsupported changeset version")) {
		t.Fatalf("error text %q missing 'unsupported changeset version'", err.Error())
	}
}

// Pin 26.
func TestWireShapeChangesetDecodeRejectsRowTooLarge(t *testing.T) {
	reg := wireShapeSingleColumnRegistry(t)

	tableID := wireShapeLookupTableID(t, reg, "t")
	var buf bytes.Buffer
	buf.WriteByte(0x01) // version
	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], 1) // table_count
	buf.Write(scratch[:])
	binary.LittleEndian.PutUint32(scratch[:], uint32(tableID))
	buf.Write(scratch[:])
	binary.LittleEndian.PutUint32(scratch[:], 1) // insert_count
	buf.Write(scratch[:])
	binary.LittleEndian.PutUint32(scratch[:], 1_000_000) // row_len >> max
	buf.Write(scratch[:])

	_, err := decodeChangesetWithMax(buf.Bytes(), reg, 16)
	if err == nil {
		t.Fatal("expected error")
	}
	var rtl *RowTooLargeError
	if !errors.As(err, &rtl) {
		t.Fatalf("errors.As(*RowTooLargeError) = false: %v", err)
	}
	if rtl.Size != 1_000_000 || rtl.Max != 16 {
		t.Fatalf("RowTooLargeError = %+v, want Size=1000000 Max=16", rtl)
	}
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false: %v", err)
	}
}

// --- Divergence-from-reference pins (behavioral contract) ---

// Pin 27 — delta entry #8: Shunter has no epoch field in the record
// header. The 14-byte header (8 TxID + 1 RecordType + 1 Flags + 4
// data_len) leaves no room for a reference `epoch u64 LE`.
func TestWireShapeShunterHasNoEpochField(t *testing.T) {
	if RecordHeaderSize != 14 {
		t.Fatalf("RecordHeaderSize = %d, want 14 (no epoch field)", RecordHeaderSize)
	}
	// 8 (TxID) + 1 (RecordType) + 1 (Flags) + 4 (data_len) = 14.
	const (
		txIDFieldBytes    = 8
		recordTypeBytes   = 1
		flagsBytes        = 1
		dataLenFieldBytes = 4
	)
	sum := txIDFieldBytes + recordTypeBytes + flagsBytes + dataLenFieldBytes
	if sum != RecordHeaderSize {
		t.Fatalf("header field sum = %d, want %d (unexpected field introduced)", sum, RecordHeaderSize)
	}
}

// Pin 28 — delta entry #6: Shunter frames at record granularity, not
// commit granularity. Two successful appends produce two physical
// records in the segment file, not one grouped commit unit.
func TestWireShapeShunterHasNoCommitGrouping(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	dw, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(1, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	dw.EnqueueCommitted(2, &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}})
	if _, err := dw.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, SegmentFileName(1))
	sr, err := OpenSegment(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()

	var txIDs []uint64
	for {
		rec, err := sr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		txIDs = append(txIDs, rec.TxID)
	}
	if len(txIDs) != 2 {
		t.Fatalf("segment contains %d records, want 2 (one per TxID; no commit grouping)", len(txIDs))
	}
	if txIDs[0] != 1 || txIDs[1] != 2 {
		t.Fatalf("segment TxIDs = %v, want [1 2]", txIDs)
	}
}

// Pin 29 — delta entry #11: byte 8 of every record header is a typed
// record-type discriminator. Today only 1 (Changeset) is accepted.
func TestWireShapeShunterRecordTypeByteIsDiscriminator(t *testing.T) {
	rec := &Record{TxID: 1, RecordType: 99, Flags: 0, Payload: nil}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	_, err := DecodeRecord(bytes.NewReader(buf.Bytes()), 0)
	if err == nil {
		t.Fatal("expected error")
	}
	var urt *UnknownRecordTypeError
	if !errors.As(err, &urt) {
		t.Fatalf("errors.As(*UnknownRecordTypeError) = false: %v", err)
	}
	if urt.Type != 99 {
		t.Fatalf("UnknownRecordTypeError.Type = %d, want 99", urt.Type)
	}
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false: %v", err)
	}
}

// Pin 30 — delta entry #12: byte 9 of every record header is a
// reserved flags byte; any non-zero value is rejected at decode.
func TestWireShapeShunterRejectsNonZeroFlagsMidRecord(t *testing.T) {
	rec := &Record{TxID: 1, RecordType: RecordTypeChangeset, Flags: 0x01, Payload: nil}
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	_, err := DecodeRecord(bytes.NewReader(buf.Bytes()), 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrBadFlags) {
		t.Fatalf("errors.Is(err, ErrBadFlags) = false: %v", err)
	}
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false: %v", err)
	}
}

// Pin 31 — delta entries #5, #23: an all-zero record-header region is
// treated as end-of-stream. This lets recovery tolerate preallocated zero
// tails without classifying the tail as damaged user data.
func TestWireShapeShunterZeroRecordHeaderActsAsEOS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SegmentFileName(1))

	var buf bytes.Buffer
	if err := WriteSegmentHeader(&buf); err != nil {
		t.Fatal(err)
	}
	// 18 bytes of zero following the header: 14-byte header region
	// (TxID=0, RecordType=0, Flags=0, data_len=0) + 4-byte CRC region
	// (stored CRC=0). The zero header is the EOS sentinel; the trailing
	// zero CRC bytes are preallocated tail bytes and are ignored.
	buf.Write(make([]byte, 18))

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := scanOneSegment(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if info.LastTx != 0 {
		t.Fatalf("LastTx = %d, want 0 for empty preallocated segment", info.LastTx)
	}
	if info.AppendMode != AppendInPlace {
		t.Fatalf("AppendMode = %d, want %d", info.AppendMode, AppendInPlace)
	}
}

// --- Constants / structural pin ---

// Pin 32.
func TestWireShapeConstantsMatchBytes(t *testing.T) {
	// Segment header bytes emitted by pin 1 ≡ SegmentHeaderSize.
	var hbuf bytes.Buffer
	if err := WriteSegmentHeader(&hbuf); err != nil {
		t.Fatal(err)
	}
	if hbuf.Len() != SegmentHeaderSize {
		t.Fatalf("segment header emitted %d bytes, SegmentHeaderSize = %d", hbuf.Len(), SegmentHeaderSize)
	}

	// Record header bytes emitted by pin 9 ≡ RecordHeaderSize;
	// total encoded record overhead ≡ RecordOverhead for an
	// empty-payload record.
	rec := &Record{TxID: 1, RecordType: RecordTypeChangeset, Flags: 0, Payload: nil}
	var rbuf bytes.Buffer
	if err := EncodeRecord(&rbuf, rec); err != nil {
		t.Fatal(err)
	}
	if rbuf.Len() != RecordOverhead {
		t.Fatalf("empty-payload record encoded %d bytes, RecordOverhead = %d", rbuf.Len(), RecordOverhead)
	}
	if rbuf.Len() != RecordHeaderSize+RecordCRCSize {
		t.Fatalf("empty-payload record encoded %d bytes, RecordHeaderSize+RecordCRCSize = %d", rbuf.Len(), RecordHeaderSize+RecordCRCSize)
	}

	// And with a non-empty payload: header + payload + CRC.
	rec2 := &Record{TxID: 2, RecordType: RecordTypeChangeset, Payload: []byte{0x01, 0x02, 0x03, 0x04, 0x05}}
	var rbuf2 bytes.Buffer
	if err := EncodeRecord(&rbuf2, rec2); err != nil {
		t.Fatal(err)
	}
	want := RecordHeaderSize + len(rec2.Payload) + RecordCRCSize
	if rbuf2.Len() != want {
		t.Fatalf("record with 5-byte payload encoded %d, want %d", rbuf2.Len(), want)
	}
}

// --- Integration pin ---

// Pin 33 — encode a deterministic changeset, write it as a record,
// decode, re-encode, assert byte identity. Pins encode/decode as a
// bijection for the supported shape.
func TestWireShapeSegmentRoundTripByteIdenticalAfterEncodeDecode(t *testing.T) {
	reg := wireShapeSingleColumnRegistry(t)
	tableID := wireShapeLookupTableID(t, reg, "t")

	row := types.ProductValue{types.NewUint8(0x5A)}
	cs := &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{
		tableID: {TableID: tableID, TableName: "t", Inserts: []types.ProductValue{row}},
	}}

	first, err := EncodeChangeset(cs)
	if err != nil {
		t.Fatal(err)
	}

	// Wrap in a record, round-trip the record.
	rec := &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: first}
	var wbuf bytes.Buffer
	if err := EncodeRecord(&wbuf, rec); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRecord(bytes.NewReader(wbuf.Bytes()), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Payload, first) {
		t.Fatalf("record round-trip payload mismatch:\n got  %v\n want %v", decoded.Payload, first)
	}

	// Decode changeset, re-encode, assert bit-identical to original.
	cs2, err := DecodeChangeset(decoded.Payload, reg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeChangeset(cs2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("changeset encode/decode not byte-identical:\n first  %v\n second %v", first, second)
	}
}

// --- Test helpers ---

func wireShapeSingleColumnRegistry(t *testing.T) schema.SchemaRegistry {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "v", Type: types.KindUint8}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return eng.Registry()
}

func wireShapeLookupTableID(t *testing.T, reg schema.SchemaRegistry, name string) schema.TableID {
	t.Helper()
	for id := schema.TableID(0); id < 16; id++ {
		ts, ok := reg.Table(id)
		if !ok {
			continue
		}
		if ts.Name == name {
			return id
		}
	}
	t.Fatalf("table %q not found in registry", name)
	return 0
}

package commitlog

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanSegmentsThreeContiguous(t *testing.T) {
	dir := t.TempDir()

	makeScanTestSegment(t, dir, 1, 1, 2)
	makeScanTestSegment(t, dir, 3, 3, 4)
	makeScanTestSegment(t, dir, 5, 5, 6)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if horizon != 6 {
		t.Fatalf("horizon = %d, want 6", horizon)
	}
	if len(segments) != 3 {
		t.Fatalf("len(segments) = %d, want 3", len(segments))
	}

	assertSegmentInfo(t, segments[0], filepath.Join(dir, SegmentFileName(1)), 1, 2, true)
	assertSegmentInfo(t, segments[1], filepath.Join(dir, SegmentFileName(3)), 3, 4, true)
	assertSegmentInfo(t, segments[2], filepath.Join(dir, SegmentFileName(5)), 5, 6, true)
	if segments[0].AppendMode != AppendForbidden {
		t.Fatalf("first append mode = %d, want %d", segments[0].AppendMode, AppendForbidden)
	}
	if segments[1].AppendMode != AppendForbidden {
		t.Fatalf("middle append mode = %d, want %d", segments[1].AppendMode, AppendForbidden)
	}
	if segments[2].AppendMode != AppendInPlace {
		t.Fatalf("last append mode = %d, want %d", segments[2].AppendMode, AppendInPlace)
	}
}

func TestScanSegmentsEmptyDir(t *testing.T) {
	dir := t.TempDir()

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 0 {
		t.Fatalf("len(segments) = %d, want 0", len(segments))
	}
	if horizon != 0 {
		t.Fatalf("horizon = %d, want 0", horizon)
	}
}

func TestScanSegmentsSingleRecord(t *testing.T) {
	dir := t.TempDir()

	makeScanTestSegment(t, dir, 42, 42)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	assertSegmentInfo(t, segments[0], filepath.Join(dir, SegmentFileName(42)), 42, 42, true)
	if horizon != 42 {
		t.Fatalf("horizon = %d, want 42", horizon)
	}
	if segments[0].AppendMode != AppendInPlace {
		t.Fatalf("append mode = %d, want %d", segments[0].AppendMode, AppendInPlace)
	}
}

func TestScanSegmentsHistoryGapMissingMiddle(t *testing.T) {
	dir := t.TempDir()

	makeScanTestSegment(t, dir, 1, 1, 2)
	makeScanTestSegment(t, dir, 5, 5, 6)

	_, _, err := ScanSegments(dir)
	assertHistoryGap(t, err, 3, 5)
}

func TestScanSegmentsHistoryGapOverlap(t *testing.T) {
	dir := t.TempDir()

	makeScanTestSegment(t, dir, 1, 1, 2, 3)
	makeScanTestSegment(t, dir, 3, 3, 4)

	_, _, err := ScanSegments(dir)
	assertHistoryGap(t, err, 4, 3)
}

func TestScanSegmentsOutOfOrderTxID(t *testing.T) {
	dir := t.TempDir()

	makeManualScanTestSegment(t, dir, 1, 1, 3, 2)

	_, _, err := ScanSegments(dir)
	assertHistoryGap(t, err, 2, 3)
}

func TestScanSegmentsTruncatedTail(t *testing.T) {
	dir := t.TempDir()

	path := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	truncateScanTestFile(t, path, 2)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	assertSegmentInfo(t, segments[0], path, 1, 2, true)
	if horizon != 2 {
		t.Fatalf("horizon = %d, want 2", horizon)
	}
	if segments[0].AppendMode != AppendByFreshNextSegment {
		t.Fatalf("append mode = %d, want %d", segments[0].AppendMode, AppendByFreshNextSegment)
	}
}

func TestScanSegmentsCorruptSealedSegment(t *testing.T) {
	dir := t.TempDir()

	path := makeScanTestSegment(t, dir, 1, 1, 2)
	makeScanTestSegment(t, dir, 3, 3, 4)
	corruptScanTestByte(t, path, -1)

	_, _, err := ScanSegments(dir)
	if err == nil {
		t.Fatal("expected error for corrupt sealed segment")
	}
}

func TestScanSegmentsCorruptFirstRecordActiveSegmentClassifiesEmptyDamagedTail(t *testing.T) {
	dir := t.TempDir()

	path := makeScanTestSegment(t, dir, 7, 7)
	corruptScanTestByte(t, path, SegmentHeaderSize+RecordHeaderSize)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	assertSegmentInfo(t, segments[0], path, 7, 6, true)
	if horizon != 6 {
		t.Fatalf("horizon = %d, want 6", horizon)
	}
	if segments[0].AppendMode != AppendByFreshNextSegment {
		t.Fatalf("append mode = %d, want %d", segments[0].AppendMode, AppendByFreshNextSegment)
	}
}

func TestScanSegmentsZeroLengthActiveSegmentClassifiesEmptyDamagedTail(t *testing.T) {
	dir := t.TempDir()

	path := createZeroLengthSegment(t, dir, 7)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	assertSegmentInfo(t, segments[0], path, 7, 6, true)
	if horizon != 6 {
		t.Fatalf("horizon = %d, want 6", horizon)
	}
	if segments[0].AppendMode != AppendByFreshNextSegment {
		t.Fatalf("append mode = %d, want %d", segments[0].AppendMode, AppendByFreshNextSegment)
	}
}

func TestScanSegmentsZeroLengthRolloverRecoversValidPrefix(t *testing.T) {
	dir := t.TempDir()

	makeScanTestSegment(t, dir, 1, 1, 2)
	path := createZeroLengthSegment(t, dir, 3)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("len(segments) = %d, want 2", len(segments))
	}
	assertSegmentInfo(t, segments[0], filepath.Join(dir, SegmentFileName(1)), 1, 2, true)
	assertSegmentInfo(t, segments[1], path, 3, 2, true)
	if horizon != 2 {
		t.Fatalf("horizon = %d, want 2", horizon)
	}
	if segments[0].AppendMode != AppendForbidden {
		t.Fatalf("first append mode = %d, want %d", segments[0].AppendMode, AppendForbidden)
	}
	if segments[1].AppendMode != AppendByFreshNextSegment {
		t.Fatalf("last append mode = %d, want %d", segments[1].AppendMode, AppendByFreshNextSegment)
	}
}

func TestScanSegmentsZeroLengthSealedSegmentFailsLoudly(t *testing.T) {
	dir := t.TempDir()

	createZeroLengthSegment(t, dir, 1)
	makeScanTestSegment(t, dir, 2, 2)

	segments, horizon, err := ScanSegments(dir)
	if err == nil {
		t.Fatal("expected zero-length sealed segment to fail loudly")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ScanSegments error = %v, want io.EOF", err)
	}
	if len(segments) != 0 || horizon != 0 {
		t.Fatalf("partial scan = (%+v, %d), want no segments or horizon", segments, horizon)
	}
}

func TestScanSegmentsMalformedSegmentFilenameFailsLoudly(t *testing.T) {
	for _, tc := range []struct {
		name       string
		fileName   string
		wantDetail string
	}{
		{
			name:       "invalid",
			fileName:   "not-a-segment.log",
			wantDetail: `invalid segment filename "not-a-segment.log"`,
		},
		{
			name:       "non-canonical",
			fileName:   "1.log",
			wantDetail: `non-canonical segment filename "1.log"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tc.fileName)
			if err := os.WriteFile(path, []byte("not used"), 0o644); err != nil {
				t.Fatal(err)
			}

			segments, horizon, err := ScanSegments(dir)
			if err == nil {
				t.Fatal("expected malformed segment filename to fail loudly")
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("ScanSegments error = %v, want detail %q", err, tc.wantDetail)
			}
			if len(segments) != 0 || horizon != 0 {
				t.Fatalf("partial scan = (%+v, %d), want no segments or horizon", segments, horizon)
			}
		})
	}
}

func TestScanSegmentsCorruptActiveSegmentAfterValidPrefixUsesFreshNextSegment(t *testing.T) {
	dir := t.TempDir()

	path := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	corruptScanTestRecordPayloadByte(t, path, 2, 0)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	assertSegmentInfo(t, segments[0], path, 1, 2, true)
	if horizon != 2 {
		t.Fatalf("horizon = %d, want 2", horizon)
	}
	if segments[0].AppendMode != AppendByFreshNextSegment {
		t.Fatalf("append mode = %d, want %d", segments[0].AppendMode, AppendByFreshNextSegment)
	}
}

func TestScanSegmentsChecksumMismatchAfterValidPrefixUsesFreshNextSegment(t *testing.T) {
	dir := t.TempDir()

	path := makeScanTestSegment(t, dir, 1, 1, 2, 3)
	corruptScanTestRecordCRCByte(t, path, 2, 0)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	assertSegmentInfo(t, segments[0], path, 1, 2, true)
	if horizon != 2 {
		t.Fatalf("horizon = %d, want 2", horizon)
	}
	if segments[0].AppendMode != AppendByFreshNextSegment {
		t.Fatalf("append mode = %d, want %d", segments[0].AppendMode, AppendByFreshNextSegment)
	}
}

func TestScanSegmentsStructuredRecordFaultAfterValidPrefixFailsLoudly(t *testing.T) {
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
					t.Fatalf("ScanSegments error = %T (%v), want UnknownRecordTypeError", err, err)
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
					t.Fatalf("ScanSegments error = %v, want ErrBadFlags", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			makeManualScanTestRecords(t, dir, 1,
				Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte{0x01}},
				tc.record,
			)

			segments, horizon, err := ScanSegments(dir)
			if err == nil {
				t.Fatal("expected structured record fault to fail loudly")
			}
			if len(segments) != 0 || horizon != 0 {
				t.Fatalf("partial scan = (%+v, %d), want no segments or horizon", segments, horizon)
			}
			tc.assertErr(t, err)
		})
	}
}

func TestScanSegmentsZeroHeaderWithNonZeroTailIsDamagedTail(t *testing.T) {
	dir := t.TempDir()

	path := makeScanTestSegment(t, dir, 1, 1, 2)
	appendZeroHeaderNonZeroTail(t, path)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatalf("ScanSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	assertSegmentInfo(t, segments[0], path, 1, 2, true)
	if horizon != 2 {
		t.Fatalf("horizon = %d, want 2", horizon)
	}
	if segments[0].AppendMode != AppendByFreshNextSegment {
		t.Fatalf("append mode = %d, want %d", segments[0].AppendMode, AppendByFreshNextSegment)
	}
}

func makeScanTestSegment(t *testing.T, dir string, startTx uint64, txs ...uint64) string {
	t.Helper()

	sw, err := CreateSegment(dir, startTx)
	if err != nil {
		t.Fatalf("CreateSegment() error = %v", err)
	}
	for _, tx := range txs {
		if err := sw.Append(&Record{TxID: tx, RecordType: RecordTypeChangeset, Payload: []byte{byte(tx)}}); err != nil {
			t.Fatalf("Append(%d) error = %v", tx, err)
		}
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return filepath.Join(dir, SegmentFileName(startTx))
}

func createZeroLengthSegment(t *testing.T, dir string, startTx uint64) string {
	t.Helper()
	path := filepath.Join(dir, SegmentFileName(startTx))
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}

func makeManualScanTestSegment(t *testing.T, dir string, startTx uint64, txs ...uint64) string {
	t.Helper()
	records := make([]Record, 0, len(txs))
	for _, tx := range txs {
		records = append(records, Record{TxID: tx, RecordType: RecordTypeChangeset, Payload: []byte{byte(tx)}})
	}
	return makeManualScanTestRecords(t, dir, startTx, records...)
}

func makeManualScanTestRecords(t *testing.T, dir string, startTx uint64, records ...Record) string {
	t.Helper()

	path := filepath.Join(dir, SegmentFileName(startTx))
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	defer f.Close()

	if err := WriteSegmentHeader(f); err != nil {
		t.Fatalf("WriteSegmentHeader() error = %v", err)
	}
	for i := range records {
		if err := EncodeRecord(f, &records[i]); err != nil {
			t.Fatalf("EncodeRecord(%d) error = %v", records[i].TxID, err)
		}
	}
	return path
}

func appendZeroHeaderNonZeroTail(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(make([]byte, RecordHeaderSize)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{1}); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func truncateScanTestFile(t *testing.T, path string, trim int64) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	truncateScanTestFileToOffset(t, path, info.Size()-trim)
}

func truncateScanTestFileToOffset(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.Truncate(path, size); err != nil {
		t.Fatalf("os.Truncate() error = %v", err)
	}
}

func corruptScanTestByte(t *testing.T, path string, offset int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	idx := offset
	if idx < 0 {
		idx = len(data) + idx
	}
	if idx < 0 || idx >= len(data) {
		t.Fatalf("invalid corruption offset %d for file size %d", offset, len(data))
	}
	data[idx] ^= 0xff
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

func corruptScanTestRecordPayloadByte(t *testing.T, path string, recordIndex int, payloadOffset int) {
	t.Helper()
	corruptScanTestByte(t, path, scanTestRecordPayloadOffset(t, path, recordIndex, payloadOffset))
}

func corruptScanTestRecordCRCByte(t *testing.T, path string, recordIndex int, crcOffset int) {
	t.Helper()
	corruptScanTestByte(t, path, scanTestRecordCRCOffset(t, path, recordIndex, crcOffset))
}

func scanTestRecordPayloadOffset(t *testing.T, path string, recordIndex int, payloadOffset int) int {
	t.Helper()
	base := scanTestRecordOffset(t, path, recordIndex)
	return base + RecordHeaderSize + payloadOffset
}

func scanTestRecordCRCOffset(t *testing.T, path string, recordIndex int, crcOffset int) int {
	t.Helper()
	base := scanTestRecordOffset(t, path, recordIndex)
	payloadLen := scanTestRecordPayloadLength(t, path, recordIndex)
	return base + RecordHeaderSize + payloadLen + crcOffset
}

func scanTestRecordOffset(t *testing.T, path string, recordIndex int) int {
	t.Helper()
	if recordIndex < 0 {
		t.Fatalf("recordIndex must be >= 0, got %d", recordIndex)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	offset := SegmentHeaderSize
	for idx := 0; idx < recordIndex; idx++ {
		if offset+RecordHeaderSize > len(data) {
			t.Fatalf("record %d header out of bounds in %s", idx, path)
		}
		payloadLen := int(binary.LittleEndian.Uint32(data[offset+10 : offset+14]))
		offset += RecordOverhead + payloadLen
	}
	if offset+RecordHeaderSize > len(data) {
		t.Fatalf("record %d header out of bounds in %s", recordIndex, path)
	}
	return offset
}

func scanTestRecordPayloadLength(t *testing.T, path string, recordIndex int) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	offset := scanTestRecordOffset(t, path, recordIndex)
	return int(binary.LittleEndian.Uint32(data[offset+10 : offset+14]))
}

func assertHistoryGap(t *testing.T, err error, expected, got uint64) {
	t.Helper()

	if err == nil {
		t.Fatal("expected history gap error")
	}
	var gapErr *HistoryGapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("expected HistoryGapError, got %T (%v)", err, err)
	}
	if gapErr.Expected != expected || gapErr.Got != got {
		t.Fatalf("HistoryGapError = {Expected:%d Got:%d}, want {Expected:%d Got:%d}", gapErr.Expected, gapErr.Got, expected, got)
	}
}

func assertSegmentInfo(t *testing.T, got SegmentInfo, wantPath string, wantStart, wantLast uint64, wantValid bool) {
	t.Helper()

	if got.Path != wantPath {
		t.Fatalf("Path = %q, want %q", got.Path, wantPath)
	}
	if uint64(got.StartTx) != wantStart {
		t.Fatalf("StartTx = %d, want %d", got.StartTx, wantStart)
	}
	if uint64(got.LastTx) != wantLast {
		t.Fatalf("LastTx = %d, want %d", got.LastTx, wantLast)
	}
	if got.Valid != wantValid {
		t.Fatalf("Valid = %v, want %v", got.Valid, wantValid)
	}
}

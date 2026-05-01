package commitlog

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"

	"github.com/ponchione/shunter/types"
)

// Segment constants.
var SegmentMagic = [4]byte{'S', 'H', 'N', 'T'}

const (
	SegmentVersion    = 1
	SegmentHeaderSize = 8 // magic(4) + version(1) + flags(1) + padding(2)
)

// Record types.
const (
	RecordTypeChangeset byte = 1
)

const (
	RecordHeaderSize = 14 // tx_id(8) + record_type(1) + flags(1) + data_len(4)
	RecordCRCSize    = 4
	RecordOverhead   = RecordHeaderSize + RecordCRCSize
)

// Record is a single commit log entry.
type Record struct {
	TxID       uint64
	RecordType byte
	Flags      byte
	Payload    []byte
}

// WriteSegmentHeader writes an 8-byte segment header.
func WriteSegmentHeader(w io.Writer) error {
	var buf [SegmentHeaderSize]byte
	copy(buf[:4], SegmentMagic[:])
	buf[4] = SegmentVersion
	// buf[5] = flags (0)
	// buf[6:8] = padding (0)
	return writeFull(w, buf[:])
}

// ReadSegmentHeader validates an 8-byte segment header.
func ReadSegmentHeader(r io.Reader) error {
	var buf [SegmentHeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	if buf[0] != SegmentMagic[0] || buf[1] != SegmentMagic[1] ||
		buf[2] != SegmentMagic[2] || buf[3] != SegmentMagic[3] {
		return ErrBadMagic
	}
	if buf[4] != SegmentVersion {
		return &BadVersionError{Got: buf[4]}
	}
	if buf[5] != 0 || buf[6] != 0 || buf[7] != 0 {
		return ErrBadFlags
	}
	return nil
}

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// ComputeRecordCRC computes CRC32C over the record header + payload.
func ComputeRecordCRC(rec *Record) uint32 {
	h := crc32.New(crc32cTable)
	var buf [RecordHeaderSize]byte
	binary.LittleEndian.PutUint64(buf[:8], rec.TxID)
	buf[8] = rec.RecordType
	buf[9] = rec.Flags
	binary.LittleEndian.PutUint32(buf[10:14], uint32(len(rec.Payload)))
	h.Write(buf[:])
	h.Write(rec.Payload)
	return h.Sum32()
}

// EncodeRecord writes a record with CRC.
func EncodeRecord(w io.Writer, rec *Record) error {
	var buf [RecordHeaderSize]byte
	binary.LittleEndian.PutUint64(buf[:8], rec.TxID)
	buf[8] = rec.RecordType
	buf[9] = rec.Flags
	binary.LittleEndian.PutUint32(buf[10:14], uint32(len(rec.Payload)))
	if err := writeFull(w, buf[:]); err != nil {
		return err
	}
	if err := writeFull(w, rec.Payload); err != nil {
		return err
	}
	crc := ComputeRecordCRC(rec)
	var crcBuf [4]byte
	binary.LittleEndian.PutUint32(crcBuf[:], crc)
	return writeFull(w, crcBuf[:])
}

func writeFull(w io.Writer, p []byte) error {
	if len(p) == 0 {
		return nil
	}
	n, err := w.Write(p)
	if err != nil {
		return err
	}
	if n != len(p) {
		return io.ErrShortWrite
	}
	return nil
}

func allBytesZero(p []byte) bool {
	for _, b := range p {
		if b != 0 {
			return false
		}
	}
	return true
}

func isZeroRecordHeader(buf [RecordHeaderSize]byte) bool {
	return allBytesZero(buf[:])
}

// DecodeRecord reads and validates a record. An all-zero full or partial
// record header is treated as an end-of-stream sentinel so preallocated zero
// tails are ignored during recovery/replay instead of surfacing as corrupt
// records.
func DecodeRecord(r io.Reader, maxPayload uint32) (*Record, error) {
	var buf [RecordHeaderSize]byte
	if n, err := io.ReadFull(r, buf[:]); err != nil {
		if err == io.ErrUnexpectedEOF {
			if allBytesZero(buf[:n]) {
				return nil, io.EOF
			}
			return nil, ErrTruncatedRecord
		}
		return nil, err
	}
	if isZeroRecordHeader(buf) {
		return nil, io.EOF
	}

	rec := &Record{
		TxID:       binary.LittleEndian.Uint64(buf[:8]),
		RecordType: buf[8],
		Flags:      buf[9],
	}
	dataLen := binary.LittleEndian.Uint32(buf[10:14])

	if maxPayload > 0 && dataLen > maxPayload {
		return nil, &RecordTooLargeError{Size: dataLen, Max: maxPayload}
	}

	rec.Payload = make([]byte, dataLen)
	if _, err := io.ReadFull(r, rec.Payload); err != nil {
		if err == io.ErrUnexpectedEOF {
			return nil, ErrTruncatedRecord
		}
		return nil, err
	}

	var crcBuf [4]byte
	if _, err := io.ReadFull(r, crcBuf[:]); err != nil {
		if err == io.ErrUnexpectedEOF {
			return nil, ErrTruncatedRecord
		}
		return nil, err
	}

	expectedCRC := binary.LittleEndian.Uint32(crcBuf[:])
	actualCRC := ComputeRecordCRC(rec)
	if expectedCRC != actualCRC {
		return nil, &ChecksumMismatchError{Expected: expectedCRC, Got: actualCRC, TxID: rec.TxID}
	}
	if rec.RecordType != RecordTypeChangeset {
		return nil, &UnknownRecordTypeError{Type: rec.RecordType}
	}
	if rec.Flags != 0 {
		return nil, ErrBadFlags
	}

	return rec, nil
}

// SegmentFileName returns the log filename for a starting TxID.
func SegmentFileName(startTxID uint64) string {
	return fmt.Sprintf("%020d.log", startTxID)
}

// SegmentWriter writes records to a segment file.
type SegmentWriter struct {
	file             *os.File
	bw               *bufio.Writer
	size             int64
	startTx          uint64
	lastTx           uint64
	lastRecordOffset int64
	hasLastRecord    bool
}

// CreateSegment creates a new segment file.
func CreateSegment(dir string, startTxID uint64) (*SegmentWriter, error) {
	path := filepath.Join(dir, SegmentFileName(startTxID))
	if err := rejectBootstrapSegmentStart(startTxID, path); err != nil {
		return nil, err
	}
	if err := requireCreatableSegmentPath(path); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriter(f)
	if err := WriteSegmentHeader(bw); err != nil {
		f.Close()
		return nil, err
	}
	return &SegmentWriter{
		file:    f,
		bw:      bw,
		size:    SegmentHeaderSize,
		startTx: startTxID,
	}, nil
}

func requireCreatableSegmentPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: segment file %s is not a regular file", ErrOpen, path)
	}
	return nil
}

// OpenSegmentForAppend opens an existing segment for appending.
// It validates the header, scans all valid records to find the write position
// and last TxID, truncates any partial trailing record, and returns a writer
// positioned at the end.
func OpenSegmentForAppend(dir string, startTxID uint64) (*SegmentWriter, error) {
	path := filepath.Join(dir, SegmentFileName(startTxID))
	if err := rejectBootstrapSegmentStart(startTxID, path); err != nil {
		return nil, err
	}
	if err := requireRegularSegmentFile(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	if err := ReadSegmentHeader(f); err != nil {
		f.Close()
		return nil, err
	}

	size := int64(SegmentHeaderSize)
	var lastTx uint64
	var lastRecordOffset int64
	var recordCount int
	reader := &SegmentReader{file: f, startTx: startTxID}

	// Scan forward through valid records.
	for {
		recordStart := size
		rec, err := scanNextRecord(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if recordCount == 0 || !isDamagedTailError(err) {
				f.Close()
				return nil, err
			}
			// Partial/corrupt tail after a valid prefix — truncate to last good position.
			if truncErr := f.Truncate(size); truncErr != nil {
				f.Close()
				return nil, truncErr
			}
			break
		}
		if recordCount == 0 {
			if rec.TxID != startTxID {
				f.Close()
				return nil, &HistoryGapError{
					Expected: startTxID,
					Got:      rec.TxID,
					Segment:  path,
				}
			}
		} else if rec.TxID != lastTx+1 {
			f.Close()
			return nil, &HistoryGapError{
				Expected: lastTx + 1,
				Got:      rec.TxID,
				Segment:  path,
			}
		}
		size += int64(RecordOverhead + len(rec.Payload))
		lastTx = rec.TxID
		lastRecordOffset = recordStart
		recordCount++
	}

	// Seek to write position.
	if _, err := f.Seek(size, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}

	return &SegmentWriter{
		file:             f,
		bw:               bufio.NewWriter(f),
		size:             size,
		startTx:          startTxID,
		lastTx:           lastTx,
		lastRecordOffset: lastRecordOffset,
		hasLastRecord:    recordCount > 0,
	}, nil
}

// Append writes a record. TxID must be monotonically increasing.
func (sw *SegmentWriter) Append(rec *Record) error {
	if rec.RecordType != RecordTypeChangeset {
		return &UnknownRecordTypeError{Type: rec.RecordType}
	}
	if rec.Flags != 0 {
		return ErrBadFlags
	}
	if sw.lastTx == 0 {
		if rec.TxID != sw.startTx {
			return fmt.Errorf("commitlog: first tx_id %d must equal segment start %d", rec.TxID, sw.startTx)
		}
	} else if rec.TxID <= sw.lastTx {
		return fmt.Errorf("commitlog: tx_id %d not > last %d", rec.TxID, sw.lastTx)
	}
	byteOffset := sw.size
	if err := EncodeRecord(sw.bw, rec); err != nil {
		return err
	}
	sw.size += int64(RecordOverhead + len(rec.Payload))
	sw.lastTx = rec.TxID
	sw.lastRecordOffset = byteOffset
	sw.hasLastRecord = true
	return nil
}

func rejectBootstrapSegmentStart(startTxID uint64, path string) error {
	if startTxID != 0 {
		return nil
	}
	if path == "" {
		return fmt.Errorf("%w: segment starts at bootstrap tx 0", ErrOpen)
	}
	return fmt.Errorf("%w: segment %s starts at bootstrap tx 0", ErrOpen, path)
}

// LastRecordByteOffset returns the segment byte offset of the most recently
// appended record's header. Valid only after a successful Append; the second
// return is false when no record has been appended since construction.
func (sw *SegmentWriter) LastRecordByteOffset() (int64, bool) {
	return sw.lastRecordOffset, sw.hasLastRecord
}

// Sync flushes and fsyncs.
func (sw *SegmentWriter) Sync() error {
	if err := sw.bw.Flush(); err != nil {
		return err
	}
	return sw.file.Sync()
}

// Close syncs and closes.
func (sw *SegmentWriter) Close() error {
	if err := sw.Sync(); err != nil {
		return err
	}
	return sw.file.Close()
}

// Size returns bytes written.
func (sw *SegmentWriter) Size() int64 { return sw.size }

// SegmentReader reads records from a segment file.
type SegmentReader struct {
	file    *os.File
	startTx uint64
	lastTx  uint64
}

// OpenSegment opens and validates a segment file.
func OpenSegment(path string) (*SegmentReader, error) {
	if err := requireRegularSegmentFile(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if err := ReadSegmentHeader(f); err != nil {
		f.Close()
		return nil, err
	}
	// Parse startTx from filename.
	base := filepath.Base(path)
	var startTx uint64
	if n, scanErr := fmt.Sscanf(base, "%d.log", &startTx); scanErr != nil || n != 1 {
		f.Close()
		return nil, fmt.Errorf("commitlog: invalid segment filename %q", base)
	}
	if base != SegmentFileName(startTx) {
		f.Close()
		return nil, fmt.Errorf("commitlog: non-canonical segment filename %q", base)
	}

	return &SegmentReader{file: f, startTx: startTx}, nil
}

func requireRegularSegmentFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: segment file %s is not a regular file", ErrOpen, path)
	}
	return nil
}

// Next reads the next record using the default max record payload limit.
func (sr *SegmentReader) Next() (*Record, error) {
	return sr.nextWithMax(DefaultCommitLogOptions().MaxRecordPayloadBytes)
}

func (sr *SegmentReader) nextWithMax(maxPayload uint32) (*Record, error) {
	rec, err := DecodeRecord(sr.file, maxPayload)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	sr.lastTx = rec.TxID
	return rec, nil
}

// SeekToTxID positions the reader so that the next Next() call returns the
// smallest record whose TxID is >= target, or io.EOF if no such record
// remains in the segment.
//
// If idx is non-nil, KeyLookup(target) is used to jump to the largest
// indexed record with TxID <= target; any index error (including
// ErrOffsetIndexKeyNotFound) falls back to a linear scan from the segment
// header. Index errors are never propagated to the caller — the index is
// advisory.
func (sr *SegmentReader) SeekToTxID(target types.TxID, idx *OffsetIndex) error {
	startOff := int64(SegmentHeaderSize)
	if idx != nil {
		if key, off, err := idx.KeyLookup(target); err == nil {
			valid, err := sr.validIndexedOffset(off, uint64(key))
			if err != nil {
				return err
			}
			if valid {
				startOff = int64(off)
			}
		}
	}
	if _, err := sr.file.Seek(startOff, io.SeekStart); err != nil {
		return err
	}
	sr.lastTx = 0
	maxPayload := DefaultCommitLogOptions().MaxRecordPayloadBytes
	targetKey := uint64(target)
	for {
		pos, err := sr.file.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		rec, err := sr.nextWithMax(maxPayload)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if rec.TxID >= targetKey {
			if _, err := sr.file.Seek(pos, io.SeekStart); err != nil {
				return err
			}
			sr.lastTx = 0
			return nil
		}
	}
}

func (sr *SegmentReader) validIndexedOffset(off uint64, txID uint64) (bool, error) {
	pos, err := sr.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, err
	}
	lastTx := sr.lastTx
	defer func() {
		sr.lastTx = lastTx
		_, _ = sr.file.Seek(pos, io.SeekStart)
	}()

	info, err := sr.file.Stat()
	if err != nil {
		return false, err
	}
	if off < uint64(SegmentHeaderSize) || off >= uint64(info.Size()) {
		return false, nil
	}
	if _, err := sr.file.Seek(int64(off), io.SeekStart); err != nil {
		return false, err
	}
	rec, err := scanNextRecord(sr)
	if err != nil {
		return false, nil
	}
	return rec.TxID == txID, nil
}

// Close closes the file.
func (sr *SegmentReader) Close() error { return sr.file.Close() }

// StartTxID returns the segment's starting TxID.
func (sr *SegmentReader) StartTxID() uint64 { return sr.startTx }

// LastTxID returns the last read TxID.
func (sr *SegmentReader) LastTxID() uint64 { return sr.lastTx }

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
	_, err := w.Write(buf[:])
	return err
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
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	if _, err := w.Write(rec.Payload); err != nil {
		return err
	}
	crc := ComputeRecordCRC(rec)
	var crcBuf [4]byte
	binary.LittleEndian.PutUint32(crcBuf[:], crc)
	_, err := w.Write(crcBuf[:])
	return err
}

// DecodeRecord reads and validates a record.
func DecodeRecord(r io.Reader, maxPayload uint32) (*Record, error) {
	var buf [RecordHeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		if err == io.ErrUnexpectedEOF {
			return nil, ErrTruncatedRecord
		}
		return nil, err
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
	file    *os.File
	bw      *bufio.Writer
	size    int64
	startTx uint64
	lastTx  uint64
}

// CreateSegment creates a new segment file.
func CreateSegment(dir string, startTxID uint64) (*SegmentWriter, error) {
	path := filepath.Join(dir, SegmentFileName(startTxID))
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

// OpenSegmentForAppend opens an existing segment for appending.
// It validates the header, scans all valid records to find the write position
// and last TxID, truncates any partial trailing record, and returns a writer
// positioned at the end.
func OpenSegmentForAppend(dir string, startTxID uint64) (*SegmentWriter, error) {
	path := filepath.Join(dir, SegmentFileName(startTxID))
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

	// Scan forward through valid records.
	for {
		rec, err := DecodeRecord(f, 0)
		if err != nil {
			if err == io.EOF {
				break
			}
			// Partial/corrupt tail — truncate to last good position.
			if truncErr := f.Truncate(size); truncErr != nil {
				f.Close()
				return nil, truncErr
			}
			break
		}
		size += int64(RecordOverhead + len(rec.Payload))
		lastTx = rec.TxID
	}

	// Seek to write position.
	if _, err := f.Seek(size, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}

	return &SegmentWriter{
		file:    f,
		bw:      bufio.NewWriter(f),
		size:    size,
		startTx: startTxID,
		lastTx:  lastTx,
	}, nil
}

// Append writes a record. TxID must be monotonically increasing.
func (sw *SegmentWriter) Append(rec *Record) error {
	if rec.TxID <= sw.lastTx && sw.lastTx > 0 {
		return fmt.Errorf("commitlog: tx_id %d not > last %d", rec.TxID, sw.lastTx)
	}
	if err := EncodeRecord(sw.bw, rec); err != nil {
		return err
	}
	sw.size += int64(RecordOverhead + len(rec.Payload))
	sw.lastTx = rec.TxID
	return nil
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
	fmt.Sscanf(base, "%d.log", &startTx)

	return &SegmentReader{file: f, startTx: startTx}, nil
}

// Next reads the next record.
func (sr *SegmentReader) Next(maxPayload uint32) (*Record, error) {
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

// Close closes the file.
func (sr *SegmentReader) Close() error { return sr.file.Close() }

// StartTxID returns the segment's starting TxID.
func (sr *SegmentReader) StartTxID() uint64 { return sr.startTx }

// LastTxID returns the last read TxID.
func (sr *SegmentReader) LastTxID() uint64 { return sr.lastTx }

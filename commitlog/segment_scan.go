package commitlog

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ponchione/shunter/types"
)

// AppendMode indicates how recovery should resume writing after scanning segments.
type AppendMode uint8

const (
	// AppendInPlace means the active segment ended cleanly and can be reopened.
	AppendInPlace AppendMode = iota
	// AppendByFreshNextSegment means the active segment has a valid prefix but a
	// truncated or corrupt tail; future writes must continue in a new segment.
	AppendByFreshNextSegment
	// AppendForbidden means this segment must not be appended to.
	AppendForbidden
)

// SegmentInfo describes one scanned segment file.
type SegmentInfo struct {
	Path       string
	StartTx    types.TxID
	LastTx     types.TxID
	Valid      bool
	AppendMode AppendMode
}

// ScanSegments lists, validates, and orders all segment files in dir.
// It returns the validated segment list, plus the highest contiguous valid TxID.
func ScanSegments(dir string) ([]SegmentInfo, types.TxID, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}

	type segmentPath struct {
		path    string
		startTx uint64
	}

	paths := make([]segmentPath, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		startTx, err := parseSegmentFileStartTx(entry.Name())
		if err != nil {
			return nil, 0, err
		}
		paths = append(paths, segmentPath{
			path:    filepath.Join(dir, entry.Name()),
			startTx: startTx,
		})
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].startTx < paths[j].startTx })

	if len(paths) == 0 {
		return nil, 0, nil
	}

	segments := make([]SegmentInfo, 0, len(paths))
	for i, path := range paths {
		info, err := scanOneSegment(path.path, i == len(paths)-1)
		if err != nil {
			return nil, 0, err
		}
		segments = append(segments, info)
	}

	for i := 1; i < len(segments); i++ {
		prev := segments[i-1]
		cur := segments[i]
		expected := uint64(prev.LastTx) + 1
		if uint64(cur.StartTx) != expected {
			return nil, 0, &HistoryGapError{
				Expected: expected,
				Got:      uint64(cur.StartTx),
				Segment:  cur.Path,
			}
		}
	}

	return segments, segments[len(segments)-1].LastTx, nil
}

func parseSegmentFileStartTx(name string) (uint64, error) {
	var startTx uint64
	if n, err := fmt.Sscanf(name, "%d.log", &startTx); err != nil || n != 1 {
		return 0, fmt.Errorf("commitlog: invalid segment filename %q", name)
	}
	if name != SegmentFileName(startTx) {
		return 0, fmt.Errorf("commitlog: non-canonical segment filename %q", name)
	}
	return startTx, nil
}

func scanNextRecord(sr *SegmentReader) (*Record, error) {
	offset, err := sr.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	info, err := sr.file.Stat()
	if err != nil {
		return nil, err
	}
	remaining := info.Size() - offset
	if remaining == 0 {
		return nil, io.EOF
	}
	if remaining < RecordHeaderSize {
		return nil, wrapCategory(ErrOpen, ErrTruncatedRecord)
	}

	var header [RecordHeaderSize]byte
	if _, err := io.ReadFull(sr.file, header[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, wrapCategory(ErrOpen, ErrTruncatedRecord)
		}
		return nil, err
	}

	rec := &Record{
		TxID:       binary.LittleEndian.Uint64(header[:8]),
		RecordType: header[8],
		Flags:      header[9],
	}
	dataLen := binary.LittleEndian.Uint32(header[10:14])
	if int64(dataLen)+RecordCRCSize > remaining-RecordHeaderSize {
		return nil, wrapCategory(ErrTraversal, ErrTruncatedRecord)
	}

	rec.Payload = make([]byte, dataLen)
	if _, err := io.ReadFull(sr.file, rec.Payload); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, wrapCategory(ErrTraversal, ErrTruncatedRecord)
		}
		return nil, err
	}

	var crcBuf [RecordCRCSize]byte
	if _, err := io.ReadFull(sr.file, crcBuf[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, wrapCategory(ErrTraversal, ErrTruncatedRecord)
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
		return nil, wrapCategory(ErrTraversal, ErrBadFlags)
	}

	sr.lastTx = rec.TxID
	return rec, nil
}

func isDamagedTailError(err error) bool {
	if errors.Is(err, ErrTruncatedRecord) {
		return true
	}
	var checksumErr *ChecksumMismatchError
	return errors.As(err, &checksumErr)
}

func canTreatAsDamagedTail(err error, isLast bool, recordCount int) bool {
	return isLast && recordCount > 0 && isDamagedTailError(err)
}

func scanOneSegment(path string, isLast bool) (SegmentInfo, error) {
	sr, err := OpenSegment(path)
	if err != nil {
		return SegmentInfo{}, err
	}
	defer sr.Close()

	info := SegmentInfo{
		Path:       path,
		StartTx:    types.TxID(sr.StartTxID()),
		Valid:      true,
		AppendMode: AppendForbidden,
	}
	if isLast {
		info.AppendMode = AppendInPlace
	}

	var lastTx uint64
	var recordCount int
	for {
		rec, err := scanNextRecord(sr)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if canTreatAsDamagedTail(err, isLast, recordCount) {
				info.AppendMode = AppendByFreshNextSegment
				break
			}
			return SegmentInfo{}, err
		}

		if recordCount == 0 {
			if rec.TxID != uint64(info.StartTx) {
				return SegmentInfo{}, &HistoryGapError{
					Expected: uint64(info.StartTx),
					Got:      rec.TxID,
					Segment:  path,
				}
			}
		} else if rec.TxID != lastTx+1 {
			return SegmentInfo{}, &HistoryGapError{
				Expected: lastTx + 1,
				Got:      rec.TxID,
				Segment:  path,
			}
		}

		lastTx = rec.TxID
		recordCount++
	}

	if recordCount == 0 {
		if uint64(info.StartTx) == 0 {
			info.LastTx = 0
		} else {
			info.LastTx = info.StartTx - 1
		}
		return info, nil
	}

	info.LastTx = types.TxID(lastTx)
	return info, nil
}

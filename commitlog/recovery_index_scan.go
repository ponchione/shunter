package commitlog

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func scanRecoverySegmentsAndSelectSnapshot(baseDir string, reg schema.SchemaRegistry) ([]SegmentInfo, types.TxID, *SnapshotData, []SkippedSnapshotReport, error) {
	segments, durableHorizon, snapshot, skipped, ok, err := tryIndexedSnapshotRecoveryScan(baseDir, reg)
	if err != nil {
		return segments, durableHorizon, nil, skipped, err
	}
	if ok {
		return segments, durableHorizon, snapshot, skipped, nil
	}

	segments, durableHorizon, err = ScanSegments(baseDir)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	snapshotHorizon := durableHorizon
	if len(segments) == 0 {
		snapshotHorizon = types.TxID(^uint64(0))
	}
	snapshot, skipped, err = selectSnapshotWithReport(baseDir, snapshotHorizon, reg)
	return segments, durableHorizon, snapshot, skipped, err
}

func tryIndexedSnapshotRecoveryScan(baseDir string, reg schema.SchemaRegistry) ([]SegmentInfo, types.TxID, *SnapshotData, []SkippedSnapshotReport, bool, error) {
	snapshotDir, _ := resolveSnapshotAndLogDirs(baseDir)

	ids, err := ListSnapshots(snapshotDir)
	if err != nil || len(ids) == 0 {
		return nil, 0, nil, nil, false, nil
	}
	paths, err := listSegmentPaths(baseDir)
	if err != nil || len(paths) == 0 {
		return nil, 0, nil, nil, false, nil
	}

	var skipped []SkippedSnapshotReport
	for _, txID := range ids {
		segments, durableHorizon, ok, err := scanSegmentPathsForSnapshotBoundary(paths, txID)
		if err != nil {
			return nil, 0, nil, skipped, false, err
		}
		if !ok {
			return nil, 0, nil, nil, false, nil
		}
		if txID > durableHorizon {
			skipped = append(skipped, SkippedSnapshotReport{
				TxID:   txID,
				Reason: SnapshotSkipPastDurableHorizon,
			})
			continue
		}

		snapshot, err := ReadSnapshot(filepath.Join(snapshotDir, fmt.Sprintf("%d", txID)))
		if err != nil {
			if isUnsafeSnapshotSelectionError(err) {
				return segments, durableHorizon, nil, skipped, true, err
			}
			skipped = append(skipped, SkippedSnapshotReport{
				TxID:   txID,
				Reason: SnapshotSkipReadFailed,
				Detail: err.Error(),
			})
			continue
		}
		if snapshot.TxID != txID {
			err := fmt.Errorf("%w: snapshot tx_id mismatch: directory=%d header=%d", ErrSnapshot, txID, snapshot.TxID)
			skipped = append(skipped, SkippedSnapshotReport{
				TxID:   txID,
				Reason: SnapshotSkipReadFailed,
				Detail: err.Error(),
			})
			continue
		}
		if err := compareSnapshotSchema(snapshot, reg); err != nil {
			return segments, durableHorizon, nil, skipped, true, err
		}
		return segments, durableHorizon, snapshot, skipped, true, nil
	}

	return nil, 0, nil, nil, false, nil
}

func scanSegmentPathsForSnapshotBoundary(paths []segmentPath, snapshotTxID types.TxID) ([]SegmentInfo, types.TxID, bool, error) {
	segments := make([]SegmentInfo, 0, len(paths))
	target := indexedSnapshotBoundaryTarget(snapshotTxID)
	for i, path := range paths {
		isLast := i == len(paths)-1
		var (
			info SegmentInfo
			err  error
		)
		if types.TxID(path.startTx) <= snapshotTxID {
			var ok bool
			info, ok, err = scanOneSegmentFromOffsetIndex(path.path, isLast, target)
			if err != nil {
				return nil, 0, false, err
			}
			if !ok {
				return nil, 0, false, nil
			}
		} else {
			info, err = scanOneSegment(path.path, isLast)
			if err != nil {
				return nil, 0, false, err
			}
		}
		segments = append(segments, info)
	}
	if err := validateSegmentContinuity(segments); err != nil {
		return nil, 0, false, err
	}
	return segments, segments[len(segments)-1].LastTx, true, nil
}

func indexedSnapshotBoundaryTarget(snapshotTxID types.TxID) types.TxID {
	if snapshotTxID == types.TxID(^uint64(0)) {
		return snapshotTxID
	}
	return snapshotTxID + 1
}

func scanOneSegmentFromOffsetIndex(path string, isLast bool, target types.TxID) (SegmentInfo, bool, error) {
	sr, err := OpenSegment(path)
	if err != nil {
		return SegmentInfo{}, false, nil
	}
	defer sr.Close()

	idxPath := offsetIndexPathForSegment(path)
	if idxPath == "" {
		return SegmentInfo{}, false, nil
	}
	idx, err := openRecoveryOffsetIndex(idxPath)
	if err != nil {
		return SegmentInfo{}, false, nil
	}
	defer idx.Close()

	key, off, err := idx.KeyLookup(target)
	if err != nil {
		return SegmentInfo{}, false, nil
	}
	if uint64(key) < sr.StartTxID() {
		return SegmentInfo{}, false, nil
	}
	valid, err := sr.validIndexedOffset(off, uint64(key))
	if err != nil || !valid {
		return SegmentInfo{}, false, nil
	}
	if _, err := sr.file.Seek(int64(off), io.SeekStart); err != nil {
		return SegmentInfo{}, false, nil
	}
	sr.lastTx = 0

	info := SegmentInfo{
		Path:       path,
		StartTx:    types.TxID(sr.StartTxID()),
		Valid:      true,
		AppendMode: AppendForbidden,
	}
	if isLast {
		info.AppendMode = AppendInPlace
	}

	first, err := scanNextRecord(sr)
	if err != nil || first.TxID != uint64(key) {
		return SegmentInfo{}, false, nil
	}
	if segmentScanRecordHook != nil {
		segmentScanRecordHook(first)
	}
	lastTx := first.TxID
	for {
		rec, err := scanNextRecord(sr)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if isDamagedTailError(err) {
				info.AppendMode = AppendByFreshNextSegment
				break
			}
			return SegmentInfo{}, true, err
		}
		expected, gapErr := nextContiguousRecordTxID(lastTx, rec.TxID, path)
		if gapErr != nil {
			return SegmentInfo{}, true, gapErr
		}
		if rec.TxID != expected {
			return SegmentInfo{}, true, &HistoryGapError{
				Expected: expected,
				Got:      rec.TxID,
				Segment:  path,
			}
		}
		if segmentScanRecordHook != nil {
			segmentScanRecordHook(rec)
		}
		lastTx = rec.TxID
	}
	info.LastTx = types.TxID(lastTx)
	return info, true, nil
}

func openRecoveryOffsetIndex(path string) (*OffsetIndex, error) {
	f, err := openExistingRegularFile(path, os.O_RDONLY, ErrOpen, "offset index file")
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size()%OffsetIndexEntrySize != 0 {
		f.Close()
		return nil, ErrOffsetIndexCorrupt
	}
	capEntries := uint64(info.Size() / OffsetIndexEntrySize)
	n, err := scanRecoveryOffsetIndexPrefix(f, capEntries)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &OffsetIndex{f: f, numEntries: n}, nil
}

func scanRecoveryOffsetIndexPrefix(f *os.File, capEntries uint64) (uint64, error) {
	var buf [OffsetIndexEntrySize]byte
	var (
		n        uint64
		last     uint64
		zeroTail bool
	)
	for i := uint64(0); i < capEntries; i++ {
		if _, err := f.ReadAt(buf[:], int64(i*OffsetIndexEntrySize)); err != nil {
			return 0, err
		}
		key := binary.LittleEndian.Uint64(buf[offsetIndexKeyOff:])
		val := binary.LittleEndian.Uint64(buf[offsetIndexValOff:])
		if key == 0 && val == 0 {
			zeroTail = true
			continue
		}
		if zeroTail || key == 0 || val < SegmentHeaderSize {
			return 0, ErrOffsetIndexCorrupt
		}
		if n > 0 && key <= last {
			return 0, ErrOffsetIndexCorrupt
		}
		n++
		last = key
	}
	return n, nil
}

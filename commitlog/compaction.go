package commitlog

import (
	"os"
	"strings"

	"github.com/ponchione/shunter/types"
)

// SegmentRange describes the covered TxID range for one scanned segment.
type SegmentRange struct {
	Path    string
	MinTxID types.TxID
	MaxTxID types.TxID
	Active  bool
}

var syncDir = syncDirPath

// SegmentCoverage projects recovery-produced SegmentInfo into compaction ranges.
func SegmentCoverage(segments []SegmentInfo) []SegmentRange {
	if len(segments) == 0 {
		return nil
	}

	ranges := make([]SegmentRange, 0, len(segments))
	for i, seg := range segments {
		ranges = append(ranges, SegmentRange{
			Path:    seg.Path,
			MinTxID: seg.StartTx,
			MaxTxID: seg.LastTx,
			Active:  i == len(segments)-1,
		})
	}
	return ranges
}

// Compact decides which segments can be deleted after a snapshot.
func Compact(segments []SegmentRange, snapshotTxID types.TxID) (deleted []string, retained []string) {
	for _, seg := range segments {
		switch {
		case seg.Active:
			retained = append(retained, seg.Path)
		case snapshotTxID == 0:
			retained = append(retained, seg.Path)
		case seg.MaxTxID <= snapshotTxID:
			deleted = append(deleted, seg.Path)
		default:
			retained = append(retained, seg.Path)
		}
	}
	return deleted, retained
}

// RunCompaction deletes sealed segments fully covered by snapshotTxID.
func RunCompaction(dir string, snapshotTxID types.TxID) error {
	segments, _, err := ScanSegments(dir)
	if err != nil {
		return err
	}
	deleted, _ := Compact(SegmentCoverage(segments), snapshotTxID)
	if len(deleted) == 0 {
		return nil
	}
	for _, path := range deleted {
		if err := os.Remove(path); err != nil {
			return err
		}
		if idxPath := offsetIndexPathForSegment(path); idxPath != "" {
			if err := os.Remove(idxPath); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return syncDir(dir)
}

// offsetIndexPathForSegment returns the sidecar offset index path that pairs
// with a %020d.log segment path. Returns "" when path does not look like a
// segment filename (in which case the caller skips cleanup).
func offsetIndexPathForSegment(segmentPath string) string {
	if !strings.HasSuffix(segmentPath, ".log") {
		return ""
	}
	return strings.TrimSuffix(segmentPath, ".log") + ".idx"
}

func syncDirPath(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

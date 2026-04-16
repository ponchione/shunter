package commitlog

import "github.com/ponchione/shunter/types"

// SegmentRange describes the covered TxID range for one scanned segment.
type SegmentRange struct {
	Path    string
	MinTxID types.TxID
	MaxTxID types.TxID
	Active  bool
}

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

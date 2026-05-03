package commitlog

import (
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestReplaySkipsExhaustedSegmentsWithoutOpeningThem pins Shunter's
// segment-level replay-horizon short-circuit.
func TestReplaySkipsExhaustedSegmentsWithoutOpeningThem(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)

	// Segment A: real file with records at tx 1..2.
	pathA := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)

	// Segment B: a SegmentInfo entry that points at a path which does
	// not exist on disk. Any attempt to open it via OpenSegment would
	// return an error. Its LastTx is <= fromTxID, so ReplayLog must
	// skip it without opening.
	nonExistentPath := filepath.Join(root, "segment_never_opened.log")

	segments := []SegmentInfo{
		{Path: pathA, StartTx: 1, LastTx: 2, Valid: true},
		{Path: nonExistentPath, StartTx: 3, LastTx: 5, Valid: true},
	}

	// fromTxID = 5 means: segment A has LastTx 2 <= 5 (skip whole
	// segment), segment B has LastTx 5 <= 5 (skip whole segment).
	// ReplayLog must return cleanly without opening either file.
	maxTxID, err := ReplayLog(committed, segments, 5, reg)
	if err != nil {
		t.Fatalf("ReplayLog with all segments exhausted returned error %v; "+
			"segment-level skip must short-circuit before OpenSegment", err)
	}
	if maxTxID != 5 {
		t.Fatalf("ReplayLog max tx = %d, want 5 (fromTxID preserved)", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{})

	// Sanity: with fromTxID lowered below segment B's LastTx, ReplayLog
	// would attempt to open segment B and fail. This proves the
	// short-circuit above really is gated on LastTx <= fromTxID, not
	// accidentally skipping for some unrelated reason.
	committed2, reg2 := buildReplayCommittedState(t)
	_, err = ReplayLog(committed2, segments, 2, reg2)
	if err == nil {
		t.Fatal("expected open error when segment B (non-existent path) is in-range; " +
			"short-circuit must not fire when LastTx > fromTxID")
	}
}

package commitlog

import (
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestReplaySkipsExhaustedSegmentsWithoutOpeningThem pins
// Shunter's intentional divergence from reference replay-horizon skip
// granularity.
//
// Reference (SpacetimeDB commitlog): per-commit skip via
// `CommitInfo::adjust_initial_offset` in
// `reference/SpacetimeDB/crates/commitlog/src/commitlog.rs:834-845`.
// Every segment in the range is opened; individual commits below the
// resume offset are filtered as they stream.
//
// Shunter: segment-level short-circuit in
// `commitlog/replay.go:21-23`. When `segment.LastTx <= fromTxID`, the
// segment file is never opened. This is safe because `ScanSegments`
// already observed the segment's `LastTx`, so no commit inside can
// contribute above the horizon. Same externally visible outcome; the
// mechanism difference is pinned here as intentional.
//
// This test is the direct Shunter contract anchor for the segment-level
// short-circuit divergence.
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

package commitlog

import "testing"

func TestSegmentCoverageBuildsRangesFromSegmentInfo(t *testing.T) {
	segments := []SegmentInfo{
		{Path: "00000000000000000001.log", StartTx: 1, LastTx: 4},
		{Path: "00000000000000000005.log", StartTx: 5, LastTx: 7},
		{Path: "00000000000000000008.log", StartTx: 8, LastTx: 10},
	}

	got := SegmentCoverage(segments)
	want := []SegmentRange{
		{Path: "00000000000000000001.log", MinTxID: 1, MaxTxID: 4, Active: false},
		{Path: "00000000000000000005.log", MinTxID: 5, MaxTxID: 7, Active: false},
		{Path: "00000000000000000008.log", MinTxID: 8, MaxTxID: 10, Active: true},
	}

	assertSegmentRangesEqual(t, got, want)
}

func TestSegmentCoverageHandlesSingleRecordAndEmptySegments(t *testing.T) {
	segments := []SegmentInfo{
		{Path: "00000000000000000011.log", StartTx: 11, LastTx: 11},
		{Path: "00000000000000000012.log", StartTx: 12, LastTx: 11},
	}

	got := SegmentCoverage(segments)
	want := []SegmentRange{
		{Path: "00000000000000000011.log", MinTxID: 11, MaxTxID: 11, Active: false},
		{Path: "00000000000000000012.log", MinTxID: 12, MaxTxID: 11, Active: true},
	}

	assertSegmentRangesEqual(t, got, want)
}

func assertSegmentRangesEqual(t *testing.T, got, want []SegmentRange) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("range count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].MinTxID != want[i].MinTxID || got[i].MaxTxID != want[i].MaxTxID || got[i].Active != want[i].Active {
			t.Fatalf("range[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

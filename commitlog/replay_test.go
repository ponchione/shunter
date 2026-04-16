package commitlog

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestReplayLogReplaysAcrossSegmentsFromZeroAndReturnsMaxTxID(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)

	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)
	writeReplaySegment(t, root, 3,
		replayRecord{txID: 3, deletes: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)

	segments := []SegmentInfo{
		{Path: filepath.Join(root, SegmentFileName(1)), StartTx: 1, LastTx: 2, Valid: true},
		{Path: filepath.Join(root, SegmentFileName(3)), StartTx: 3, LastTx: 4, Valid: true},
	}

	maxTxID, err := ReplayLog(committed, segments, 0, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 4 {
		t.Fatalf("ReplayLog max tx = %d, want 4", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{2: "bob", 3: "carol"})
}

func TestReplayLogSkipsRecordsAtOrBelowFromTxID(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	seedReplayState(t, committed, map[uint64]string{1: "alice", 2: "bob"})

	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)
	writeReplaySegment(t, root, 3,
		replayRecord{txID: 3, deletes: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)

	segments := []SegmentInfo{
		{Path: filepath.Join(root, SegmentFileName(1)), StartTx: 1, LastTx: 2, Valid: true},
		{Path: filepath.Join(root, SegmentFileName(3)), StartTx: 3, LastTx: 4, Valid: true},
	}

	maxTxID, err := ReplayLog(committed, segments, 2, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 4 {
		t.Fatalf("ReplayLog max tx = %d, want 4", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{2: "bob", 3: "carol"})
}

func TestReplayLogEmptyReplayReturnsFromTxID(t *testing.T) {
	committed, reg := buildReplayCommittedState(t)

	maxTxID, err := ReplayLog(committed, nil, 7, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 7 {
		t.Fatalf("ReplayLog max tx = %d, want 7", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{})
}

func TestReplayLogSkipAllRecordsReturnsFromTxID(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	seedReplayState(t, committed, map[uint64]string{1: "alice", 2: "bob"})

	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)
	segments := []SegmentInfo{{Path: filepath.Join(root, SegmentFileName(1)), StartTx: 1, LastTx: 2, Valid: true}}

	maxTxID, err := ReplayLog(committed, segments, 2, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("ReplayLog max tx = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, committed, map[uint64]string{1: "alice", 2: "bob"})
}

func TestReplayLogDecodeErrorIncludesTxAndSegmentContext(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	segmentPath := writeReplaySegment(t, root, 5, replayRecord{txID: 5, rawPayload: []byte{0xFF}})
	segments := []SegmentInfo{{Path: segmentPath, StartTx: 5, LastTx: 5, Valid: true}}

	_, err := ReplayLog(committed, segments, 0, reg)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "tx 5") {
		t.Fatalf("decode error %q missing tx context", err)
	}
	if !strings.Contains(err.Error(), segmentPath) {
		t.Fatalf("decode error %q missing segment path", err)
	}
}

func TestReplayLogApplyErrorIncludesTxAndSegmentContext(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	segmentPath := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice-again")}}},
	)
	segments := []SegmentInfo{{Path: segmentPath, StartTx: 1, LastTx: 2, Valid: true}}

	_, err := ReplayLog(committed, segments, 0, reg)
	if err == nil {
		t.Fatal("expected apply error")
	}
	var pkErr *store.PrimaryKeyViolationError
	if !errors.As(err, &pkErr) {
		t.Fatalf("expected wrapped PrimaryKeyViolationError, got %v", err)
	}
	if !strings.Contains(err.Error(), "tx 2") {
		t.Fatalf("apply error %q missing tx context", err)
	}
	if !strings.Contains(err.Error(), segmentPath) {
		t.Fatalf("apply error %q missing segment path", err)
	}
}

type replayRecord struct {
	txID       uint64
	inserts    []types.ProductValue
	deletes    []types.ProductValue
	rawPayload []byte
}

func buildReplayCommittedState(t *testing.T) (*store.CommittedState, schema.SchemaRegistry) {
	t.Helper()
	_, reg := testSchema()
	committed := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		tableSchema, _ := reg.Table(tableID)
		committed.RegisterTable(tableID, store.NewTable(tableSchema))
	}
	return committed, reg
}

func seedReplayState(t *testing.T, committed *store.CommittedState, rows map[uint64]string) {
	t.Helper()
	table, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	ids := make([]uint64, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(id), types.NewString(rows[id])}); err != nil {
			t.Fatal(err)
		}
	}
}

func writeReplaySegment(t *testing.T, root string, startTx uint64, records ...replayRecord) string {
	t.Helper()
	seg, err := CreateSegment(root, startTx)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range records {
		payload := rec.rawPayload
		if payload == nil {
			payload, err = EncodeChangeset(&store.Changeset{
				TxID: types.TxID(rec.txID),
				Tables: map[schema.TableID]*store.TableChangeset{
					0: {
						TableID:   0,
						TableName: "players",
						Inserts:   rec.inserts,
						Deletes:   rec.deletes,
					},
				},
			})
			if err != nil {
				_ = seg.Close()
				t.Fatal(err)
			}
		}
		if err := seg.Append(&Record{TxID: rec.txID, RecordType: RecordTypeChangeset, Payload: payload}); err != nil {
			_ = seg.Close()
			t.Fatal(err)
		}
	}
	if err := seg.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, SegmentFileName(startTx))
}

func assertReplayPlayerRows(t *testing.T, committed *store.CommittedState, want map[uint64]string) {
	t.Helper()
	table, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	got := make(map[uint64]string, table.RowCount())
	for _, row := range table.Scan() {
		got[row[0].AsUint64()] = row[1].AsString()
	}
	if len(got) != len(want) {
		t.Fatalf("players row count = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for id, wantName := range want {
		if gotName, ok := got[id]; !ok || gotName != wantName {
			t.Fatalf("players rows = %v, want %v", got, want)
		}
	}
}

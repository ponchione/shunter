package store

import (
	"errors"
	"sort"
	"testing"

	"github.com/ponchione/shunter/types"
	"pgregory.net/rapid"
)

type rapidStoreModel struct {
	rows   map[uint64]string
	rowIDs map[uint64]types.RowID
}

func newRapidStoreModel() rapidStoreModel {
	return rapidStoreModel{
		rows:   make(map[uint64]string),
		rowIDs: make(map[uint64]types.RowID),
	}
}

func (m rapidStoreModel) clone() rapidStoreModel {
	cp := newRapidStoreModel()
	for pk, name := range m.rows {
		cp.rows[pk] = name
		cp.rowIDs[pk] = m.rowIDs[pk]
	}
	return cp
}

func (m rapidStoreModel) sortedKeys() []uint64 {
	keys := make([]uint64, 0, len(m.rows))
	for pk := range m.rows {
		keys = append(keys, pk)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func rapidPlayerName() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-z0-9_]{0,16}`)
}

func rapidDrawExistingPK(t *rapid.T, m rapidStoreModel, label string) uint64 {
	return rapid.SampledFrom(m.sortedKeys()).Draw(t, label)
}

func TestRapidStoreCommitMatchesModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cs, reg := buildTestState()
		committed := newRapidStoreModel()
		txCount := rapid.IntRange(1, 8).Draw(t, "txCount")

		for txIndex := range txCount {
			tx := NewTransaction(cs, reg)
			txModel := committed.clone()
			opCount := rapid.IntRange(1, 12).Draw(t, "opCount")
			for opIndex := range opCount {
				rapidApplyStoreOperation(t, tx, txModel, "tx"+string(rune('0'+txIndex))+"op"+string(rune('0'+opIndex)))
			}

			if rapid.Bool().Draw(t, "commit") {
				if _, err := Commit(cs, tx); err != nil {
					t.Fatalf("Commit: %v", err)
				}
				committed = txModel
				assertRapidCommittedPlayers(t, cs, committed)
			} else {
				Rollback(tx)
				assertRapidCommittedPlayers(t, cs, committed)
			}
		}
	})
}

func TestRapidStateViewMatchesTransactionModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cs, reg := buildTestState()
		committed := newRapidStoreModel()
		txCount := rapid.IntRange(1, 8).Draw(t, "txCount")

		for txIndex := range txCount {
			tx := NewTransaction(cs, reg)
			txModel := committed.clone()
			opCount := rapid.IntRange(1, 12).Draw(t, "opCount")
			for opIndex := range opCount {
				rapidApplyStoreOperation(t, tx, txModel, "tx"+string(rune('0'+txIndex))+"op"+string(rune('0'+opIndex)))
				low := rapid.Uint64Range(0, 32).Draw(t, "low")
				high := rapid.Uint64Range(low, 33).Draw(t, "high")
				assertRapidStateViewPlayers(t, NewStateView(cs, tx.TxState()), txModel, low, high)
			}

			if rapid.Bool().Draw(t, "commit") {
				if _, err := Commit(cs, tx); err != nil {
					t.Fatalf("Commit: %v", err)
				}
				committed = txModel
			} else {
				Rollback(tx)
			}
		}
	})
}

func TestRapidStoreRejectsDuplicatePrimaryKeyWithoutMutation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cs, reg := buildTestState()
		pks := rapid.SliceOfNDistinct(rapid.Uint64Range(0, 32), 2, 2, rapid.ID[uint64]).Draw(t, "pks")
		nameA := rapidPlayerName().Draw(t, "nameA")
		nameB := rapidPlayerName().Draw(t, "nameB")
		nameDup := rapidPlayerName().Draw(t, "nameDup")

		tx := NewTransaction(cs, reg)
		rowIDA, err := tx.Insert(0, mkRow(pks[0], nameA))
		if err != nil {
			t.Fatalf("insert first row: %v", err)
		}
		before := newRapidStoreModel()
		before.rows[pks[0]] = nameA
		before.rowIDs[pks[0]] = rowIDA

		_, err = tx.Insert(0, mkRow(pks[0], nameDup))
		if !errors.Is(err, ErrPrimaryKeyViolation) {
			t.Fatalf("duplicate tx insert err = %v, want ErrPrimaryKeyViolation", err)
		}
		assertRapidStateViewPlayers(t, NewStateView(cs, tx.TxState()), before, 0, 33)

		rowIDB, err := tx.Insert(0, mkRow(pks[1], nameB))
		if err != nil {
			t.Fatalf("insert second row: %v", err)
		}
		before.rows[pks[1]] = nameB
		before.rowIDs[pks[1]] = rowIDB

		_, err = tx.Update(0, rowIDA, mkRow(pks[1], nameDup))
		if !errors.Is(err, ErrPrimaryKeyViolation) {
			t.Fatalf("duplicate update err = %v, want ErrPrimaryKeyViolation", err)
		}
		assertRapidStateViewPlayers(t, NewStateView(cs, tx.TxState()), before, 0, 33)

		if _, err := Commit(cs, tx); err != nil {
			t.Fatalf("commit seed rows: %v", err)
		}
		assertRapidCommittedPlayers(t, cs, before)

		tx2 := NewTransaction(cs, reg)
		_, err = tx2.Insert(0, mkRow(pks[0], nameDup))
		if !errors.Is(err, ErrPrimaryKeyViolation) {
			t.Fatalf("duplicate committed insert err = %v, want ErrPrimaryKeyViolation", err)
		}
		assertRapidStateViewPlayers(t, NewStateView(cs, tx2.TxState()), before, 0, 33)
		assertRapidCommittedPlayers(t, cs, before)
	})
}

func rapidApplyStoreOperation(t *rapid.T, tx *Transaction, model rapidStoreModel, label string) {
	op := rapid.IntRange(0, 2).Draw(t, label+"op")
	if len(model.rows) == 0 {
		op = 0
	}

	switch op {
	case 0:
		pk := rapid.Uint64Range(0, 32).Draw(t, label+"insertPK")
		name := rapidPlayerName().Draw(t, label+"insertName")
		rowID, err := tx.Insert(0, mkRow(pk, name))
		if _, exists := model.rows[pk]; exists {
			if !errors.Is(err, ErrPrimaryKeyViolation) {
				t.Fatalf("duplicate insert pk %d err = %v, want ErrPrimaryKeyViolation", pk, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("insert pk %d: %v", pk, err)
		}
		model.rows[pk] = name
		model.rowIDs[pk] = rowID
	case 1:
		pk := rapidDrawExistingPK(t, model, label+"deletePK")
		if err := tx.Delete(0, model.rowIDs[pk]); err != nil {
			t.Fatalf("delete pk %d row %d: %v", pk, model.rowIDs[pk], err)
		}
		delete(model.rows, pk)
		delete(model.rowIDs, pk)
	case 2:
		oldPK := rapidDrawExistingPK(t, model, label+"updateOldPK")
		newPK := rapid.Uint64Range(0, 32).Draw(t, label+"updateNewPK")
		newName := rapidPlayerName().Draw(t, label+"updateName")
		newRowID, err := tx.Update(0, model.rowIDs[oldPK], mkRow(newPK, newName))
		if _, exists := model.rows[newPK]; exists && newPK != oldPK {
			if !errors.Is(err, ErrPrimaryKeyViolation) {
				t.Fatalf("duplicate update pk %d -> %d err = %v, want ErrPrimaryKeyViolation", oldPK, newPK, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("update pk %d -> %d: %v", oldPK, newPK, err)
		}
		delete(model.rows, oldPK)
		delete(model.rowIDs, oldPK)
		model.rows[newPK] = newName
		model.rowIDs[newPK] = newRowID
	default:
		t.Fatalf("unknown rapid store op %d", op)
	}
}

func assertRapidCommittedPlayers(t rapidStoreFataler, cs *CommittedState, want rapidStoreModel) {
	t.Helper()
	table, ok := cs.Table(0)
	if !ok {
		t.Fatalf("players table missing")
	}
	got := collectRapidRows(table.Scan())
	assertRapidRowsMatch(t, got, want)

	pk := table.PrimaryIndex()
	if pk == nil {
		t.Fatalf("primary index missing")
	}
	for id := uint64(0); id <= 32; id++ {
		gotIDs := pk.Seek(NewIndexKey(types.NewUint64(id)))
		wantRowID, exists := want.rowIDs[id]
		if !exists {
			if len(gotIDs) != 0 {
				t.Fatalf("primary seek pk %d = %v, want none", id, gotIDs)
			}
			continue
		}
		if len(gotIDs) != 1 || gotIDs[0] != wantRowID {
			t.Fatalf("primary seek pk %d = %v, want [%d]", id, gotIDs, wantRowID)
		}
	}
}

func assertRapidStateViewPlayers(t rapidStoreFataler, sv *StateView, want rapidStoreModel, low, high uint64) {
	t.Helper()
	got := collectRapidRows(sv.ScanTable(0))
	assertRapidRowsMatch(t, got, want)

	for pk, wantName := range want.rows {
		rowID := want.rowIDs[pk]
		row, ok := sv.GetRow(0, rowID)
		if !ok {
			t.Fatalf("StateView.GetRow(%d) missing for pk %d", rowID, pk)
		}
		if gotPK, gotName := row[0].AsUint64(), row[1].AsString(); gotPK != pk || gotName != wantName {
			t.Fatalf("StateView.GetRow(%d) = (%d,%q), want (%d,%q)", rowID, gotPK, gotName, pk, wantName)
		}

		exact := collectRapidRowIDSeq(sv.SeekIndex(0, 0, NewIndexKey(types.NewUint64(pk))))
		if len(exact) != 1 || exact[0] != rowID {
			t.Fatalf("StateView.SeekIndex pk %d = %v, want [%d]", pk, exact, rowID)
		}
	}
	for pk := uint64(0); pk <= 32; pk++ {
		if _, exists := want.rows[pk]; exists {
			continue
		}
		if gotIDs := collectRapidRowIDSeq(sv.SeekIndex(0, 0, NewIndexKey(types.NewUint64(pk)))); len(gotIDs) != 0 {
			t.Fatalf("StateView.SeekIndex absent pk %d = %v, want none", pk, gotIDs)
		}
	}

	lowKey := NewIndexKey(types.NewUint64(low))
	highKey := NewIndexKey(types.NewUint64(high))
	rangeIDs := collectRapidRowIDSeq(sv.SeekIndexRange(0, 0, &lowKey, &highKey))
	wantRange := make([]types.RowID, 0)
	for pk, rowID := range want.rowIDs {
		if pk >= low && pk < high {
			wantRange = append(wantRange, rowID)
		}
	}
	sort.Slice(wantRange, func(i, j int) bool { return wantRange[i] < wantRange[j] })
	sort.Slice(rangeIDs, func(i, j int) bool { return rangeIDs[i] < rangeIDs[j] })
	if len(rangeIDs) != len(wantRange) {
		t.Fatalf("StateView.SeekIndexRange [%d,%d) = %v, want %v", low, high, rangeIDs, wantRange)
	}
	for i := range rangeIDs {
		if rangeIDs[i] != wantRange[i] {
			t.Fatalf("StateView.SeekIndexRange [%d,%d) = %v, want %v", low, high, rangeIDs, wantRange)
		}
	}
}

type rapidStoreFataler interface {
	Helper()
	Fatalf(string, ...any)
}

func collectRapidRows(seq RowIterator) rapidStoreModel {
	got := newRapidStoreModel()
	for rowID, row := range seq {
		pk := row[0].AsUint64()
		got.rows[pk] = row[1].AsString()
		got.rowIDs[pk] = rowID
	}
	return got
}

func collectRapidRowIDSeq(seq func(func(types.RowID) bool)) []types.RowID {
	var out []types.RowID
	for rowID := range seq {
		out = append(out, rowID)
	}
	return out
}

func assertRapidRowsMatch(t rapidStoreFataler, got, want rapidStoreModel) {
	t.Helper()
	if len(got.rows) != len(want.rows) {
		t.Fatalf("row count = %d, want %d (got=%v want=%v)", len(got.rows), len(want.rows), got.rows, want.rows)
	}
	for pk, wantName := range want.rows {
		if gotName, ok := got.rows[pk]; !ok || gotName != wantName {
			t.Fatalf("rows = %v, want %v", got.rows, want.rows)
		}
		if gotID, wantID := got.rowIDs[pk], want.rowIDs[pk]; gotID != wantID {
			t.Fatalf("rowID for pk %d = %d, want %d", pk, gotID, wantID)
		}
	}
}

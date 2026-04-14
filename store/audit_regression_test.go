package store

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func buildNoPKState(t *testing.T) (*CommittedState, schema.SchemaRegistry) {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "logs",
		Columns: []schema.ColumnDefinition{{Name: "msg", Type: types.KindString}},
	})
	e, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := e.Registry()
	cs := NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, NewTable(ts))
	}
	return cs, reg
}

func TestValidateRowTypeMismatchMatchesCatalog(t *testing.T) {
	ts := pkSchema()
	err := ValidateRow(ts, types.ProductValue{types.NewString("bad"), types.NewString("alice")})
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
	var tmErr *TypeMismatchError
	if !errors.As(err, &tmErr) {
		t.Fatalf("expected TypeMismatchError, got %T", err)
	}
	if !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("expected errors.Is(err, ErrTypeMismatch), got %v", err)
	}
}

func TestBTreeSeekRangeNilBoundsAndMultiColumnBytesOrdering(t *testing.T) {
	bt := NewBTreeIndex()
	keys := []IndexKey{
		NewIndexKey(types.NewBytes([]byte{0x61}), types.NewUint64(2)),
		NewIndexKey(types.NewBytes([]byte{0x61}), types.NewUint64(1)),
		NewIndexKey(types.NewBytes([]byte{0x61, 0x00}), types.NewUint64(1)),
		NewIndexKey(types.NewBytes([]byte{0x62}), types.NewUint64(1)),
	}
	for i, key := range keys {
		bt.Insert(key, types.RowID(i+1))
	}

	low := NewIndexKey(types.NewBytes([]byte{0x61}), types.NewUint64(1))
	var gotLow []types.RowID
	for rid := range bt.SeekRange(&low, nil) {
		gotLow = append(gotLow, rid)
	}
	wantLow := []types.RowID{2, 1, 3, 4}
	if len(gotLow) != len(wantLow) {
		t.Fatalf("low-bounded range len = %d, want %d (%v)", len(gotLow), len(wantLow), gotLow)
	}
	for i := range wantLow {
		if gotLow[i] != wantLow[i] {
			t.Fatalf("low-bounded range = %v, want %v", gotLow, wantLow)
		}
	}

	high := NewIndexKey(types.NewBytes([]byte{0x62}), types.NewUint64(1))
	var gotHigh []types.RowID
	for rid := range bt.SeekRange(nil, &high) {
		gotHigh = append(gotHigh, rid)
	}
	wantHigh := []types.RowID{2, 1, 3}
	if len(gotHigh) != len(wantHigh) {
		t.Fatalf("high-bounded range len = %d, want %d (%v)", len(gotHigh), len(wantHigh), gotHigh)
	}
	for i := range wantHigh {
		if gotHigh[i] != wantHigh[i] {
			t.Fatalf("high-bounded range = %v, want %v", gotHigh, wantHigh)
		}
	}
}

func TestTransactionInsertUndeletesCommittedPrimaryKey(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	originalID := tbl.AllocRowID()
	row := mkRow(1, "alice")
	if err := tbl.InsertRow(originalID, row); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, reg)
	if err := tx.Delete(0, originalID); err != nil {
		t.Fatal(err)
	}
	returnedID, err := tx.Insert(0, row)
	if err != nil {
		t.Fatalf("insert should undelete committed row: %v", err)
	}
	if returnedID != originalID {
		t.Fatalf("undelete returned RowID %d, want original %d", returnedID, originalID)
	}
	if tx.tx.IsDeleted(0, originalID) {
		t.Fatal("undelete should cancel pending delete")
	}
	if tx.tx.IsInserted(0, returnedID) {
		t.Fatal("undelete should not leave a tx-local insert")
	}
}

func TestTransactionInsertUndeletesCommittedSetSemanticsRow(t *testing.T) {
	cs, reg := buildNoPKState(t)
	tbl, _ := cs.Table(0)
	originalID := tbl.AllocRowID()
	row := types.ProductValue{types.NewString("hello")}
	if err := tbl.InsertRow(originalID, row); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, reg)
	if err := tx.Delete(0, originalID); err != nil {
		t.Fatal(err)
	}
	returnedID, err := tx.Insert(0, row)
	if err != nil {
		t.Fatalf("insert should undelete committed duplicate row: %v", err)
	}
	if returnedID != originalID {
		t.Fatalf("undelete returned RowID %d, want original %d", returnedID, originalID)
	}
	if tx.tx.IsDeleted(0, originalID) {
		t.Fatal("undelete should cancel pending delete")
	}
}

func TestCommitDeleteIdenticalReinsertCollapsesToEmptyChangeset(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	originalID := tbl.AllocRowID()
	row := mkRow(1, "alice")
	if err := tbl.InsertRow(originalID, row); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, reg)
	if err := tx.Delete(0, originalID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Insert(0, row); err != nil {
		t.Fatal(err)
	}

	changeset, err := Commit(cs, tx)
	if err != nil {
		t.Fatal(err)
	}
	if !changeset.IsEmpty() {
		t.Fatalf("identical delete+reinsert should collapse to empty changeset, got %#v", changeset)
	}
}

func TestSnapshotBlocksCommitUntilClose(t *testing.T) {
	cs, reg := buildTestState()
	snap := cs.Snapshot()

	tx := NewTransaction(cs, reg)
	if _, err := tx.Insert(0, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Commit(cs, tx)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("commit completed before snapshot close: %v", err)
	default:
	}

	snap.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestApplyChangesetDeletesByPrimaryKeyNotStoredRowID(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	storedID := tbl.AllocRowID()
	row := mkRow(7, "alice")
	if err := tbl.InsertRow(storedID, row); err != nil {
		t.Fatal(err)
	}

	changeset := &Changeset{
		TxID: 2,
		Tables: map[schema.TableID]*TableChangeset{
			0: {
				Deletes: []types.ProductValue{row},
			},
		},
	}

	if err := ApplyChangeset(cs, changeset); err != nil {
		t.Fatal(err)
	}
	if tbl.RowCount() != 0 {
		t.Fatalf("apply delete by PK should remove row; row count = %d", tbl.RowCount())
	}
}

func TestApplyChangesetDeletesByRowEqualityForSetSemanticsTables(t *testing.T) {
	cs, _ := buildNoPKState(t)
	tbl, _ := cs.Table(0)
	row := types.ProductValue{types.NewString("hello")}
	if err := tbl.InsertRow(tbl.AllocRowID(), row); err != nil {
		t.Fatal(err)
	}

	changeset := &Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*TableChangeset{
			0: {
				Deletes: []types.ProductValue{row},
			},
		},
	}

	if err := ApplyChangeset(cs, changeset); err != nil {
		t.Fatal(err)
	}
	if tbl.RowCount() != 0 {
		t.Fatalf("apply delete by row equality should remove row; row count = %d", tbl.RowCount())
	}
}

func TestApplyChangesetUnknownTableReturnsError(t *testing.T) {
	cs, _ := buildTestState()
	changeset := &Changeset{Tables: map[schema.TableID]*TableChangeset{99: {}}}
	if err := ApplyChangeset(cs, changeset); err == nil {
		t.Fatal("expected unknown table error")
	}
}

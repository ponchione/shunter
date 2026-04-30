package store

import (
	"errors"
	"slices"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// --- Helpers ---

func pkSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   0,
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "name", Type: types.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
		},
	}
}

func noPKSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   1,
		Name: "logs",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "msg", Type: types.KindString},
		},
	}
}

func mkRow(id uint64, name string) types.ProductValue {
	return types.ProductValue{types.NewUint64(id), types.NewString(name)}
}

// --- IndexKey tests (E3 Story 3.1) ---

func TestIndexKeyCompare(t *testing.T) {
	a := NewIndexKey(types.NewString("a"))
	b := NewIndexKey(types.NewString("b"))
	if a.Compare(b) != -1 {
		t.Fatal("a < b")
	}
	if a.Compare(a) != 0 {
		t.Fatal("a == a")
	}
}

func TestIndexKeyMultiColumn(t *testing.T) {
	a1 := NewIndexKey(types.NewString("a"), types.NewInt64(1))
	a2 := NewIndexKey(types.NewString("a"), types.NewInt64(2))
	b1 := NewIndexKey(types.NewString("b"), types.NewInt64(1))

	if a1.Compare(a2) != -1 {
		t.Fatal("(a,1) < (a,2)")
	}
	if a2.Compare(b1) != -1 {
		t.Fatal("(a,2) < (b,1)")
	}
}

func TestIndexKeyPrefixOrdering(t *testing.T) {
	short := NewIndexKey(types.NewString("a"))
	long := NewIndexKey(types.NewString("a"), types.NewInt64(1))
	if short.Compare(long) != -1 {
		t.Fatal("shorter prefix < longer")
	}
}

func TestBoundConstructors(t *testing.T) {
	var _ Bound

	low := UnboundedLow()
	if !low.Unbounded {
		t.Fatal("UnboundedLow should set Unbounded")
	}

	high := UnboundedHigh()
	if !high.Unbounded {
		t.Fatal("UnboundedHigh should set Unbounded")
	}

	inclValue := types.NewInt64(7)
	incl := Inclusive(inclValue)
	if incl.Unbounded {
		t.Fatal("Inclusive should not be unbounded")
	}
	if !incl.Inclusive {
		t.Fatal("Inclusive should set Inclusive=true")
	}
	if incl.Value.Compare(inclValue) != 0 {
		t.Fatal("Inclusive should preserve bound value")
	}

	exclValue := types.NewInt64(9)
	excl := Exclusive(exclValue)
	if excl.Unbounded {
		t.Fatal("Exclusive should not be unbounded")
	}
	if excl.Inclusive {
		t.Fatal("Exclusive should set Inclusive=false")
	}
	if excl.Value.Compare(exclValue) != 0 {
		t.Fatal("Exclusive should preserve bound value")
	}
}

// --- BTreeIndex tests (E3 Stories 3.2-3.4) ---

func TestBTreeInsertSeek(t *testing.T) {
	bt := NewBTreeIndex()
	k := NewIndexKey(types.NewUint64(1))
	bt.Insert(k, 10)
	bt.Insert(k, 20)

	ids := bt.Seek(k)
	if len(ids) != 2 || ids[0] != 10 || ids[1] != 20 {
		t.Fatalf("Seek = %v, want [10 20]", ids)
	}
}

func TestBTreeRemove(t *testing.T) {
	bt := NewBTreeIndex()
	k := NewIndexKey(types.NewUint64(1))
	bt.Insert(k, 10)
	bt.Insert(k, 20)
	bt.Remove(k, 10)

	ids := bt.Seek(k)
	if len(ids) != 1 || ids[0] != 20 {
		t.Fatalf("after remove: %v, want [20]", ids)
	}
}

func TestBTreeRemoveLastDeletesKey(t *testing.T) {
	bt := NewBTreeIndex()
	k := NewIndexKey(types.NewUint64(1))
	bt.Insert(k, 10)
	bt.Remove(k, 10)
	if bt.Seek(k) != nil {
		t.Fatal("key should be gone")
	}
	if bt.Len() != 0 {
		t.Fatal("Len should be 0")
	}
}

func TestBTreeSeekRange(t *testing.T) {
	bt := NewBTreeIndex()
	for i := range 5 {
		bt.Insert(NewIndexKey(types.NewInt64(int64(i))), types.RowID(i))
	}

	low := NewIndexKey(types.NewInt64(1))
	high := NewIndexKey(types.NewInt64(3))
	var got []types.RowID
	for rid := range bt.SeekRange(&low, &high) {
		got = append(got, rid)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("SeekRange [1,3) = %v, want [1 2]", got)
	}
}

func TestBTreeScan(t *testing.T) {
	bt := NewBTreeIndex()
	for i := range 3 {
		bt.Insert(NewIndexKey(types.NewInt64(int64(i))), types.RowID(i))
	}
	var got []types.RowID
	for rid := range bt.Scan() {
		got = append(got, rid)
	}
	if len(got) != 3 {
		t.Fatalf("Scan = %v, want 3 entries", got)
	}
}

func TestBTreePagedInsertRemoveMaintainsOrderedScan(t *testing.T) {
	bt := NewBTreeIndex()
	for i := 200; i >= 1; i-- {
		bt.Insert(NewIndexKey(types.NewUint64(uint64(i))), types.RowID(i))
	}
	if len(bt.pages) < 2 {
		t.Fatalf("paged index did not split: pages=%d", len(bt.pages))
	}
	var got []types.RowID
	for rid := range bt.Scan() {
		got = append(got, rid)
	}
	want := make([]types.RowID, 200)
	for i := range want {
		want[i] = types.RowID(i + 1)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Scan after paged inserts = %v, want %v", got, want)
	}

	for i := 1; i <= 200; i += 3 {
		bt.Remove(NewIndexKey(types.NewUint64(uint64(i))), types.RowID(i))
	}
	got = got[:0]
	for rid := range bt.Scan() {
		got = append(got, rid)
	}
	want = want[:0]
	for i := 1; i <= 200; i++ {
		if i%3 != 1 {
			want = append(want, types.RowID(i))
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Scan after paged removes = %v, want %v", got, want)
	}
	if bt.Len() != len(want) {
		t.Fatalf("Len = %d, want %d", bt.Len(), len(want))
	}
}

func TestExtractKey(t *testing.T) {
	row := types.ProductValue{types.NewUint64(42), types.NewString("alice")}
	k := ExtractKey(row, []int{0})
	if k.Len() != 1 || k.Part(0).AsUint64() != 42 {
		t.Fatal("ExtractKey wrong")
	}
}

// --- Row validation (E2 Story 2.3) ---

func TestValidateRowOK(t *testing.T) {
	ts := pkSchema()
	if err := ValidateRow(ts, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRowWrongCount(t *testing.T) {
	ts := pkSchema()
	err := ValidateRow(ts, types.ProductValue{types.NewUint64(1)})
	if !errors.Is(err, ErrRowShapeMismatch) {
		t.Fatalf("expected ErrRowShapeMismatch, got %v", err)
	}
}

func TestValidateRowTypeMismatch(t *testing.T) {
	ts := pkSchema()
	err := ValidateRow(ts, types.ProductValue{types.NewString("bad"), types.NewString("alice")})
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
	var tmErr *TypeMismatchError
	if !errors.As(err, &tmErr) {
		t.Fatalf("expected TypeMismatchError, got %T", err)
	}
}

// --- Table + constraints (E2 Story 2.2, E4) ---

func TestTableInsertGetDelete(t *testing.T) {
	tbl := NewTable(pkSchema())
	id := tbl.AllocRowID()
	row := mkRow(1, "alice")
	if err := tbl.InsertRow(id, row); err != nil {
		t.Fatal(err)
	}
	got, ok := tbl.GetRow(id)
	if !ok || got[1].AsString() != "alice" {
		t.Fatal("GetRow failed")
	}
	old, ok := tbl.DeleteRow(id)
	if !ok || old[1].AsString() != "alice" {
		t.Fatal("DeleteRow failed")
	}
	if tbl.RowCount() != 0 {
		t.Fatal("RowCount should be 0")
	}
}

func TestTablePKViolation(t *testing.T) {
	tbl := NewTable(pkSchema())
	row := mkRow(1, "alice")
	_ = tbl.InsertRow(tbl.AllocRowID(), row)
	err := tbl.InsertRow(tbl.AllocRowID(), mkRow(1, "bob"))
	if err == nil {
		t.Fatal("expected PK violation")
	}
	var pkErr *PrimaryKeyViolationError
	if !errors.As(err, &pkErr) {
		t.Fatalf("expected PrimaryKeyViolationError, got %T: %v", err, err)
	}
}

func TestTableSetSemantics(t *testing.T) {
	tbl := NewTable(noPKSchema())
	row := types.ProductValue{types.NewString("hello")}
	_ = tbl.InsertRow(tbl.AllocRowID(), row)
	err := tbl.InsertRow(tbl.AllocRowID(), types.ProductValue{types.NewString("hello")})
	if !errors.Is(err, ErrDuplicateRow) {
		t.Fatalf("expected ErrDuplicateRow, got %v", err)
	}
}

func TestTableDeleteReinsert(t *testing.T) {
	tbl := NewTable(pkSchema())
	row := mkRow(1, "alice")
	id := tbl.AllocRowID()
	_ = tbl.InsertRow(id, row)
	tbl.DeleteRow(id)
	if err := tbl.InsertRow(tbl.AllocRowID(), row); err != nil {
		t.Fatalf("reinsert after delete should succeed: %v", err)
	}
}

func TestTableScan(t *testing.T) {
	tbl := NewTable(pkSchema())
	for i := range 5 {
		_ = tbl.InsertRow(tbl.AllocRowID(), mkRow(uint64(i), "x"))
	}
	count := 0
	for range tbl.Scan() {
		count++
	}
	if count != 5 {
		t.Fatalf("Scan yielded %d, want 5", count)
	}
}

func TestAllocRowIDNeverResets(t *testing.T) {
	tbl := NewTable(pkSchema())
	id1 := tbl.AllocRowID()
	id2 := tbl.AllocRowID()
	if id2 <= id1 {
		t.Fatal("RowID must strictly increase")
	}
}

// --- Transaction tests (E5) ---

func buildTestState() (*CommittedState, schema.SchemaRegistry) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "players",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "name", Type: types.KindString},
		},
	})
	e, _ := b.Build(schema.EngineOptions{})
	reg := e.Registry()

	cs := NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, NewTable(ts))
	}
	return cs, reg
}

func TestTransactionInsertVisible(t *testing.T) {
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)

	id, err := tx.Insert(0, mkRow(1, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, id)
	if !ok || row[1].AsString() != "alice" {
		t.Fatal("inserted row should be visible in tx")
	}
}

func TestTransactionDeleteCollapse(t *testing.T) {
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)

	id, _ := tx.Insert(0, mkRow(1, "alice"))
	if err := tx.Delete(0, id); err != nil {
		t.Fatal(err)
	}
	// Insert-then-delete collapses — row gone from tx.
	_, ok := tx.GetRow(0, id)
	if ok {
		t.Fatal("deleted tx-insert should not be visible")
	}
}

func TestTransactionCommittedDelete(t *testing.T) {
	cs, reg := buildTestState()

	// Pre-populate committed state.
	tbl, _ := cs.Table(0)
	rid := tbl.AllocRowID()
	_ = tbl.InsertRow(rid, mkRow(1, "alice"))

	tx := NewTransaction(cs, reg)
	if err := tx.Delete(0, rid); err != nil {
		t.Fatal(err)
	}
	_, ok := tx.GetRow(0, rid)
	if ok {
		t.Fatal("deleted committed row should not be visible in tx")
	}
}

func TestTransactionUpdate(t *testing.T) {
	cs, reg := buildTestState()

	tbl, _ := cs.Table(0)
	rid := tbl.AllocRowID()
	_ = tbl.InsertRow(rid, mkRow(1, "alice"))

	tx := NewTransaction(cs, reg)
	newID, err := tx.Update(0, rid, mkRow(2, "bob"))
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, newID)
	if !ok || row[1].AsString() != "bob" {
		t.Fatal("updated row should be visible")
	}
	_, ok = tx.GetRow(0, rid)
	if ok {
		t.Fatal("old row should not be visible")
	}
}

// --- Commit tests (E6) ---

func TestCommitApplies(t *testing.T) {
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)

	tx.Insert(0, mkRow(1, "alice"))
	changeset, err := Commit(cs, tx)
	if err != nil {
		t.Fatal(err)
	}

	if changeset.IsEmpty() {
		t.Fatal("changeset should not be empty")
	}

	// Row should be in committed state.
	tbl, _ := cs.Table(0)
	if tbl.RowCount() != 1 {
		t.Fatal("committed table should have 1 row")
	}
}

func TestCommitNetEffectInsertDelete(t *testing.T) {
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)

	id, _ := tx.Insert(0, mkRow(1, "alice"))
	tx.Delete(0, id) // collapse

	changeset, err := Commit(cs, tx)
	if err != nil {
		t.Fatal(err)
	}
	if !changeset.IsEmpty() {
		t.Fatal("insert-then-delete should produce empty changeset")
	}
}

func TestCommitProducesLocalChangesetsAcrossTransactions(t *testing.T) {
	cs, reg := buildTestState()

	tx1 := NewTransaction(cs, reg)
	tx1.Insert(0, mkRow(1, "a"))
	changeset1, err := Commit(cs, tx1)
	if err != nil {
		t.Fatal(err)
	}

	tx2 := NewTransaction(cs, reg)
	tx2.Insert(0, mkRow(2, "b"))
	changeset2, err := Commit(cs, tx2)
	if err != nil {
		t.Fatal(err)
	}

	if changeset1 == nil || changeset2 == nil {
		t.Fatal("commit should return changesets for both transactions")
	}
	if changeset1.TxID != 0 || changeset2.TxID != 0 {
		t.Fatal("store commit should leave TxID assignment to executor")
	}
}

// --- Snapshot tests (E7) ---

func TestSnapshotPointInTime(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	_ = tbl.InsertRow(tbl.AllocRowID(), mkRow(1, "alice"))

	snap := cs.Snapshot()
	defer snap.Close()

	if snap.RowCount(0) != 1 {
		t.Fatal("snapshot should see 1 row")
	}
}

func TestSnapshotCloseSafe(t *testing.T) {
	cs, _ := buildTestState()
	snap := cs.Snapshot()
	snap.Close()
	snap.Close() // double close should not panic
}

// --- Sequence tests (E8) ---

func TestSequenceMonotonic(t *testing.T) {
	s := NewSequence()
	a := s.Next()
	b := s.Next()
	if b <= a {
		t.Fatal("sequence should be monotonic")
	}
}

func TestSequenceReset(t *testing.T) {
	s := NewSequence()
	s.Next()
	s.Reset(100)
	if s.Peek() != 100 {
		t.Fatalf("Peek after Reset = %d, want 100", s.Peek())
	}
}

// --- Recovery tests (E8) ---

func TestApplyChangeset(t *testing.T) {
	cs, _ := buildTestState()

	changeset := &Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*TableChangeset{
			0: {
				Inserts: []types.ProductValue{mkRow(1, "alice")},
			},
		},
	}

	if err := ApplyChangeset(cs, changeset); err != nil {
		t.Fatal(err)
	}

	tbl, _ := cs.Table(0)
	if tbl.RowCount() != 1 {
		t.Fatal("ApplyChangeset should insert 1 row")
	}
}

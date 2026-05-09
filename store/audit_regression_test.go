package store

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func buildNoPKState(t *testing.T) (*CommittedState, schema.SchemaRegistry) {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "logs",
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

func buildAutoIncrementState(t *testing.T) (*CommittedState, schema.SchemaRegistry) {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "jobs",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", Type: types.KindString},
		},
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

func buildUint8AutoIncrementState(t *testing.T) (*CommittedState, schema.SchemaRegistry) {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "jobs",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint8, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", Type: types.KindString},
		},
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

func bytesPrimaryKeySchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   10,
		Name: "blobs",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindBytes},
			{Index: 1, Name: "name", Type: types.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "blobs_pk", Columns: []int{0}, Unique: true, Primary: true},
		},
	}
}

func arrayStringUniqueSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   11,
		Name: "tagged",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "tags", Type: types.KindArrayString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "tagged_pk", Columns: []int{0}, Unique: true, Primary: true},
			{ID: 1, Name: "tagged_tags", Columns: []int{1}, Unique: true},
		},
	}
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

func TestTransactionUndeleteRejectsTxLocalPrimaryKeyConflict(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	originalID := tbl.AllocRowID()
	original := mkRow(1, "alice")
	if err := tbl.InsertRow(originalID, original); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, reg)
	if err := tx.Delete(0, originalID); err != nil {
		t.Fatal(err)
	}
	replacementID, err := tx.Insert(0, mkRow(1, "replacement"))
	if err != nil {
		t.Fatalf("replacement insert should be allowed after delete: %v", err)
	}
	_, err = tx.Insert(0, original)
	if !errors.Is(err, ErrPrimaryKeyViolation) {
		t.Fatalf("undelete conflict err = %v, want ErrPrimaryKeyViolation", err)
	}
	if !tx.tx.IsDeleted(0, originalID) {
		t.Fatal("failed undelete must leave original row deleted")
	}
	row, ok := tx.GetRow(0, replacementID)
	if !ok || row[1].AsString() != "replacement" {
		t.Fatalf("replacement row should remain visible after failed undelete: ok=%v row=%v", ok, row)
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

func TestCommitAdvancesRowIDAllocation(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	beforeNextID := tbl.NextID()

	tx := NewTransaction(cs, reg)
	rowID, err := tx.Insert(0, mkRow(1, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	if rowID != beforeNextID {
		t.Fatalf("insert RowID = %d, want current next %d", rowID, beforeNextID)
	}
	if tbl.NextID() != beforeNextID {
		t.Fatalf("NextID before commit = %d, want unchanged %d", tbl.NextID(), beforeNextID)
	}

	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}
	if tbl.NextID() != beforeNextID+1 {
		t.Fatalf("NextID after commit = %d, want %d", tbl.NextID(), beforeNextID+1)
	}
}

func TestCommitFailureDoesNotAdvanceRowIDAllocation(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	beforeNextID := tbl.NextID()

	tx := NewTransaction(cs, reg)
	if _, err := tx.Insert(0, mkRow(1, "valid")); err != nil {
		t.Fatal(err)
	}
	// Bypass transaction-time validation so commit revalidation fails after
	// the transaction has allocated a RowID.
	tx.TxState().AddInsert(0, 999, types.ProductValue{types.NewUint64(2)})

	_, err := Commit(cs, tx)
	if !errors.Is(err, ErrRowShapeMismatch) {
		t.Fatalf("commit error = %v, want ErrRowShapeMismatch", err)
	}
	if tbl.NextID() != beforeNextID {
		t.Fatalf("NextID after failed commit = %d, want unchanged %d", tbl.NextID(), beforeNextID)
	}
	if tbl.RowCount() != 0 {
		t.Fatalf("row count after failed commit = %d, want 0", tbl.RowCount())
	}
}

func TestCommitAtomicityFailureLeavesCommittedStateUnchanged(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	keptID := tbl.AllocRowID()
	deletedID := tbl.AllocRowID()
	keptRow := mkRow(1, "kept")
	deletedRow := mkRow(2, "to-delete")
	if err := tbl.InsertRow(keptID, keptRow); err != nil {
		t.Fatal(err)
	}
	if err := tbl.InsertRow(deletedID, deletedRow); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, reg)
	if err := tx.Delete(0, deletedID); err != nil {
		t.Fatal(err)
	}
	// Bypass transaction-time validation so commit revalidation catches the
	// duplicate key before mutating committed state.
	tx.TxState().AddInsert(0, 999, mkRow(77, "first-duplicate"))
	tx.TxState().AddInsert(0, 1000, mkRow(77, "second-duplicate"))

	_, err := Commit(cs, tx)
	if err == nil {
		t.Fatal("expected commit to fail on duplicate primary key")
	}
	if !errors.Is(err, ErrPrimaryKeyViolation) {
		t.Fatalf("commit error = %v, want ErrPrimaryKeyViolation", err)
	}

	if tbl.RowCount() != 2 {
		t.Fatalf("row count after failed commit = %d, want 2", tbl.RowCount())
	}
	if row, ok := tbl.GetRow(keptID); !ok || !row.Equal(keptRow) {
		t.Fatalf("kept row changed after failed commit: ok=%v row=%v", ok, row)
	}
	if row, ok := tbl.GetRow(deletedID); !ok || !row.Equal(deletedRow) {
		t.Fatalf("deleted row missing after failed commit: ok=%v row=%v", ok, row)
	}
	if _, ok := tbl.GetRow(999); ok {
		t.Fatal("failed commit must not leave first staged insert behind")
	}
	if _, ok := tbl.GetRow(1000); ok {
		t.Fatal("failed commit must not leave second staged insert behind")
	}
}

func TestCommitDuplicateRowIDRevalidationLeavesCommittedStateUnchanged(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	keptID := tbl.AllocRowID()
	deletedID := tbl.AllocRowID()
	keptRow := mkRow(1, "kept")
	deletedRow := mkRow(2, "to-delete")
	if err := tbl.InsertRow(keptID, keptRow); err != nil {
		t.Fatal(err)
	}
	if err := tbl.InsertRow(deletedID, deletedRow); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, reg)
	if err := tx.Delete(0, deletedID); err != nil {
		t.Fatal(err)
	}
	tx.TxState().AddInsert(0, keptID, mkRow(3, "duplicate-row-id"))

	_, err := Commit(cs, tx)
	if !errors.Is(err, ErrDuplicateRowID) {
		t.Fatalf("commit error = %v, want ErrDuplicateRowID", err)
	}

	if tbl.RowCount() != 2 {
		t.Fatalf("row count after failed duplicate-row-id commit = %d, want 2", tbl.RowCount())
	}
	if row, ok := tbl.GetRow(keptID); !ok || !row.Equal(keptRow) {
		t.Fatalf("kept row changed after failed duplicate-row-id commit: ok=%v row=%v", ok, row)
	}
	if row, ok := tbl.GetRow(deletedID); !ok || !row.Equal(deletedRow) {
		t.Fatalf("deleted row missing after failed duplicate-row-id commit: ok=%v row=%v", ok, row)
	}
}

func TestCommitMissingDeleteTargetReturnsErrorWithoutMutation(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	rid := tbl.AllocRowID()
	row := mkRow(1, "alice")
	if err := tbl.InsertRow(rid, row); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, reg)
	tx.TxState().AddDelete(0, rid+99)

	_, err := Commit(cs, tx)
	if err == nil {
		t.Fatal("expected commit to fail on missing delete target")
	}
	if !errors.Is(err, ErrRowNotFound) {
		t.Fatalf("commit error = %v, want ErrRowNotFound", err)
	}
	if tbl.RowCount() != 1 {
		t.Fatalf("row count after failed delete commit = %d, want 1", tbl.RowCount())
	}
	if got, ok := tbl.GetRow(rid); !ok || !got.Equal(row) {
		t.Fatalf("committed row changed after failed delete commit: ok=%v row=%v", ok, got)
	}
}

func TestCommitRevalidatesMalformedTxStateInsertWithoutMutation(t *testing.T) {
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)
	tx.TxState().AddInsert(0, 1, types.ProductValue{types.NewUint64(1)})

	_, err := Commit(cs, tx)
	if !errors.Is(err, ErrRowShapeMismatch) {
		t.Fatalf("commit error = %v, want ErrRowShapeMismatch", err)
	}
	tbl, _ := cs.Table(0)
	if got := tbl.RowCount(); got != 0 {
		t.Fatalf("row count after failed malformed commit = %d, want 0", got)
	}
}

func TestCommitRevalidatesMalformedNoPKTxStateInsertWithoutMutation(t *testing.T) {
	cs, reg := buildNoPKState(t)
	tx := NewTransaction(cs, reg)
	tx.TxState().AddInsert(0, 1, types.ProductValue{})

	_, err := Commit(cs, tx)
	if !errors.Is(err, ErrRowShapeMismatch) {
		t.Fatalf("commit error = %v, want ErrRowShapeMismatch", err)
	}
	tbl, _ := cs.Table(0)
	if got := tbl.RowCount(); got != 0 {
		t.Fatalf("row count after failed malformed no-PK commit = %d, want 0", got)
	}
}

func TestCommittedStateRegisterAndLookupAreRaceFree(t *testing.T) {
	cs := NewCommittedState()
	tables := []*Table{NewTable(pkSchema()), NewTable(noPKSchema())}
	registered := make(chan struct{}, len(tables))
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			for id := range []schema.TableID{0, 1} {
				cs.Table(schema.TableID(id))
			}
			_ = cs.TableIDs()
		}
	}()

	for id, tbl := range tables {
		go func(id schema.TableID, tbl *Table) {
			cs.RegisterTable(id, tbl)
			registered <- struct{}{}
		}(schema.TableID(id), tbl)
	}
	for range tables {
		<-registered
	}
	close(stop)
	<-done

	for id := range []schema.TableID{0, 1} {
		if _, ok := cs.Table(schema.TableID(id)); !ok {
			t.Fatalf("table %d should be registered", id)
		}
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

func TestLeakedSnapshotEventuallyStopsBlockingCommitAfterGC(t *testing.T) {
	cs, reg := buildTestState()
	func() {
		_ = cs.Snapshot()
	}()

	tx := NewTransaction(cs, reg)
	if _, err := tx.Insert(0, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Commit(cs, tx)
		done <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		runtime.Gosched()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
	}

	t.Fatal("commit remained blocked after leaked snapshot became unreachable and GC ran")
}

func TestTableInsertDetachesFromCaller(t *testing.T) {
	tbl := NewTable(pkSchema())
	id := tbl.AllocRowID()
	row := mkRow(1, "alice")
	if err := tbl.InsertRow(id, row); err != nil {
		t.Fatal(err)
	}

	// Mutate caller's slice after insert.
	row[1] = types.NewString("mutated")

	got, ok := tbl.GetRow(id)
	if !ok {
		t.Fatal("row should exist")
	}
	if got[1].AsString() != "alice" {
		t.Fatalf("stored row mutated by caller: got %q, want %q", got[1].AsString(), "alice")
	}
}

func TestTableInsertDetachesBytesIndexKeyFromCallerOwnedValue(t *testing.T) {
	tbl := NewTable(bytesPrimaryKeySchema())
	payload := []byte{1, 2, 3}
	id := tbl.AllocRowID()
	if err := tbl.InsertRow(id, types.ProductValue{
		types.NewBytesOwned(payload),
		types.NewString("first"),
	}); err != nil {
		t.Fatal(err)
	}

	payload[0] = 9

	pk := tbl.PrimaryIndex()
	if pk == nil {
		t.Fatal("primary index missing")
	}
	originalKey := NewIndexKey(types.NewBytes([]byte{1, 2, 3}))
	if got := pk.Seek(originalKey); len(got) != 1 || got[0] != id {
		t.Fatalf("primary index after caller mutation = %v, want [%d]", got, id)
	}

	err := tbl.InsertRow(tbl.AllocRowID(), types.ProductValue{
		types.NewBytes([]byte{1, 2, 3}),
		types.NewString("duplicate"),
	})
	if !errors.Is(err, ErrPrimaryKeyViolation) {
		t.Fatalf("duplicate bytes primary key insert err = %v, want ErrPrimaryKeyViolation", err)
	}
}

func TestTableInsertDetachesArrayStringIndexKeyFromCallerOwnedValue(t *testing.T) {
	tbl := NewTable(arrayStringUniqueSchema())
	tags := []string{"red", "blue"}
	id := tbl.AllocRowID()
	if err := tbl.InsertRow(id, types.ProductValue{
		types.NewUint64(1),
		types.NewArrayStringOwned(tags),
	}); err != nil {
		t.Fatal(err)
	}

	tags[0] = "green"

	unique := tbl.IndexByID(schema.IndexID(1))
	if unique == nil {
		t.Fatal("unique index missing")
	}
	originalKey := NewIndexKey(types.NewArrayString([]string{"red", "blue"}))
	if got := unique.Seek(originalKey); len(got) != 1 || got[0] != id {
		t.Fatalf("array-string unique index after caller mutation = %v, want [%d]", got, id)
	}

	err := tbl.InsertRow(tbl.AllocRowID(), types.ProductValue{
		types.NewUint64(2),
		types.NewArrayString([]string{"red", "blue"}),
	})
	if !errors.Is(err, ErrUniqueConstraintViolation) {
		t.Fatalf("duplicate array-string unique insert err = %v, want ErrUniqueConstraintViolation", err)
	}
}

func TestTableInsertRejectsDuplicateRowIDBeforeIndexMutation(t *testing.T) {
	tbl := NewTable(pkSchema())
	id := tbl.AllocRowID()
	if err := tbl.InsertRow(id, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	err := tbl.InsertRow(id, mkRow(2, "bob"))
	if !errors.Is(err, ErrDuplicateRowID) {
		t.Fatalf("duplicate row id insert err = %v, want ErrDuplicateRowID", err)
	}
	got, ok := tbl.GetRow(id)
	if !ok {
		t.Fatal("original row should still exist")
	}
	if got[0].AsUint64() != 1 || got[1].AsString() != "alice" {
		t.Fatalf("duplicate row id insert mutated row: got %#v", got)
	}

	pk := tbl.PrimaryIndex()
	if pk == nil {
		t.Fatal("primary index missing")
	}
	if gotIDs := pk.Seek(NewIndexKey(types.NewUint64(1))); len(gotIDs) != 1 || gotIDs[0] != id {
		t.Fatalf("original primary index entry = %v, want [%d]", gotIDs, id)
	}
	if gotIDs := pk.Seek(NewIndexKey(types.NewUint64(2))); len(gotIDs) != 0 {
		t.Fatalf("duplicate row id insert left stale primary index entry for new key: %v", gotIDs)
	}
}

func TestTableGetRowReturnsDetachedCopy(t *testing.T) {
	tbl := NewTable(pkSchema())
	id := tbl.AllocRowID()
	if err := tbl.InsertRow(id, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	// Mutate row returned by GetRow.
	got, _ := tbl.GetRow(id)
	got[1] = types.NewString("mutated-via-getrow")

	// Subsequent read should be unaffected.
	got2, _ := tbl.GetRow(id)
	if got2[1].AsString() != "alice" {
		t.Fatalf("stored row mutated via GetRow: got %q, want %q", got2[1].AsString(), "alice")
	}
}

func TestTxStateAddInsertDetachesFromCaller(t *testing.T) {
	tx := NewTxState()
	row := mkRow(1, "alice")
	tx.AddInsert(0, 1, row)

	// Mutate caller's slice.
	row[1] = types.NewString("mutated")

	stored := tx.Inserts(0)[1]
	if stored[1].AsString() != "alice" {
		t.Fatalf("tx insert mutated by caller: got %q, want %q", stored[1].AsString(), "alice")
	}
}

func TestTxStateAccessorsReturnDetachedMaps(t *testing.T) {
	tx := NewTxState()
	tx.AddInsert(0, 1, mkRow(1, "alice"))
	tx.AddDelete(0, 2)

	inserts := tx.Inserts(0)
	inserts[1][1] = types.NewString("mutated")
	delete(inserts, 1)
	allInserts := tx.AllInserts()
	delete(allInserts, 0)
	deletes := tx.Deletes(0)
	delete(deletes, 2)
	allDeletes := tx.AllDeletes()
	delete(allDeletes, 0)

	stored := tx.Inserts(0)[1]
	if stored[1].AsString() != "alice" {
		t.Fatalf("tx insert mutated through accessor: got %q, want alice", stored[1].AsString())
	}
	if !tx.IsDeleted(0, 2) {
		t.Fatal("tx delete removed through accessor")
	}
}

func TestTxStateRemovesEmptyTableBuffers(t *testing.T) {
	tx := NewTxState()
	tx.AddInsert(0, 1, mkRow(1, "alice"))
	tx.RemoveInsert(0, 1)
	if got := tx.AllInserts(); len(got) != 0 {
		t.Fatalf("AllInserts after removing last insert = %v, want empty", got)
	}

	tx.AddDelete(0, 2)
	tx.CancelDelete(0, 2)
	if got := tx.AllDeletes(); len(got) != 0 {
		t.Fatalf("AllDeletes after canceling last delete = %v, want empty", got)
	}
}

func TestRollbackBlocksPostRollbackInsert(t *testing.T) {
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)
	Rollback(tx)

	_, err := tx.Insert(0, mkRow(1, "alice"))
	if !errors.Is(err, ErrTransactionRolledBack) {
		t.Fatalf("post-rollback Insert: got %v, want ErrTransactionRolledBack", err)
	}
}

func TestRollbackBlocksPostRollbackDelete(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	rid := tbl.AllocRowID()
	_ = tbl.InsertRow(rid, mkRow(1, "alice"))

	tx := NewTransaction(cs, reg)
	Rollback(tx)

	err := tx.Delete(0, rid)
	if !errors.Is(err, ErrTransactionRolledBack) {
		t.Fatalf("post-rollback Delete: got %v, want ErrTransactionRolledBack", err)
	}
}

func TestRollbackBlocksPostRollbackUpdate(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	rid := tbl.AllocRowID()
	_ = tbl.InsertRow(rid, mkRow(1, "alice"))

	tx := NewTransaction(cs, reg)
	Rollback(tx)

	_, err := tx.Update(0, rid, mkRow(2, "bob"))
	if !errors.Is(err, ErrTransactionRolledBack) {
		t.Fatalf("post-rollback Update: got %v, want ErrTransactionRolledBack", err)
	}
}

func TestRollbackBlocksPostRollbackCommit(t *testing.T) {
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)
	if _, err := tx.Insert(0, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}
	Rollback(tx)

	_, err := Commit(cs, tx)
	if !errors.Is(err, ErrTransactionRolledBack) {
		t.Fatalf("post-rollback Commit: got %v, want ErrTransactionRolledBack", err)
	}
}

func TestRollbackGetRowReturnsNil(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	rid := tbl.AllocRowID()
	_ = tbl.InsertRow(rid, mkRow(1, "alice"))

	tx := NewTransaction(cs, reg)
	Rollback(tx)

	row, ok := tx.GetRow(0, rid)
	if ok || row != nil {
		t.Fatalf("post-rollback GetRow should return nil, false; got %v, %v", row, ok)
	}
}

func TestRollbackScanTableYieldsNothing(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	_ = tbl.InsertRow(tbl.AllocRowID(), mkRow(1, "alice"))

	tx := NewTransaction(cs, reg)
	Rollback(tx)

	count := 0
	for range tx.ScanTable(0) {
		count++
	}
	if count != 0 {
		t.Fatalf("post-rollback ScanTable yielded %d rows, want 0", count)
	}
}

func TestRollbackLeavesCommittedStateUnchanged(t *testing.T) {
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	beforeNextID := tbl.NextID()
	tx := NewTransaction(cs, reg)
	if _, err := tx.Insert(0, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}
	Rollback(tx)

	if tbl.RowCount() != 0 {
		t.Fatalf("rollback should not mutate committed state; row count = %d", tbl.RowCount())
	}
	if tbl.NextID() != beforeNextID {
		t.Fatalf("rollback should not advance NextID: got %d, want %d", tbl.NextID(), beforeNextID)
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

func TestCommittedSnapshotIndexScanByPrimaryKey(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	rowID := tbl.AllocRowID()
	if err := tbl.InsertRow(rowID, mkRow(7, "alice")); err != nil {
		t.Fatal(err)
	}

	snap := cs.Snapshot()
	defer snap.Close()

	var got []types.ProductValue
	for _, row := range snap.IndexScan(0, 0, types.NewUint64(7)) {
		got = append(got, row)
	}
	if len(got) != 1 {
		t.Fatalf("IndexScan result len = %d, want 1", len(got))
	}
	if got[0][0].AsUint64() != 7 || got[0][1].AsString() != "alice" {
		t.Fatalf("IndexScan row = %#v, want pk=7 name=alice", got[0])
	}
}

func TestCommittedSnapshotIndexScanMissingValueReturnsEmpty(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	if err := tbl.InsertRow(tbl.AllocRowID(), mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	snap := cs.Snapshot()
	defer snap.Close()

	count := 0
	for range snap.IndexScan(0, 0, types.NewUint64(99)) {
		count++
	}
	if count != 0 {
		t.Fatalf("missing-value IndexScan yielded %d rows, want 0", count)
	}
}

func TestCommittedSnapshotIndexRangeBoundSemantics(t *testing.T) {
	cs, reg := buildTestState()
	tx := NewTransaction(cs, reg)
	for _, row := range []types.ProductValue{
		mkRow(1, "alice"),
		mkRow(2, "bob"),
		mkRow(3, "carol"),
		mkRow(4, "dave"),
	} {
		if _, err := tx.Insert(0, row); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}

	snap := cs.Snapshot()
	defer snap.Close()

	var got []uint64
	for _, row := range snap.IndexRange(0, 0, Inclusive(types.NewUint64(2)), Exclusive(types.NewUint64(4))) {
		got = append(got, row[0].AsUint64())
	}
	want := []uint64{2, 3}
	if len(got) != len(want) {
		t.Fatalf("bounded IndexRange len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bounded IndexRange = %v, want %v", got, want)
		}
	}

	got = nil
	for _, row := range snap.IndexRange(0, 0, UnboundedLow(), Inclusive(types.NewUint64(2))) {
		got = append(got, row[0].AsUint64())
	}
	want = []uint64{1, 2}
	if len(got) != len(want) {
		t.Fatalf("unbounded-lower IndexRange len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unbounded-lower IndexRange = %v, want %v", got, want)
		}
	}
}

func TestTransactionInsertAutoIncrementAssignsSequentialValues(t *testing.T) {
	cs, reg := buildAutoIncrementState(t)
	tx := NewTransaction(cs, reg)

	id1, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("job-a")})
	if err != nil {
		t.Fatal(err)
	}
	row1, ok := tx.GetRow(0, id1)
	if !ok {
		t.Fatal("first inserted row should be visible")
	}
	if got := row1[0].AsUint64(); got != 1 {
		t.Fatalf("first autoincrement value = %d, want 1", got)
	}

	id2, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("job-b")})
	if err != nil {
		t.Fatal(err)
	}
	row2, ok := tx.GetRow(0, id2)
	if !ok {
		t.Fatal("second inserted row should be visible")
	}
	if got := row2[0].AsUint64(); got != 2 {
		t.Fatalf("second autoincrement value = %d, want 2", got)
	}

	id3, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("job-c")})
	if err != nil {
		t.Fatal(err)
	}
	row3, ok := tx.GetRow(0, id3)
	if !ok {
		t.Fatal("third inserted row should be visible")
	}
	if got := row3[0].AsUint64(); got != 3 {
		t.Fatalf("third autoincrement value = %d, want 3", got)
	}
}

func TestTransactionInsertAutoIncrementUint8AssignsValue(t *testing.T) {
	cs, reg := buildUint8AutoIncrementState(t)
	tx := NewTransaction(cs, reg)

	id, err := tx.Insert(0, types.ProductValue{types.NewUint8(0), types.NewString("job-a")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, id)
	if !ok {
		t.Fatal("inserted row should be visible")
	}
	if got := row[0].AsUint8(); got != 1 {
		t.Fatalf("uint8 autoincrement value = %d, want 1", got)
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}
	tbl, _ := cs.Table(0)
	if seq, has := tbl.SequenceValue(); !has || seq != 2 {
		t.Fatalf("SequenceValue after commit = (%d, %v), want (2, true)", seq, has)
	}
}

func TestTransactionInsertAutoIncrementPreservesExplicitValue(t *testing.T) {
	cs, reg := buildAutoIncrementState(t)
	tx := NewTransaction(cs, reg)

	id, err := tx.Insert(0, types.ProductValue{types.NewUint64(42), types.NewString("job-explicit")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, id)
	if !ok {
		t.Fatal("explicit-value inserted row should be visible")
	}
	if got := row[0].AsUint64(); got != 42 {
		t.Fatalf("explicit autoincrement value = %d, want 42", got)
	}
}

func TestTransactionInsertAutoIncrementExplicitValueAdvancesSequence(t *testing.T) {
	cs, reg := buildAutoIncrementState(t)
	tx := NewTransaction(cs, reg)

	if _, err := tx.Insert(0, types.ProductValue{types.NewUint64(42), types.NewString("job-explicit")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}
	tbl, _ := cs.Table(0)
	if seq, has := tbl.SequenceValue(); !has || seq != 43 {
		t.Fatalf("SequenceValue after explicit commit = (%d, %v), want (43, true)", seq, has)
	}

	next := NewTransaction(cs, reg)
	id, err := next.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("job-generated")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := next.GetRow(0, id)
	if !ok {
		t.Fatal("generated row should be visible")
	}
	if got := row[0].AsUint64(); got != 43 {
		t.Fatalf("generated autoincrement value after explicit insert = %d, want 43", got)
	}
}

func TestRollbackDiscardsAutoIncrementSequence(t *testing.T) {
	cs, reg := buildAutoIncrementState(t)
	tx := NewTransaction(cs, reg)

	if _, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("rolled-back")}); err != nil {
		t.Fatal(err)
	}
	Rollback(tx)

	next := NewTransaction(cs, reg)
	id, err := next.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("committed")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := next.GetRow(0, id)
	if !ok {
		t.Fatal("post-rollback inserted row should be visible")
	}
	if got := row[0].AsUint64(); got != 1 {
		t.Fatalf("post-rollback autoincrement value = %d, want 1", got)
	}
}

func TestTableSequenceStateAccessorsRoundTrip(t *testing.T) {
	cs, _ := buildAutoIncrementState(t)
	tbl, ok := cs.Table(0)
	if !ok {
		t.Fatal("expected autoincrement table")
	}

	if seq, has := tbl.SequenceValue(); !has || seq != 1 {
		t.Fatalf("initial SequenceValue = (%d, %v), want (1, true)", seq, has)
	}

	tbl.SetSequenceValue(9)
	if seq, has := tbl.SequenceValue(); !has || seq != 9 {
		t.Fatalf("restored SequenceValue = (%d, %v), want (9, true)", seq, has)
	}

	tx := NewTransaction(cs, nil)
	id, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("after-restore")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, id)
	if !ok {
		t.Fatal("post-restore inserted row should be visible")
	}
	if got := row[0].AsUint64(); got != 9 {
		t.Fatalf("post-restore autoincrement value = %d, want 9", got)
	}
	if seq, has := tbl.SequenceValue(); !has || seq != 9 {
		t.Fatalf("next SequenceValue before commit = (%d, %v), want (9, true)", seq, has)
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}
	if seq, has := tbl.SequenceValue(); !has || seq != 10 {
		t.Fatalf("next SequenceValue after commit = (%d, %v), want (10, true)", seq, has)
	}
}

func TestTransactionInsertAutoIncrementUint64ExhaustedSequenceFails(t *testing.T) {
	cs, reg := buildAutoIncrementState(t)
	tbl, ok := cs.Table(0)
	if !ok {
		t.Fatal("expected autoincrement table")
	}
	tbl.SetSequenceValue(^uint64(0))

	tx := NewTransaction(cs, reg)
	if _, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("exhausted")}); !errors.Is(err, schema.ErrSequenceOverflow) {
		t.Fatalf("Insert at exhausted sequence error = %v, want ErrSequenceOverflow", err)
	}

	if seq, has := tbl.SequenceValue(); !has || seq != ^uint64(0) {
		t.Fatalf("SequenceValue after failed insert = (%d, %v), want max uint64 and true", seq, has)
	}
}

func TestTransactionInsertExhaustedRowIDFails(t *testing.T) {
	cs, reg := buildNoPKState(t)
	tbl, ok := cs.Table(0)
	if !ok {
		t.Fatal("expected logs table")
	}
	tbl.SetNextID(^types.RowID(0))

	tx := NewTransaction(cs, reg)
	if _, err := tx.Insert(0, types.ProductValue{types.NewString("exhausted")}); !errors.Is(err, ErrRowIDOverflow) {
		t.Fatalf("Insert at exhausted RowID error = %v, want ErrRowIDOverflow", err)
	}

	if next := tbl.NextID(); next != ^types.RowID(0) {
		t.Fatalf("NextID after failed insert = %d, want max RowID", next)
	}
}

func TestTransactionInsertExhaustedRowIDDoesNotAdvanceAutoIncrement(t *testing.T) {
	cs, reg := buildAutoIncrementState(t)
	tbl, ok := cs.Table(0)
	if !ok {
		t.Fatal("expected autoincrement table")
	}
	tbl.SetNextID(^types.RowID(0))
	tbl.SetSequenceValue(7)

	tx := NewTransaction(cs, reg)
	if _, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("exhausted")}); !errors.Is(err, ErrRowIDOverflow) {
		t.Fatalf("Insert at exhausted RowID error = %v, want ErrRowIDOverflow", err)
	}

	if seq, has := tbl.SequenceValue(); !has || seq != 7 {
		t.Fatalf("SequenceValue after failed insert = (%d, %v), want (7, true)", seq, has)
	}
}

func TestApplyChangesetExhaustedRowIDFails(t *testing.T) {
	cs, _ := buildNoPKState(t)
	tbl, ok := cs.Table(0)
	if !ok {
		t.Fatal("expected logs table")
	}
	tbl.SetNextID(^types.RowID(0))

	err := ApplyChangeset(cs, &Changeset{
		Tables: map[schema.TableID]*TableChangeset{
			0: {
				TableID:   0,
				TableName: "logs",
				Inserts:   []types.ProductValue{{types.NewString("replay")}},
			},
		},
	})
	if !errors.Is(err, ErrRowIDOverflow) {
		t.Fatalf("ApplyChangeset at exhausted RowID error = %v, want ErrRowIDOverflow", err)
	}

	if next := tbl.NextID(); next != ^types.RowID(0) {
		t.Fatalf("NextID after failed replay = %d, want max RowID", next)
	}
}

func TestApplyChangesetAutoIncrementUint8AdvancesSequence(t *testing.T) {
	cs, _ := buildUint8AutoIncrementState(t)

	err := ApplyChangeset(cs, &Changeset{
		Tables: map[schema.TableID]*TableChangeset{
			0: {
				TableID:   0,
				TableName: "jobs",
				Inserts: []types.ProductValue{
					{types.NewUint8(1), types.NewString("replay")},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tbl, _ := cs.Table(0)
	if seq, has := tbl.SequenceValue(); !has || seq != 2 {
		t.Fatalf("SequenceValue after replay = (%d, %v), want (2, true)", seq, has)
	}
}

func TestApplyChangesetAutoIncrementExplicitValueAdvancesSequence(t *testing.T) {
	cs, _ := buildAutoIncrementState(t)

	err := ApplyChangeset(cs, &Changeset{
		Tables: map[schema.TableID]*TableChangeset{
			0: {
				TableID:   0,
				TableName: "jobs",
				Inserts: []types.ProductValue{
					{types.NewUint64(42), types.NewString("replay-explicit")},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tbl, _ := cs.Table(0)
	if seq, has := tbl.SequenceValue(); !has || seq != 43 {
		t.Fatalf("SequenceValue after explicit replay = (%d, %v), want (43, true)", seq, has)
	}
}

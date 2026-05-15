package store

import (
	"errors"
	"slices"
	"strings"
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

func mustUUID(t *testing.T, s string) types.Value {
	t.Helper()
	v, err := types.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return v
}

func mustJSON(t *testing.T, raw string) types.Value {
	t.Helper()
	v, err := types.NewJSON([]byte(raw))
	if err != nil {
		t.Fatalf("NewJSON(%q): %v", raw, err)
	}
	return v
}

func uuidIndexedSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   2,
		Name: "entities",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUUID},
			{Index: 1, Name: "owner_id", Type: schema.KindUUID},
			{Index: 2, Name: "name", Type: schema.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "entities_pk", Columns: []int{0}, Unique: true, Primary: true},
			{ID: 1, Name: "entities_owner", Columns: []int{1}},
		},
	}
}

func uuidRow(id, owner types.Value, name string) types.ProductValue {
	return types.ProductValue{id, owner, types.NewString(name)}
}

func jsonIndexedSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   3,
		Name: "documents",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindJSON},
			{Index: 1, Name: "metadata", Type: schema.KindJSON},
			{Index: 2, Name: "name", Type: schema.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "documents_pk", Columns: []int{0}, Unique: true, Primary: true},
			{ID: 1, Name: "documents_metadata", Columns: []int{1}},
		},
	}
}

func jsonRow(id, metadata types.Value, name string) types.ProductValue {
	return types.ProductValue{id, metadata, types.NewString(name)}
}

func compositeIndexedSchema(unique bool) *schema.TableSchema {
	return &schema.TableSchema{
		ID:   0,
		Name: "scores",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "guild", Type: types.KindString},
			{Index: 2, Name: "score", Type: types.KindUint64},
			{Index: 3, Name: "body", Type: types.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
			{ID: 1, Name: "guild_score", Columns: []int{1, 2}, Unique: unique},
		},
	}
}

func compositeRow(id uint64, guild string, score uint64, body string) types.ProductValue {
	return types.ProductValue{
		types.NewUint64(id),
		types.NewString(guild),
		types.NewUint64(score),
		types.NewString(body),
	}
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

func TestIndexKeyCompareNullBeforeNonNull(t *testing.T) {
	nullKey := NewIndexKey(types.NewNull(types.KindString))
	valueKey := NewIndexKey(types.NewString(""))
	if got := nullKey.Compare(valueKey); got >= 0 {
		t.Fatalf("null key compare = %d, want null before non-null", got)
	}
	if got := valueKey.Compare(nullKey); got <= 0 {
		t.Fatalf("non-null key compare = %d, want after null", got)
	}
}

func TestValidateRowNullableColumns(t *testing.T) {
	ts := &schema.TableSchema{
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "nickname", Type: types.KindString, Nullable: true},
		},
	}
	if err := ValidateRow(ts, types.ProductValue{types.NewUint64(1), types.NewNull(types.KindString)}); err != nil {
		t.Fatalf("ValidateRow nullable null: %v", err)
	}
	if err := ValidateRow(ts, types.ProductValue{types.NewUint64(1), types.NewNull(types.KindUint64)}); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("ValidateRow wrong null kind err = %v, want ErrTypeMismatch", err)
	}
	if err := ValidateRow(ts, types.ProductValue{types.NewNull(types.KindUint64), types.NewString("alice")}); !errors.Is(err, ErrNullNotAllowed) {
		t.Fatalf("ValidateRow non-nullable null err = %v, want ErrNullNotAllowed", err)
	}
}

func TestUniqueIndexTreatsNullAsValue(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   4,
		Name: "profiles",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "nickname", Type: types.KindString, Nullable: true},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "nickname_uniq", Columns: []int{1}, Unique: true},
		},
	}
	table := NewTable(ts)
	if err := table.InsertRow(1, types.ProductValue{types.NewUint64(1), types.NewNull(types.KindString)}); err != nil {
		t.Fatalf("InsertRow first null: %v", err)
	}
	err := table.InsertRow(2, types.ProductValue{types.NewUint64(2), types.NewNull(types.KindString)})
	if !errors.Is(err, ErrUniqueConstraintViolation) {
		t.Fatalf("InsertRow duplicate null err = %v, want unique violation", err)
	}
}

func TestIndexKeyConstructorCopiesParts(t *testing.T) {
	parts := []types.Value{types.NewString("a")}
	key := NewIndexKey(parts...)
	parts[0] = types.NewString("b")

	if got := key.Part(0).AsString(); got != "a" {
		t.Fatalf("IndexKey aliased constructor input: got %q, want a", got)
	}
}

func TestIndexKeyConstructorDetachesSliceBackedValues(t *testing.T) {
	buf := []byte{1, 2, 3}
	tags := []string{"red", "blue"}
	metadata := mustJSON(t, `{"b":2,"a":1}`)
	key := NewIndexKey(types.NewBytesOwned(buf), types.NewArrayStringOwned(tags), metadata)

	buf[0] = 9
	tags[0] = "green"
	metadata.JSONView()[1] = 'z'

	if got := key.Part(0).AsBytes(); !slices.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("bytes index key part = %v, want [1 2 3]", got)
	}
	if got := key.Part(1).AsArrayString(); !slices.Equal(got, []string{"red", "blue"}) {
		t.Fatalf("array-string index key part = %v, want [red blue]", got)
	}
	if got := string(key.Part(2).AsJSON()); got != `{"a":1,"b":2}` {
		t.Fatalf("JSON index key part = %s, want canonical payload", got)
	}
}

func TestIndexKeyPartReturnsDetachedSliceBackedValue(t *testing.T) {
	key := NewIndexKey(types.NewBytes([]byte{1, 2, 3}), mustJSON(t, `{"a":1}`))
	bytesPart := key.Part(0)
	bytesPart.BytesView()[0] = 9
	jsonPart := key.Part(1)
	jsonPart.JSONView()[1] = 'z'

	if got := key.Part(0).AsBytes(); !slices.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("index key mutated through Part result: got %v, want [1 2 3]", got)
	}
	if got := string(key.Part(1).AsJSON()); got != `{"a":1}` {
		t.Fatalf("JSON index key mutated through Part result: got %s", got)
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

func TestIndexKeyUUIDLexicographicByteOrdering(t *testing.T) {
	low := NewIndexKey(mustUUID(t, "00112233-4455-6677-8899-aabbccddee00"))
	high := NewIndexKey(mustUUID(t, "00112233-4455-6677-8899-aabbccddeeff"))
	if low.Compare(high) != -1 {
		t.Fatalf("UUID index key ordering = %d, want low < high", low.Compare(high))
	}
}

func TestIndexKeyDurationOrdering(t *testing.T) {
	low := NewIndexKey(types.NewDuration(-1))
	high := NewIndexKey(types.NewDuration(1))
	if low.Compare(high) != -1 {
		t.Fatalf("Duration index key ordering = %d, want -1 < 1", low.Compare(high))
	}
}

func TestIndexKeyJSONCanonicalOrdering(t *testing.T) {
	a := NewIndexKey(mustJSON(t, `{"b":2,"a":1}`))
	b := NewIndexKey(mustJSON(t, `{"a":1,"b":3}`))
	if a.Compare(b) != -1 {
		t.Fatalf("JSON index key ordering = %d, want canonical a < b", a.Compare(b))
	}
	if !a.Equal(NewIndexKey(mustJSON(t, `{"a":1,"b":2}`))) {
		t.Fatal("JSON index key should use canonical bytes for equality")
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

func TestBTreeInsertDuplicateRowIDIsIdempotent(t *testing.T) {
	bt := NewBTreeIndex()
	k := NewIndexKey(types.NewUint64(1))
	bt.Insert(k, 10)
	bt.Insert(k, 10)

	if got := bt.Seek(k); !slices.Equal(got, []types.RowID{10}) {
		t.Fatalf("Seek duplicate rowID = %v, want [10]", got)
	}
	if got := bt.Len(); got != 1 {
		t.Fatalf("Len after duplicate rowID insert = %d, want 1", got)
	}
}

func TestBTreeSeekReturnsDetachedRowIDs(t *testing.T) {
	bt := NewBTreeIndex()
	k := NewIndexKey(types.NewUint64(1))
	bt.Insert(k, 10)
	bt.Insert(k, 20)

	got := bt.Seek(k)
	got[0] = 99

	again := bt.Seek(k)
	if !slices.Equal(again, []types.RowID{10, 20}) {
		t.Fatalf("Seek result aliased index storage: got %v, want [10 20]", again)
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

func TestValidateRowRejectsInvalidUTF8String(t *testing.T) {
	ts := pkSchema()
	err := ValidateRow(ts, types.ProductValue{
		types.NewUint64(1),
		types.NewString(string([]byte{0xff})),
	})
	if !errors.Is(err, types.ErrInvalidUTF8) {
		t.Fatalf("ValidateRow invalid string err = %v, want ErrInvalidUTF8", err)
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("ValidateRow invalid string err = %v, want column name", err)
	}
}

func TestValidateRowRejectsInvalidUTF8ArrayStringElement(t *testing.T) {
	ts := arrayStringUniqueSchema()
	err := ValidateRow(ts, types.ProductValue{
		types.NewUint64(1),
		types.NewArrayString([]string{"ok", string([]byte{0xff})}),
	})
	if !errors.Is(err, types.ErrInvalidUTF8) {
		t.Fatalf("ValidateRow invalid array string err = %v, want ErrInvalidUTF8", err)
	}
	if !strings.Contains(err.Error(), "tags") {
		t.Fatalf("ValidateRow invalid array string err = %v, want column name", err)
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

func TestTableUUIDPrimaryAndSecondaryIndexes(t *testing.T) {
	tbl := NewTable(uuidIndexedSchema())
	id1 := mustUUID(t, "00112233-4455-6677-8899-aabbccddeeff")
	id2 := mustUUID(t, "00112233-4455-6677-8899-aabbccddee00")
	owner := mustUUID(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	row1 := uuidRow(id1, owner, "one")
	row2 := uuidRow(id2, owner, "two")

	rid1 := tbl.AllocRowID()
	if err := tbl.InsertRow(rid1, row1); err != nil {
		t.Fatal(err)
	}
	rid2 := tbl.AllocRowID()
	if err := tbl.InsertRow(rid2, row2); err != nil {
		t.Fatal(err)
	}

	if got := tbl.PrimaryIndex().Seek(NewIndexKey(id1)); !slices.Equal(got, []types.RowID{rid1}) {
		t.Fatalf("primary Seek(id1) = %v, want [%d]", got, rid1)
	}
	ownerIndex := tbl.IndexByID(1)
	if got := ownerIndex.Seek(NewIndexKey(owner)); !slices.Equal(got, []types.RowID{rid1, rid2}) {
		t.Fatalf("secondary Seek(owner) = %v, want [%d %d]", got, rid1, rid2)
	}

	deleted, ok := tbl.DeleteRow(rid1)
	if !ok || !deleted.Equal(row1) {
		t.Fatalf("DeleteRow = %v, %v; want row1,true", deleted, ok)
	}
	if got := tbl.PrimaryIndex().Seek(NewIndexKey(id1)); len(got) != 0 {
		t.Fatalf("primary Seek(id1) after delete = %v, want empty", got)
	}
	if got := ownerIndex.Seek(NewIndexKey(owner)); !slices.Equal(got, []types.RowID{rid2}) {
		t.Fatalf("secondary Seek(owner) after delete = %v, want [%d]", got, rid2)
	}
}

func TestTableJSONPrimaryAndSecondaryIndexes(t *testing.T) {
	tbl := NewTable(jsonIndexedSchema())
	id1 := mustJSON(t, `{"b":2,"a":1}`)
	id2 := mustJSON(t, `{"a":1,"b":3}`)
	metadata := mustJSON(t, `{"type":"note"}`)
	row1 := jsonRow(id1, metadata, "one")
	row2 := jsonRow(id2, metadata, "two")

	rid1 := tbl.AllocRowID()
	if err := tbl.InsertRow(rid1, row1); err != nil {
		t.Fatal(err)
	}
	rid2 := tbl.AllocRowID()
	if err := tbl.InsertRow(rid2, row2); err != nil {
		t.Fatal(err)
	}

	if got := tbl.PrimaryIndex().Seek(NewIndexKey(mustJSON(t, `{"a":1,"b":2}`))); !slices.Equal(got, []types.RowID{rid1}) {
		t.Fatalf("primary Seek(id1 canonical equivalent) = %v, want [%d]", got, rid1)
	}
	metadataIndex := tbl.IndexByID(1)
	if got := metadataIndex.Seek(NewIndexKey(metadata)); !slices.Equal(got, []types.RowID{rid1, rid2}) {
		t.Fatalf("secondary Seek(metadata) = %v, want [%d %d]", got, rid1, rid2)
	}

	if err := tbl.InsertRow(tbl.AllocRowID(), jsonRow(mustJSON(t, `{"a":1,"b":2}`), metadata, "duplicate")); !errors.Is(err, ErrPrimaryKeyViolation) {
		t.Fatalf("duplicate canonical JSON primary key err = %v, want ErrPrimaryKeyViolation", err)
	}
}

func TestTableCompositeUniqueIndexCommittedConflict(t *testing.T) {
	tbl := NewTable(compositeIndexedSchema(true))
	if err := tbl.InsertRow(tbl.AllocRowID(), compositeRow(1, "red", 10, "first")); err != nil {
		t.Fatal(err)
	}
	if err := tbl.InsertRow(tbl.AllocRowID(), compositeRow(2, "red", 11, "same guild different score")); err != nil {
		t.Fatalf("same first key part should be allowed: %v", err)
	}
	if err := tbl.InsertRow(tbl.AllocRowID(), compositeRow(3, "blue", 10, "same score different guild")); err != nil {
		t.Fatalf("same second key part should be allowed: %v", err)
	}

	err := tbl.InsertRow(tbl.AllocRowID(), compositeRow(4, "red", 10, "duplicate combined key"))
	if !errors.Is(err, ErrUniqueConstraintViolation) {
		t.Fatalf("duplicate composite key err = %v, want ErrUniqueConstraintViolation", err)
	}
	idx := tbl.IndexByID(1)
	if got := idx.Seek(NewIndexKey(types.NewString("red"), types.NewUint64(10))); len(got) != 1 {
		t.Fatalf("composite unique index after rejected insert = %v, want one row", got)
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

func TestTransactionUpdateMaintainsUUIDIndexesOnCommit(t *testing.T) {
	cs := NewCommittedState()
	ts := uuidIndexedSchema()
	tbl := NewTable(ts)
	cs.RegisterTable(2, tbl)

	id1 := mustUUID(t, "00112233-4455-6677-8899-aabbccddeeff")
	id2 := mustUUID(t, "00112233-4455-6677-8899-aabbccddee00")
	owner1 := mustUUID(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	owner2 := mustUUID(t, "bbbbbbbb-cccc-dddd-eeee-ffffffffffff")
	rid := tbl.AllocRowID()
	if err := tbl.InsertRow(rid, uuidRow(id1, owner1, "old")); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, nil)
	newRID, err := tx.Update(2, rid, uuidRow(id2, owner2, "new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}

	if got := tbl.PrimaryIndex().Seek(NewIndexKey(id1)); len(got) != 0 {
		t.Fatalf("primary old UUID after commit = %v, want empty", got)
	}
	if got := tbl.PrimaryIndex().Seek(NewIndexKey(id2)); !slices.Equal(got, []types.RowID{newRID}) {
		t.Fatalf("primary new UUID after commit = %v, want [%d]", got, newRID)
	}
	ownerIndex := tbl.IndexByID(1)
	if got := ownerIndex.Seek(NewIndexKey(owner1)); len(got) != 0 {
		t.Fatalf("secondary old owner after commit = %v, want empty", got)
	}
	if got := ownerIndex.Seek(NewIndexKey(owner2)); !slices.Equal(got, []types.RowID{newRID}) {
		t.Fatalf("secondary new owner after commit = %v, want [%d]", got, newRID)
	}
}

func TestTransactionCompositeUniqueConflictWithinOneTransaction(t *testing.T) {
	cs := NewCommittedState()
	cs.RegisterTable(0, NewTable(compositeIndexedSchema(true)))

	tx := NewTransaction(cs, nil)
	if _, err := tx.Insert(0, compositeRow(1, "red", 10, "first")); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Insert(0, compositeRow(2, "red", 11, "different score")); err != nil {
		t.Fatalf("distinct composite key in same tx should be allowed: %v", err)
	}
	_, err := tx.Insert(0, compositeRow(3, "red", 10, "duplicate combined key"))
	if !errors.Is(err, ErrUniqueConstraintViolation) {
		t.Fatalf("tx duplicate composite key err = %v, want ErrUniqueConstraintViolation", err)
	}
}

func TestTransactionUpdateMovesCompositeIndexKey(t *testing.T) {
	cs := NewCommittedState()
	tbl := NewTable(compositeIndexedSchema(true))
	cs.RegisterTable(0, tbl)
	oldRID := tbl.AllocRowID()
	if err := tbl.InsertRow(oldRID, compositeRow(1, "red", 10, "old")); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, nil)
	newRID, err := tx.Update(0, oldRID, compositeRow(1, "blue", 20, "new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}

	idx := tbl.IndexByID(1)
	if got := idx.Seek(NewIndexKey(types.NewString("red"), types.NewUint64(10))); len(got) != 0 {
		t.Fatalf("old composite key after update commit = %v, want empty", got)
	}
	if got := idx.Seek(NewIndexKey(types.NewString("blue"), types.NewUint64(20))); !slices.Equal(got, []types.RowID{newRID}) {
		t.Fatalf("new composite key after update commit = %v, want [%d]", got, newRID)
	}
}

func TestTransactionDeleteRemovesCompositeIndexEntry(t *testing.T) {
	cs := NewCommittedState()
	tbl := NewTable(compositeIndexedSchema(false))
	cs.RegisterTable(0, tbl)
	deleteRID := tbl.AllocRowID()
	keepRID := tbl.AllocRowID()
	if err := tbl.InsertRow(deleteRID, compositeRow(1, "red", 10, "delete")); err != nil {
		t.Fatal(err)
	}
	if err := tbl.InsertRow(keepRID, compositeRow(2, "red", 20, "keep")); err != nil {
		t.Fatal(err)
	}

	tx := NewTransaction(cs, nil)
	if err := tx.Delete(0, deleteRID); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(cs, tx); err != nil {
		t.Fatal(err)
	}

	idx := tbl.IndexByID(1)
	if got := idx.Seek(NewIndexKey(types.NewString("red"), types.NewUint64(10))); len(got) != 0 {
		t.Fatalf("deleted composite key after commit = %v, want empty", got)
	}
	if got := idx.Seek(NewIndexKey(types.NewString("red"), types.NewUint64(20))); !slices.Equal(got, []types.RowID{keepRID}) {
		t.Fatalf("kept composite key after delete commit = %v, want [%d]", got, keepRID)
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

func TestCommitChangesetRowsAreSortedByRowID(t *testing.T) {
	cs, reg := buildTestState()

	insertTx := NewTransaction(cs, reg)
	insertTx.tx.AddInsert(0, 3, mkRow(30, "c"))
	insertTx.tx.AddInsert(0, 1, mkRow(10, "a"))
	insertTx.tx.AddInsert(0, 2, mkRow(20, "b"))
	insertChangeset, err := Commit(cs, insertTx)
	if err != nil {
		t.Fatal(err)
	}
	if got := changesetPKs(insertChangeset.TableChanges(0).Inserts); !slices.Equal(got, []uint64{10, 20, 30}) {
		t.Fatalf("insert changeset pk order = %v, want [10 20 30]", got)
	}

	deleteTx := NewTransaction(cs, reg)
	deleteTx.tx.AddDelete(0, 3)
	deleteTx.tx.AddDelete(0, 1)
	deleteTx.tx.AddDelete(0, 2)
	deleteChangeset, err := Commit(cs, deleteTx)
	if err != nil {
		t.Fatal(err)
	}
	if got := changesetPKs(deleteChangeset.TableChanges(0).Deletes); !slices.Equal(got, []uint64{10, 20, 30}) {
		t.Fatalf("delete changeset pk order = %v, want [10 20 30]", got)
	}
}

func changesetPKs(rows []types.ProductValue) []uint64 {
	out := make([]uint64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row[0].AsUint64())
	}
	return out
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

func TestApplyChangesetErrorLeavesCommittedStateUnchanged(t *testing.T) {
	cs, _ := buildTestState()
	tbl, _ := cs.Table(0)
	seedID := tbl.AllocRowID()
	seedRow := mkRow(1, "seed")
	if err := tbl.InsertRow(seedID, seedRow); err != nil {
		t.Fatal(err)
	}
	beforeNextID := tbl.NextID()

	err := ApplyChangeset(cs, &Changeset{
		TxID: 2,
		Tables: map[schema.TableID]*TableChangeset{
			0: {
				Deletes: []types.ProductValue{seedRow},
				Inserts: []types.ProductValue{
					mkRow(2, "first"),
					mkRow(2, "duplicate"),
				},
			},
		},
	})
	if !errors.Is(err, ErrPrimaryKeyViolation) {
		t.Fatalf("ApplyChangeset error = %v, want ErrPrimaryKeyViolation", err)
	}
	if tbl.RowCount() != 1 {
		t.Fatalf("row count after failed apply = %d, want 1", tbl.RowCount())
	}
	if got, ok := tbl.GetRow(seedID); !ok || !got.Equal(seedRow) {
		t.Fatalf("seed row after failed apply = %v, %v; want unchanged", got, ok)
	}
	if tbl.NextID() != beforeNextID {
		t.Fatalf("NextID after failed apply = %d, want %d", tbl.NextID(), beforeNextID)
	}
	pk := tbl.PrimaryIndex()
	if got := pk.Seek(NewIndexKey(types.NewUint64(2))); len(got) != 0 {
		t.Fatalf("failed apply left duplicate insert in primary index: %v", got)
	}
}

func TestApplyChangesetErrorLeavesAutoIncrementSequenceUnchanged(t *testing.T) {
	cs, _ := buildAutoIncrementState(t)
	tbl, _ := cs.Table(0)
	beforeSeq, ok := tbl.SequenceValue()
	if !ok {
		t.Fatal("expected autoincrement sequence")
	}
	beforeNextID := tbl.NextID()

	err := ApplyChangeset(cs, &Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*TableChangeset{
			0: {
				Inserts: []types.ProductValue{
					{types.NewUint64(beforeSeq), types.NewString("first")},
					{types.NewUint64(beforeSeq), types.NewString("duplicate")},
				},
			},
		},
	})
	if !errors.Is(err, ErrPrimaryKeyViolation) {
		t.Fatalf("ApplyChangeset error = %v, want ErrPrimaryKeyViolation", err)
	}
	if tbl.RowCount() != 0 {
		t.Fatalf("row count after failed replay = %d, want 0", tbl.RowCount())
	}
	if got, _ := tbl.SequenceValue(); got != beforeSeq {
		t.Fatalf("SequenceValue after failed replay = %d, want %d", got, beforeSeq)
	}
	if tbl.NextID() != beforeNextID {
		t.Fatalf("NextID after failed replay = %d, want %d", tbl.NextID(), beforeNextID)
	}
}

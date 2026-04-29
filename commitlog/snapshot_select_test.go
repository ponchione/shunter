package commitlog

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestSelectSnapshotNewestValid(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	writeSelectionSnapshot(t, root, reg, cs, 10)

	snap, err := SelectSnapshot(root, 10, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil || snap.TxID != 10 {
		t.Fatalf("selected snapshot = %+v, want tx 10", snap)
	}
}

func TestSelectSnapshotCorruptNewestFallsBack(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	writeSelectionSnapshot(t, root, reg, cs, 10)
	corruptSelectionSnapshot(t, root, 10)

	snap, err := SelectSnapshot(root, 10, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil || snap.TxID != 5 {
		t.Fatalf("selected snapshot = %+v, want tx 5 fallback", snap)
	}
}

func TestSelectSnapshotHeaderTxIDMismatchFallsBack(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 4)
	writeSelectionSnapshot(t, root, reg, cs, 5)

	snapshotBase := filepath.Join(root, "snapshots")
	if err := os.Rename(filepath.Join(snapshotBase, "5"), filepath.Join(snapshotBase, "10")); err != nil {
		t.Fatal(err)
	}

	snap, skipped, err := selectSnapshotWithReport(root, 10, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil || snap.TxID != 4 {
		t.Fatalf("selected snapshot = %+v, want tx 4 fallback", snap)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped snapshots = %+v, want one mismatch skip", skipped)
	}
	got := skipped[0]
	if got.TxID != 10 || got.Reason != SnapshotSkipReadFailed {
		t.Fatalf("skipped snapshot = %+v, want tx 10 read_failed", got)
	}
	for _, want := range []string{"tx_id mismatch", "directory=10", "header=5"} {
		if !strings.Contains(got.Detail, want) {
			t.Fatalf("skipped snapshot detail = %q, want %q", got.Detail, want)
		}
	}
}

func TestSelectSnapshotTempFileCandidateFallsBack(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	writeSelectionSnapshot(t, root, reg, cs, 10)
	if err := os.WriteFile(filepath.Join(root, "snapshots", "10", "snapshot.tmp"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := SelectSnapshot(root, 10, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil || snap.TxID != 5 {
		t.Fatalf("selected snapshot = %+v, want tx 5 fallback", snap)
	}
}

func TestSelectSnapshotAllCorruptLogStartsAtTx1ReturnsNil(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	corruptSelectionSnapshot(t, root, 5)
	writeSelectionSegmentRange(t, root, reg, 1, 3)

	snap, err := SelectSnapshot(root, 3, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap != nil {
		t.Fatalf("selected snapshot = %+v, want nil", snap)
	}
}

func TestSelectSnapshotAllCorruptLogStartsAfterTx1ReturnsMissingBase(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	corruptSelectionSnapshot(t, root, 5)
	writeSelectionSegmentRange(t, root, reg, 3, 2)

	_, err := SelectSnapshot(root, 4, reg)
	if !errors.Is(err, ErrMissingBaseSnapshot) {
		t.Fatalf("expected ErrMissingBaseSnapshot, got %v", err)
	}
}

func TestSelectSnapshotSkipsSnapshotsPastDurableHorizon(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	writeSelectionSnapshot(t, root, reg, cs, 10)

	snap, err := SelectSnapshot(root, 7, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil || snap.TxID != 5 {
		t.Fatalf("selected snapshot = %+v, want tx 5", snap)
	}
}

func TestSelectSnapshotSchemaMismatchColumnType(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		players := tables[0]
		players.Columns[1].Type = schema.KindUint64
		tables[0] = players
	})

	_, err := SelectSnapshot(root, 5, mismatchReg)
	assertSchemaMismatchDetail(t, err, "column")
	assertSchemaMismatchDetail(t, err, "type")
}

func TestSelectSnapshotSchemaMismatchNewestDoesNotFallBackToOlderSnapshot(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	newerReg := buildSelectionRegistry(t, selectionRegistryConfig{tableName: "users"})
	newerState := buildSelectionCommittedState(t, newerReg)
	writeSelectionSnapshot(t, root, newerReg, newerState, 10)

	snap, skipped, err := selectSnapshotWithReport(root, 10, reg)
	if err == nil {
		t.Fatal("expected newest schema mismatch to fail loudly")
	}
	if snap != nil {
		t.Fatalf("selected snapshot = %+v, want none on schema mismatch", snap)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped snapshots = %+v, want no fallback-style skip for schema mismatch", skipped)
	}
	assertSchemaMismatchDetail(t, err, "name")
	if !strings.Contains(err.Error(), "users") || !strings.Contains(err.Error(), "players") {
		t.Fatalf("schema mismatch error = %v, want conflicting table names", err)
	}
}

func TestSelectSnapshotSchemaMismatchTableName(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		players := tables[0]
		players.Name = "users"
		tables[0] = players
	})

	_, err := SelectSnapshot(root, 5, mismatchReg)
	assertSchemaMismatchDetail(t, err, "name")
	assertSchemaMismatchDetail(t, err, "players")
	assertSchemaMismatchDetail(t, err, "users")
}

func TestSelectSnapshotSchemaMismatchColumnNameAndIndex(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(schema.TableSchema) schema.TableSchema
		want   string
	}{
		{
			name: "name",
			mutate: func(players schema.TableSchema) schema.TableSchema {
				players.Columns[1].Name = "handle"
				return players
			},
			want: "name",
		},
		{
			name: "index",
			mutate: func(players schema.TableSchema) schema.TableSchema {
				players.Columns[1].Index = 99
				return players
			},
			want: "index",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cs, reg := buildSnapshotCommittedState(t)
			writeSelectionSnapshot(t, root, reg, cs, 5)
			mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
				tables[0] = tc.mutate(tables[0])
			})

			_, err := SelectSnapshot(root, 5, mismatchReg)
			assertSchemaMismatchDetail(t, err, "column")
			assertSchemaMismatchDetail(t, err, tc.want)
		})
	}
}

func TestSelectSnapshotSchemaMismatchAutoIncrementFlag(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoveryAutoIncrementRegistry(t)
	cs := buildRecoveryCommittedState(t, reg)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		jobs := tables[0]
		jobs.Columns[0].AutoIncrement = false
		tables[0] = jobs
	})

	_, err := SelectSnapshot(root, 5, mismatchReg)
	assertSchemaMismatchDetail(t, err, "auto_increment")
}

func TestSelectSnapshotSchemaMismatchMissingTable(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		tables[999] = schema.TableSchema{
			ID:   999,
			Name: "guilds",
			Columns: []schema.ColumnSchema{
				{Index: 0, Name: "id", Type: schema.KindUint64},
				{Index: 1, Name: "name", Type: schema.KindString},
			},
			Indexes: []schema.IndexSchema{{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true}},
		}
	})

	_, err := SelectSnapshot(root, 5, mismatchReg)
	assertSchemaMismatchDetail(t, err, "missing")
	assertSchemaMismatchDetail(t, err, "guilds")
}

func TestSelectSnapshotSchemaMismatchExtraIndex(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		players := tables[0]
		players.Indexes = append(players.Indexes, schema.IndexSchema{ID: 99, Name: "by_name", Columns: []int{1}})
		tables[0] = players
	})

	_, err := SelectSnapshot(root, 5, mismatchReg)
	assertSchemaMismatchDetail(t, err, "index")
}

func TestSelectSnapshotSchemaMismatchIndexDetails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(schema.IndexSchema) schema.IndexSchema
		want   string
	}{
		{
			name: "columns",
			mutate: func(idx schema.IndexSchema) schema.IndexSchema {
				idx.Columns = []int{1}
				return idx
			},
			want: "columns",
		},
		{
			name: "unique",
			mutate: func(idx schema.IndexSchema) schema.IndexSchema {
				idx.Unique = false
				return idx
			},
			want: "unique",
		},
		{
			name: "primary",
			mutate: func(idx schema.IndexSchema) schema.IndexSchema {
				idx.Primary = false
				return idx
			},
			want: "primary",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cs, reg := buildSnapshotCommittedState(t)
			writeSelectionSnapshot(t, root, reg, cs, 5)
			mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
				players := tables[0]
				players.Indexes[0] = tc.mutate(players.Indexes[0])
				tables[0] = players
			})

			_, err := SelectSnapshot(root, 5, mismatchReg)
			assertSchemaMismatchDetail(t, err, "index")
			assertSchemaMismatchDetail(t, err, tc.want)
		})
	}
}

func TestSelectSnapshotSchemaSnapshotVersionMismatch(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	path := filepath.Join(root, "snapshots", "5", snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(data[SnapshotHeaderSize+4:SnapshotHeaderSize+8], reg.Version()+1)
	hash := ComputeSnapshotHash(data[SnapshotHeaderSize:])
	copy(data[20:52], hash[:])
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = SelectSnapshot(root, 5, reg)
	assertSchemaMismatchDetail(t, err, "schema snapshot version")
}

type selectionRegistryConfig struct {
	tableName      string
	extraTableName string
	nameType       schema.ValueKind
	extraNameIndex bool
	version        uint32
}

func buildSelectionRegistry(t *testing.T, cfg selectionRegistryConfig) schema.SchemaRegistry {
	t.Helper()
	if cfg.tableName == "" {
		cfg.tableName = "players"
	}
	if cfg.nameType == 0 {
		cfg.nameType = schema.KindString
	}
	if cfg.version == 0 {
		cfg.version = 1
	}

	b := schema.NewBuilder()
	b.SchemaVersion(cfg.version)
	def := schema.TableDefinition{
		Name: cfg.tableName,
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint64, PrimaryKey: true},
			{Name: "name", Type: cfg.nameType},
		},
	}
	if cfg.extraNameIndex {
		def.Indexes = append(def.Indexes, schema.IndexDefinition{Name: "by_name", Columns: []string{"name"}, Unique: false})
	}
	b.TableDef(def)
	if cfg.extraTableName != "" {
		b.TableDef(schema.TableDefinition{
			Name: cfg.extraTableName,
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: schema.KindUint64, PrimaryKey: true},
				{Name: "label", Type: schema.KindString},
			},
		})
	}
	engine, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return engine.Registry()
}

func buildSelectionCommittedState(t *testing.T, reg schema.SchemaRegistry) *store.CommittedState {
	t.Helper()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	if players, ok := reg.Table(0); ok && players.Name == "players" {
		table, _ := cs.Table(0)
		if err := table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
			t.Fatal(err)
		}
	}
	return cs
}

func writeSelectionSnapshot(t *testing.T, root string, reg schema.SchemaRegistry, cs *store.CommittedState, txID types.TxID) {
	t.Helper()
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, txID)
}

func corruptSelectionSnapshot(t *testing.T, root string, txID types.TxID) {
	t.Helper()
	path := filepath.Join(root, "snapshots", txIDString(uint64(txID)), "snapshot")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSelectionSegmentRange(t *testing.T, root string, reg schema.SchemaRegistry, startTx types.TxID, count int) {
	t.Helper()
	seg, err := CreateSegment(root, uint64(startTx))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := seg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	for i := 0; i < count; i++ {
		txID := uint64(startTx) + uint64(i)
		cs := &store.Changeset{TxID: types.TxID(txID), Tables: map[schema.TableID]*store.TableChangeset{}}
		if players, ok := reg.Table(0); ok && players.Name == "players" {
			cs.Tables[0] = &store.TableChangeset{
				TableID:   0,
				TableName: "players",
				Inserts:   []types.ProductValue{{types.NewUint64(txID), types.NewString("user")}},
			}
		}
		payload, err := EncodeChangeset(cs)
		if err != nil {
			t.Fatal(err)
		}
		if err := seg.Append(&Record{TxID: txID, RecordType: RecordTypeChangeset, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
}

func cloneSelectionRegistry(reg schema.SchemaRegistry, mutate func(map[schema.TableID]schema.TableSchema)) schema.SchemaRegistry {
	tables := make(map[schema.TableID]schema.TableSchema, len(reg.Tables()))
	for _, tableID := range reg.Tables() {
		table, ok := reg.Table(tableID)
		if !ok {
			continue
		}
		clone := *table
		clone.Columns = append([]schema.ColumnSchema(nil), table.Columns...)
		clone.Indexes = make([]schema.IndexSchema, len(table.Indexes))
		for i, idx := range table.Indexes {
			idxClone := idx
			idxClone.Columns = append([]int(nil), idx.Columns...)
			clone.Indexes[i] = idxClone
		}
		tables[tableID] = clone
	}
	if mutate != nil {
		mutate(tables)
	}
	ids := make([]schema.TableID, 0, len(tables))
	for tableID := range tables {
		ids = append(ids, tableID)
	}
	slices.Sort(ids)
	return selectionSchemaRegistry{tables: tables, ids: ids, version: reg.Version()}
}

type selectionSchemaRegistry struct {
	tables  map[schema.TableID]schema.TableSchema
	ids     []schema.TableID
	version uint32
}

func (r selectionSchemaRegistry) Table(id schema.TableID) (*schema.TableSchema, bool) {
	table, ok := r.tables[id]
	if !ok {
		return nil, false
	}
	clone := table
	clone.Columns = append([]schema.ColumnSchema(nil), table.Columns...)
	clone.Indexes = make([]schema.IndexSchema, len(table.Indexes))
	for i, idx := range table.Indexes {
		idxClone := idx
		idxClone.Columns = append([]int(nil), idx.Columns...)
		clone.Indexes[i] = idxClone
	}
	return &clone, true
}

func (r selectionSchemaRegistry) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	for _, tableID := range r.ids {
		table := r.tables[tableID]
		if table.Name == name {
			ts, ok := r.Table(tableID)
			return tableID, ts, ok
		}
	}
	return 0, nil, false
}

func (r selectionSchemaRegistry) TableExists(id schema.TableID) bool {
	_, ok := r.tables[id]
	return ok
}

func (r selectionSchemaRegistry) TableName(id schema.TableID) string {
	if t, ok := r.tables[id]; ok {
		return t.Name
	}
	return ""
}

func (r selectionSchemaRegistry) ColumnExists(id schema.TableID, col types.ColID) bool {
	t, ok := r.tables[id]
	if !ok {
		return false
	}
	return int(col) >= 0 && int(col) < len(t.Columns)
}

func (r selectionSchemaRegistry) ColumnType(id schema.TableID, col types.ColID) schema.ValueKind {
	t, ok := r.tables[id]
	if !ok || int(col) < 0 || int(col) >= len(t.Columns) {
		return 0
	}
	return t.Columns[col].Type
}

func (r selectionSchemaRegistry) HasIndex(id schema.TableID, col types.ColID) bool {
	_, ok := r.IndexIDForColumn(id, col)
	return ok
}

func (r selectionSchemaRegistry) ColumnCount(id schema.TableID) int {
	t, ok := r.tables[id]
	if !ok {
		return 0
	}
	return len(t.Columns)
}

func (r selectionSchemaRegistry) IndexIDForColumn(id schema.TableID, col types.ColID) (schema.IndexID, bool) {
	t, ok := r.tables[id]
	if !ok {
		return 0, false
	}
	for _, idx := range t.Indexes {
		if len(idx.Columns) == 1 && idx.Columns[0] == int(col) {
			return idx.ID, true
		}
	}
	return 0, false
}

func (r selectionSchemaRegistry) Tables() []schema.TableID {
	return append([]schema.TableID(nil), r.ids...)
}

func (r selectionSchemaRegistry) Reducer(string) (schema.ReducerHandler, bool) { return nil, false }
func (r selectionSchemaRegistry) Reducers() []string                           { return nil }
func (r selectionSchemaRegistry) OnConnect() func(*schema.ReducerContext) error {
	return nil
}
func (r selectionSchemaRegistry) OnDisconnect() func(*schema.ReducerContext) error {
	return nil
}
func (r selectionSchemaRegistry) Version() uint32 { return r.version }

func assertSchemaMismatchDetail(t *testing.T, err error, want string) {
	t.Helper()
	var mismatch *SchemaMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SchemaMismatchError, got %v", err)
	}
	if !strings.Contains(strings.ToLower(mismatch.Detail), strings.ToLower(want)) {
		t.Fatalf("schema mismatch detail %q does not contain %q", mismatch.Detail, want)
	}
}

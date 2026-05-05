package schema

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ponchione/shunter/types"
)

const (
	schemaBuildFuzzMaxInputBytes = 512
	schemaBuildFuzzRuntimeConfig = "max_tables=3,max_columns=4,max_indexes=2"
)

func FuzzBuildPreviewRegistryExport(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		[]byte{0, 1, 2, 3, 4, 5},
		[]byte("schema-build-public-surface"),
		[]byte{13, 21, 34, 55, 89, 144, 233},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > schemaBuildFuzzMaxInputBytes {
			t.Skip("schema build fuzz input above bounded local limit")
		}
		spec := newSchemaBuildFuzzSpec(data)
		label := schemaBuildFuzzLabel(data)

		preview, previewErr := buildSchemaFuzzEngine(spec, false)
		built, buildErr := buildSchemaFuzzEngine(spec, true)
		if (previewErr == nil) != (buildErr == nil) {
			t.Fatalf("%s operation=BuildPreview-vs-Build observed_preview_error=%v observed_build_error=%v expected=same-acceptance",
				label, previewErr, buildErr)
		}
		if previewErr != nil {
			return
		}

		previewExport := preview.ExportSchema()
		builtExport := built.ExportSchema()
		if !reflect.DeepEqual(previewExport, builtExport) {
			t.Fatalf("%s operation=ExportSchema(BuildPreview-vs-Build) observed=%#v expected=%#v",
				label, previewExport, builtExport)
		}
		assertSchemaBuildFuzzRegistry(t, built.Registry(), builtExport, label)
		assertSchemaBuildFuzzExportDetached(t, built, label)
	})
}

type schemaBuildFuzzSpec struct {
	version              uint32
	tables               []TableDefinition
	registerReducer      bool
	reducerName          string
	nilReducer           bool
	duplicateReducer     bool
	registerOnConnect    bool
	nilOnConnect         bool
	registerOnDisconnect bool
	nilOnDisconnect      bool
}

func newSchemaBuildFuzzSpec(data []byte) schemaBuildFuzzSpec {
	r := newSchemaBuildFuzzReader(data)
	spec := schemaBuildFuzzSpec{
		version: uint32(r.byte()%5) + 1,
	}
	if r.byte()%47 == 1 {
		spec.version = 0
	}

	tableCount := int(r.byte()%3) + 1
	spec.tables = make([]TableDefinition, 0, tableCount)
	for tableIdx := range tableCount {
		spec.tables = append(spec.tables, schemaBuildFuzzTable(r, tableIdx))
	}

	if r.byte()%2 == 1 {
		spec.registerReducer = true
		spec.reducerName = schemaBuildFuzzReducerName(r)
		spec.nilReducer = r.byte()%17 == 1
		spec.duplicateReducer = r.byte()%19 == 1
	}
	spec.registerOnConnect = r.byte()%7 == 1
	spec.nilOnConnect = r.byte()%13 == 1
	spec.registerOnDisconnect = r.byte()%11 == 1
	spec.nilOnDisconnect = r.byte()%17 == 1
	return spec
}

func schemaBuildFuzzTable(r *schemaBuildFuzzReader, tableIdx int) TableDefinition {
	tableNames := []string{"players", "tasks", "messages", "files", "Events"}
	columnNames := []string{"id", "owner", "name", "score", "done", "payload", "created_at", "tags", "ttl", "metadata"}
	kinds := []ValueKind{KindBool, KindUint64, KindString, KindBytes, KindInt64, KindTimestamp, KindArrayString, KindUUID, KindDuration, KindJSON}

	name := tableNames[(int(r.byte())+tableIdx)%len(tableNames)]
	switch r.byte() % 53 {
	case 1:
		name = ""
	case 2:
		name = "sys_clients"
	case 3:
		name = "bad-name"
	}

	columnCount := int(r.byte()%4) + 1
	columns := make([]ColumnDefinition, 0, columnCount)
	for columnIdx := range columnCount {
		colName := columnNames[(int(r.byte())+columnIdx)%len(columnNames)]
		kind := kinds[int(r.byte())%len(kinds)]
		primary := false
		if columnIdx == 0 {
			colName = "id"
			kind = KindUint64
			primary = r.byte()%4 != 1
		}
		switch r.byte() % 59 {
		case 1:
			colName = ""
		case 2:
			colName = "BadColumn"
		case 3:
			colName = "id"
		}
		columns = append(columns, ColumnDefinition{
			Name:          colName,
			Type:          kind,
			PrimaryKey:    primary,
			Nullable:      r.byte()%43 == 1,
			AutoIncrement: r.byte()%37 == 1,
		})
	}

	indexCount := int(r.byte() % 3)
	indexes := make([]IndexDefinition, 0, indexCount)
	for indexIdx := range indexCount {
		columnName := columns[int(r.byte())%len(columns)].Name
		if len(columns) > 1 && r.byte()%5 != 1 {
			columnName = columns[1+int(r.byte())%(len(columns)-1)].Name
		}
		if r.byte()%29 == 1 {
			columnName = "missing_column"
		}
		indexName := fmt.Sprintf("%s_idx_%d", columnName, indexIdx)
		if r.byte()%31 == 1 {
			indexName = ""
		}
		indexColumns := []string{columnName}
		if len(columns) > 2 && r.byte()%4 == 1 {
			indexColumns = append(indexColumns, columns[2+int(r.byte())%(len(columns)-2)].Name)
		}
		indexes = append(indexes, IndexDefinition{
			Name:    indexName,
			Columns: indexColumns,
			Unique:  r.byte()%2 == 1,
		})
	}

	return TableDefinition{
		Name:       name,
		Columns:    columns,
		Indexes:    indexes,
		ReadPolicy: schemaBuildFuzzReadPolicy(r),
	}
}

func schemaBuildFuzzReadPolicy(r *schemaBuildFuzzReader) ReadPolicy {
	switch r.byte() % 5 {
	case 0:
		return ReadPolicy{Access: TableAccessPrivate}
	case 1:
		return ReadPolicy{Access: TableAccessPublic}
	case 2:
		return ReadPolicy{Access: TableAccessPermissioned, Permissions: []string{schemaBuildFuzzPermission(r)}}
	case 3:
		return ReadPolicy{Access: TableAccessPermissioned}
	default:
		return ReadPolicy{Access: TableAccessPublic, Permissions: []string{"unexpected"}}
	}
}

func schemaBuildFuzzPermission(r *schemaBuildFuzzReader) string {
	permissions := []string{"tasks:read", "messages:read", "files:read", "events:read"}
	if r.byte()%23 == 1 {
		return " "
	}
	return permissions[int(r.byte())%len(permissions)]
}

func schemaBuildFuzzReducerName(r *schemaBuildFuzzReader) string {
	names := []string{"CreateTask", "UpdateScore", "ArchiveEvent", "OnConnect", ""}
	return names[int(r.byte())%len(names)]
}

func buildSchemaFuzzEngine(spec schemaBuildFuzzSpec, freeze bool) (*Engine, error) {
	b := NewBuilder().SchemaVersion(spec.version)
	for _, table := range spec.tables {
		b.TableDef(table)
	}
	if spec.registerReducer {
		var handler ReducerHandler = func(*ReducerContext, []byte) ([]byte, error) { return nil, nil }
		if spec.nilReducer {
			handler = nil
		}
		b.Reducer(spec.reducerName, handler)
		if spec.duplicateReducer {
			b.Reducer(spec.reducerName, handler)
		}
	}
	if spec.registerOnConnect {
		var handler func(*ReducerContext) error = func(*ReducerContext) error { return nil }
		if spec.nilOnConnect {
			handler = nil
		}
		b.OnConnect(handler)
	}
	if spec.registerOnDisconnect {
		var handler func(*ReducerContext) error = func(*ReducerContext) error { return nil }
		if spec.nilOnDisconnect {
			handler = nil
		}
		b.OnDisconnect(handler)
	}
	if freeze {
		return b.Build(EngineOptions{})
	}
	return b.BuildPreview(EngineOptions{})
}

func assertSchemaBuildFuzzRegistry(t *testing.T, reg SchemaRegistry, export *SchemaExport, label string) {
	t.Helper()
	ids := reg.Tables()
	if len(ids) != len(export.Tables) {
		t.Fatalf("%s operation=Registry.Tables observed_len=%d expected_export_len=%d ids=%v export=%#v",
			label, len(ids), len(export.Tables), ids, export.Tables)
	}
	seen := map[TableID]struct{}{}
	for pos, id := range ids {
		if _, exists := seen[id]; exists {
			t.Fatalf("%s operation=Registry.Tables observed_duplicate_id=%d expected_unique ids=%v", label, id, ids)
		}
		seen[id] = struct{}{}

		table, ok := reg.Table(id)
		if !ok {
			t.Fatalf("%s operation=Registry.Table(%d) observed_ok=false expected_ok=true", label, id)
		}
		if table.ID != id {
			t.Fatalf("%s operation=Registry.Table(%d) observed_id=%d expected_id=%d", label, id, table.ID, id)
		}
		exported := export.Tables[pos]
		if exported.Name != table.Name {
			t.Fatalf("%s operation=ExportSchema table %d observed_name=%q expected_name=%q",
				label, id, exported.Name, table.Name)
		}
		if !reflect.DeepEqual(exported.ReadPolicy, normalizeReadPolicy(table.ReadPolicy)) {
			t.Fatalf("%s operation=ExportSchema read-policy table %d observed=%#v expected=%#v",
				label, id, exported.ReadPolicy, normalizeReadPolicy(table.ReadPolicy))
		}

		byNameID, byName, ok := reg.TableByName(table.Name)
		if !ok || byNameID != id || byName.Name != table.Name {
			t.Fatalf("%s operation=Registry.TableByName(%q) observed=(%d,%+v,%v) expected=(%d,%q,true)",
				label, table.Name, byNameID, byName, ok, id, table.Name)
		}
		if reg.TableName(id) != table.Name {
			t.Fatalf("%s operation=Registry.TableName(%d) observed=%q expected=%q", label, id, reg.TableName(id), table.Name)
		}
		if reg.ColumnCount(id) != len(table.Columns) {
			t.Fatalf("%s operation=Registry.ColumnCount(%d) observed=%d expected=%d", label, id, reg.ColumnCount(id), len(table.Columns))
		}
		for _, col := range table.Columns {
			colID := types.ColID(col.Index)
			if !reg.ColumnExists(id, colID) || reg.ColumnType(id, colID) != col.Type {
				t.Fatalf("%s operation=Registry.Column(%d,%d) observed_exists=%v observed_type=%v expected_exists=true expected_type=%v",
					label, id, col.Index, reg.ColumnExists(id, colID), reg.ColumnType(id, colID), col.Type)
			}
		}
		if reg.ColumnExists(id, types.ColID(len(table.Columns))) {
			t.Fatalf("%s operation=Registry.ColumnExists(%d,%d) observed=true expected=false",
				label, id, len(table.Columns))
		}
		for _, idx := range table.Indexes {
			if len(idx.Columns) != 1 {
				continue
			}
			colID := types.ColID(idx.Columns[0])
			gotID, ok := reg.IndexIDForColumn(id, colID)
			if !ok || gotID != idx.ID || !reg.HasIndex(id, colID) {
				t.Fatalf("%s operation=Registry.IndexIDForColumn(%d,%d) observed=(%d,%v,%v) expected=(%d,true,true)",
					label, id, colID, gotID, ok, reg.HasIndex(id, colID), idx.ID)
			}
		}

		if len(table.Columns) > 0 {
			table.Columns[0].Name = "mutated"
			again, ok := reg.Table(id)
			if !ok || again.Columns[0].Name == "mutated" {
				t.Fatalf("%s operation=Registry.Table-detached(%d) observed=(%+v,%v) expected=unmutated-copy",
					label, id, again, ok)
			}
		}
	}
}

func assertSchemaBuildFuzzExportDetached(t *testing.T, engine *Engine, label string) {
	t.Helper()
	before := engine.ExportSchema()
	mutated := engine.ExportSchema()
	if len(mutated.Tables) > 0 {
		mutated.Tables[0].Name = "mutated"
		if len(mutated.Tables[0].Columns) > 0 {
			mutated.Tables[0].Columns[0].Name = "mutated"
		}
		if len(mutated.Tables[0].Indexes) > 0 && len(mutated.Tables[0].Indexes[0].Columns) > 0 {
			mutated.Tables[0].Indexes[0].Columns[0] = "mutated"
		}
		mutated.Tables[0].ReadPolicy.Permissions = append(mutated.Tables[0].ReadPolicy.Permissions, "mutated")
	}
	mutated.Reducers = append(mutated.Reducers, ReducerExport{Name: "Mutated"})
	after := engine.ExportSchema()
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("%s operation=ExportSchema-detached observed=%#v expected=%#v", label, after, before)
	}
}

type schemaBuildFuzzReader struct {
	data []byte
	pos  int
}

func newSchemaBuildFuzzReader(data []byte) *schemaBuildFuzzReader {
	return &schemaBuildFuzzReader{data: data}
}

func (r *schemaBuildFuzzReader) byte() byte {
	if len(r.data) == 0 {
		r.pos++
		return 0
	}
	b := r.data[r.pos%len(r.data)]
	r.pos++
	return b
}

func schemaBuildFuzzLabel(data []byte) string {
	if len(data) <= 80 {
		return fmt.Sprintf("seed_len=%d seed=%x runtime_config=%s", len(data), data, schemaBuildFuzzRuntimeConfig)
	}
	return fmt.Sprintf("seed_len=%d seed_prefix=%x runtime_config=%s", len(data), data[:80], schemaBuildFuzzRuntimeConfig)
}

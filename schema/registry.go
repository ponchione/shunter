package schema

import (
	"strings"

	"github.com/ponchione/shunter/types"
)

// SchemaLookup is the narrow read-only schema surface consumed by SPEC-004
// (subscription/validate) and SPEC-005 (protocol/handle_subscribe,
// protocol/upgrade). SchemaRegistry satisfies SchemaLookup; consumer
// packages may also declare narrower local interfaces that SchemaRegistry
// continues to satisfy.
type SchemaLookup interface {
	// Table returns the full schema for the given table ID.
	Table(id TableID) (*TableSchema, bool)

	// TableByName returns the table ID and full schema for the given name.
	// The 3-tuple shape exists so that wire handlers can resolve a name
	// to its TableID without a second lookup.
	TableByName(name string) (TableID, *TableSchema, bool)

	// TableExists reports whether the table ID is registered. Cheaper
	// than Table() when the schema body is not needed.
	TableExists(table TableID) bool

	// TableName returns the declared table name, or empty string if the
	// table ID is unknown. Used for wire/debug output.
	TableName(table TableID) string

	// ColumnExists reports whether the column index is valid for the table.
	ColumnExists(table TableID, col types.ColID) bool

	// ColumnType returns the ValueKind of the column. Behavior is undefined
	// when ColumnExists returns false; callers must check first.
	ColumnType(table TableID, col types.ColID) ValueKind

	// HasIndex reports whether a single-column index on (table, col) exists.
	HasIndex(table TableID, col types.ColID) bool
}

// IndexResolver maps (table, column) → index ID when a single-column index
// on that column exists. Used by SPEC-004 Tier-2 candidate collection.
// SchemaRegistry satisfies IndexResolver.
type IndexResolver interface {
	IndexIDForColumn(table TableID, col types.ColID) (IndexID, bool)
}

// SchemaRegistry is a read-only view of all registered tables, indexes, and
// reducers. Safe for concurrent use — immutable after construction.
type SchemaRegistry interface {
	SchemaLookup
	IndexResolver

	Tables() []TableID
	Reducer(name string) (ReducerHandler, bool)
	Reducers() []string
	OnConnect() func(*ReducerContext) error
	OnDisconnect() func(*ReducerContext) error
	Version() uint32
}

type schemaRegistry struct {
	tables       []TableSchema
	byID         map[TableID]int
	byName       map[string]int
	tableIDs     []TableID // user tables first, then system
	reducers     map[string]ReducerHandler
	reducerNames []string
	onConnect    func(*ReducerContext) error
	onDisconnect func(*ReducerContext) error
	version      uint32
}

func newSchemaRegistry(schemas []TableSchema, b *Builder) SchemaRegistry {
	r := &schemaRegistry{
		tables:   schemas,
		byID:     make(map[TableID]int, len(schemas)),
		byName:   make(map[string]int, len(schemas)),
		tableIDs: make([]TableID, len(schemas)),
		reducers: make(map[string]ReducerHandler, len(b.reducers)),
		version:  b.version,
	}

	for i := range schemas {
		r.byID[schemas[i].ID] = i
		r.byName[schemas[i].Name] = i
		r.tableIDs[i] = schemas[i].ID
	}

	for _, name := range b.reducerOrder {
		entry := b.reducers[name]
		r.reducers[name] = entry.handler
		r.reducerNames = append(r.reducerNames, name)
	}

	r.onConnect = b.onConnect
	r.onDisconnect = b.onDisconnect
	return r
}

func (r *schemaRegistry) Table(id TableID) (*TableSchema, bool) {
	i, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	ts := cloneTableSchema(r.tables[i])
	return &ts, true
}

func (r *schemaRegistry) TableByName(name string) (TableID, *TableSchema, bool) {
	if i, ok := r.byName[name]; ok {
		ts := cloneTableSchema(r.tables[i])
		return ts.ID, &ts, true
	}
	for i := range r.tables {
		if strings.EqualFold(r.tables[i].Name, name) {
			ts := cloneTableSchema(r.tables[i])
			return ts.ID, &ts, true
		}
	}
	return 0, nil, false
}

func (r *schemaRegistry) TableExists(id TableID) bool {
	_, ok := r.byID[id]
	return ok
}

func (r *schemaRegistry) TableName(id TableID) string {
	i, ok := r.byID[id]
	if !ok {
		return ""
	}
	return r.tables[i].Name
}

func (r *schemaRegistry) ColumnExists(table TableID, col types.ColID) bool {
	i, ok := r.byID[table]
	if !ok {
		return false
	}
	return int(col) >= 0 && int(col) < len(r.tables[i].Columns)
}

func (r *schemaRegistry) ColumnType(table TableID, col types.ColID) ValueKind {
	i, ok := r.byID[table]
	if !ok {
		return 0
	}
	if int(col) < 0 || int(col) >= len(r.tables[i].Columns) {
		return 0
	}
	return r.tables[i].Columns[col].Type
}

func (r *schemaRegistry) HasIndex(table TableID, col types.ColID) bool {
	_, ok := r.indexIDForColumn(table, col)
	return ok
}

func (r *schemaRegistry) IndexIDForColumn(table TableID, col types.ColID) (IndexID, bool) {
	return r.indexIDForColumn(table, col)
}

func (r *schemaRegistry) indexIDForColumn(table TableID, col types.ColID) (IndexID, bool) {
	i, ok := r.byID[table]
	if !ok {
		return 0, false
	}
	for _, idx := range r.tables[i].Indexes {
		if len(idx.Columns) == 1 && idx.Columns[0] == int(col) {
			return idx.ID, true
		}
	}
	return 0, false
}

func (r *schemaRegistry) Tables() []TableID {
	out := make([]TableID, len(r.tableIDs))
	copy(out, r.tableIDs)
	return out
}

func (r *schemaRegistry) Reducer(name string) (ReducerHandler, bool) {
	h, ok := r.reducers[name]
	return h, ok
}

func (r *schemaRegistry) Reducers() []string {
	out := make([]string, len(r.reducerNames))
	copy(out, r.reducerNames)
	return out
}

func (r *schemaRegistry) OnConnect() func(*ReducerContext) error {
	return r.onConnect
}

func (r *schemaRegistry) OnDisconnect() func(*ReducerContext) error {
	return r.onDisconnect
}

func (r *schemaRegistry) Version() uint32 {
	return r.version
}

func cloneTableSchema(ts TableSchema) TableSchema {
	clone := ts
	clone.Columns = append([]ColumnSchema(nil), ts.Columns...)
	clone.Indexes = make([]IndexSchema, len(ts.Indexes))
	for i, idx := range ts.Indexes {
		idxClone := idx
		idxClone.Columns = append([]int(nil), idx.Columns...)
		clone.Indexes[i] = idxClone
	}
	return clone
}

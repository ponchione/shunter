package schema

import "github.com/ponchione/shunter/types"

// SchemaRegistry is a read-only view of all registered tables, indexes, and reducers.
// Safe for concurrent use — immutable after construction.
type SchemaRegistry interface {
	Table(id TableID) (*TableSchema, bool)
	TableByName(name string) (*TableSchema, bool)
	Tables() []TableID
	Reducer(name string) (types.ReducerHandler, bool)
	Reducers() []string
	OnConnect() func(*types.ReducerContext) error
	OnDisconnect() func(*types.ReducerContext) error
	Version() uint32
}

type schemaRegistry struct {
	tables       []TableSchema
	byID         map[TableID]*TableSchema
	byName       map[string]*TableSchema
	tableIDs     []TableID // user tables first, then system
	reducers     map[string]types.ReducerHandler
	reducerNames []string
	onConnect    func(*types.ReducerContext) error
	onDisconnect func(*types.ReducerContext) error
	version      uint32
}

func newSchemaRegistry(schemas []TableSchema, userTableCount int, b *Builder) SchemaRegistry {
	r := &schemaRegistry{
		tables:   schemas,
		byID:     make(map[TableID]*TableSchema, len(schemas)),
		byName:   make(map[string]*TableSchema, len(schemas)),
		tableIDs: make([]TableID, len(schemas)),
		reducers: make(map[string]types.ReducerHandler, len(b.reducers)),
		version:  b.version,
	}

	for i := range schemas {
		r.byID[schemas[i].ID] = &r.tables[i]
		r.byName[schemas[i].Name] = &r.tables[i]
		r.tableIDs[i] = schemas[i].ID
	}

	for _, name := range b.reducerOrder {
		entry := b.reducers[name]
		r.reducers[name] = entry.handler
		r.reducerNames = append(r.reducerNames, name)
	}

	r.onConnect = b.onConnect
	r.onDisconnect = b.onDisconnect
	_ = userTableCount

	return r
}

func (r *schemaRegistry) Table(id TableID) (*TableSchema, bool) {
	ts, ok := r.byID[id]
	return ts, ok
}

func (r *schemaRegistry) TableByName(name string) (*TableSchema, bool) {
	ts, ok := r.byName[name]
	return ts, ok
}

func (r *schemaRegistry) Tables() []TableID {
	out := make([]TableID, len(r.tableIDs))
	copy(out, r.tableIDs)
	return out
}

func (r *schemaRegistry) Reducer(name string) (types.ReducerHandler, bool) {
	h, ok := r.reducers[name]
	return h, ok
}

func (r *schemaRegistry) Reducers() []string {
	out := make([]string, len(r.reducerNames))
	copy(out, r.reducerNames)
	return out
}

func (r *schemaRegistry) OnConnect() func(*types.ReducerContext) error {
	return r.onConnect
}

func (r *schemaRegistry) OnDisconnect() func(*types.ReducerContext) error {
	return r.onDisconnect
}

func (r *schemaRegistry) Version() uint32 {
	return r.version
}

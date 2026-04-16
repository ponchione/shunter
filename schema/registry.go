package schema

// SchemaRegistry is a read-only view of all registered tables, indexes, and reducers.
// Safe for concurrent use — immutable after construction.
type SchemaRegistry interface {
	Table(id TableID) (*TableSchema, bool)
	TableByName(name string) (*TableSchema, bool)
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

func (r *schemaRegistry) TableByName(name string) (*TableSchema, bool) {
	i, ok := r.byName[name]
	if !ok {
		return nil, false
	}
	ts := cloneTableSchema(r.tables[i])
	return &ts, true
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

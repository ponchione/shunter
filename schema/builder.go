package schema

// Builder accumulates table definitions, reducers, and engine configuration
// before Build() validates and freezes everything.
type Builder struct {
	tables                    []TableDefinition
	reducers                  map[string]reducerEntry
	reducerOrder              []string
	onConnect                 func(*ReducerContext) error
	onDisconnect              func(*ReducerContext) error
	onConnectRegistrations    int
	onDisconnectRegistrations int
	version                   uint32
	versionSet                bool
	built                     bool
}

type reducerEntry struct {
	handler ReducerHandler
	count   int // registration count for duplicate detection
}

// NewBuilder returns a new empty Builder.
func NewBuilder() *Builder {
	return &Builder{
		reducers: make(map[string]reducerEntry),
	}
}

// TableDefinition describes a table before validation and ID assignment.
type TableDefinition struct {
	Name       string
	Columns    []ColumnDefinition
	Indexes    []IndexDefinition
	ReadPolicy ReadPolicy
}

// ColumnDefinition describes a column in a table definition.
type ColumnDefinition struct {
	Name          string
	Type          ValueKind
	PrimaryKey    bool
	Nullable      bool
	AutoIncrement bool
}

// IndexDefinition describes a secondary index.
type IndexDefinition struct {
	Name    string
	Columns []string // column names, in key order
	Unique  bool
}

// TableOption configures a table registration.
type TableOption func(*tableOptions)

type tableOptions struct {
	name       string
	readPolicy *ReadPolicy
}

// WithTableName overrides the table name derived from the Go type name.
func WithTableName(name string) TableOption {
	return func(o *tableOptions) {
		o.name = name
	}
}

// WithPrivateRead marks a table private to external raw SQL reads.
func WithPrivateRead() TableOption {
	return func(o *tableOptions) {
		policy := ReadPolicy{Access: TableAccessPrivate}
		o.readPolicy = &policy
	}
}

// WithPublicRead marks a table readable by external raw SQL without
// permission tags.
func WithPublicRead() TableOption {
	return func(o *tableOptions) {
		policy := ReadPolicy{Access: TableAccessPublic}
		o.readPolicy = &policy
	}
}

// WithReadPermissions marks a table readable by external raw SQL only when all
// supplied permission tags are present.
func WithReadPermissions(permissions ...string) TableOption {
	copied := append([]string(nil), permissions...)
	return func(o *tableOptions) {
		policy := ReadPolicy{
			Access:      TableAccessPermissioned,
			Permissions: append([]string(nil), copied...),
		}
		o.readPolicy = &policy
	}
}

// TableDef registers a table definition with the builder.
func (b *Builder) TableDef(def TableDefinition, opts ...TableOption) *Builder {
	var o tableOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.name != "" {
		def.Name = o.name
	}
	if o.readPolicy != nil {
		def.ReadPolicy = copyReadPolicy(*o.readPolicy)
	} else {
		def.ReadPolicy = copyReadPolicy(def.ReadPolicy)
	}
	b.tables = append(b.tables, def)
	return b
}

// SchemaVersion sets the schema version used for compatibility checking.
func (b *Builder) SchemaVersion(v uint32) *Builder {
	b.version = v
	b.versionSet = true
	return b
}

// Reducer registers a named reducer handler. Duplicate names are preserved
// for detection during Build() validation.
func (b *Builder) Reducer(name string, h ReducerHandler) *Builder {
	e := b.reducers[name]
	if e.count == 0 {
		b.reducerOrder = append(b.reducerOrder, name)
	}
	e.handler = h
	e.count++
	b.reducers[name] = e
	return b
}

// OnConnect registers the lifecycle handler invoked when a client connects.
// Duplicate registrations are tracked for Build() validation.
func (b *Builder) OnConnect(h func(*ReducerContext) error) *Builder {
	b.onConnect = h
	b.onConnectRegistrations++
	return b
}

// OnDisconnect registers the lifecycle handler invoked when a client disconnects.
// Duplicate registrations are tracked for Build() validation.
func (b *Builder) OnDisconnect(h func(*ReducerContext) error) *Builder {
	b.onDisconnect = h
	b.onDisconnectRegistrations++
	return b
}

// EngineOptions configures runtime engine behavior.
// Zero-value defaults:
//   - DataDir: current working directory / runtime default chosen at Start()
//   - ExecutorQueueCapacity: runtime default chosen at Start()
//   - DurabilityQueueCapacity: runtime default chosen at Start()
//   - EnableProtocol: false
type EngineOptions struct {
	DataDir                 string
	ExecutorQueueCapacity   int
	DurabilityQueueCapacity int
	EnableProtocol          bool
	StartupSnapshotSchema   *SnapshotSchema
}

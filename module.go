package shunter

import "github.com/ponchione/shunter/schema"

// Module owns an application module definition before it is built into a
// Runtime. It stores module identity metadata and delegates schema, reducer,
// and lifecycle registration to the underlying schema builder.
type Module struct {
	name     string
	version  string
	metadata map[string]string
	builder  *schema.Builder
}

// NewModule creates a module definition shell with the supplied name.
// Blank names are allowed at construction time and rejected by Build.
func NewModule(name string) *Module {
	return &Module{
		name:    name,
		builder: schema.NewBuilder(),
	}
}

// Name returns the exact module name supplied to NewModule.
func (m *Module) Name() string {
	return m.name
}

// Version stores the module's application version string and returns the
// receiver for fluent module declarations.
func (m *Module) Version(v string) *Module {
	m.version = v
	return m
}

// VersionString returns the module's application version string.
func (m *Module) VersionString() string {
	return m.version
}

// SchemaVersion sets the module schema version through the underlying schema
// builder and returns the receiver for fluent module declarations.
func (m *Module) SchemaVersion(v uint32) *Module {
	m.builder.SchemaVersion(v)
	return m
}

// TableDef registers a table definition through the underlying schema builder
// and returns the receiver for fluent module declarations.
func (m *Module) TableDef(def schema.TableDefinition, opts ...schema.TableOption) *Module {
	m.builder.TableDef(def, opts...)
	return m
}

// Reducer registers a named reducer through the underlying schema builder and
// returns the receiver for fluent module declarations.
func (m *Module) Reducer(name string, h schema.ReducerHandler) *Module {
	m.builder.Reducer(name, h)
	return m
}

// OnConnect registers a connection lifecycle handler through the underlying
// schema builder and returns the receiver for fluent module declarations.
func (m *Module) OnConnect(h func(*schema.ReducerContext) error) *Module {
	m.builder.OnConnect(h)
	return m
}

// OnDisconnect registers a disconnection lifecycle handler through the
// underlying schema builder and returns the receiver for fluent module
// declarations.
func (m *Module) OnDisconnect(h func(*schema.ReducerContext) error) *Module {
	m.builder.OnDisconnect(h)
	return m
}

// Metadata replaces the module metadata with a defensive copy of values.
// Passing nil clears metadata.
func (m *Module) Metadata(values map[string]string) *Module {
	if values == nil {
		m.metadata = nil
		return m
	}

	m.metadata = copyStringMap(values)
	return m
}

// MetadataMap returns a defensive copy of the module metadata.
func (m *Module) MetadataMap() map[string]string {
	return copyStringMap(m.metadata)
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

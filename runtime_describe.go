package shunter

import "github.com/ponchione/shunter/schema"

// ModuleDescription is a detached snapshot of authored module identity and
// declaration metadata.
type ModuleDescription struct {
	Name     string
	Version  string
	Metadata map[string]string
	Queries  []QueryDescription
	Views    []ViewDescription
}

// QueryDescription is a detached declaration summary for a named read query.
type QueryDescription struct {
	Name string
}

// ViewDescription is a detached declaration summary for a named live view or
// subscription.
type ViewDescription struct {
	Name string
}

// RuntimeDescription is a detached snapshot of V1 runtime diagnostics.
type RuntimeDescription struct {
	Module ModuleDescription
	Health RuntimeHealth
}

// Describe returns a detached snapshot of the authored module identity and
// declaration metadata.
func (m *Module) Describe() ModuleDescription {
	if m == nil {
		return ModuleDescription{Metadata: map[string]string{}}
	}
	return ModuleDescription{
		Name:     m.name,
		Version:  m.version,
		Metadata: m.MetadataMap(),
		Queries:  describeQueryDeclarations(m.queries),
		Views:    describeViewDeclarations(m.views),
	}
}

// ExportSchema returns a detached schema snapshot for diagnostics and tooling.
func (r *Runtime) ExportSchema() *schema.SchemaExport {
	if r == nil || r.engine == nil {
		return &schema.SchemaExport{}
	}
	return r.engine.ExportSchema()
}

// Describe returns a detached snapshot of module identity and runtime health.
func (r *Runtime) Describe() RuntimeDescription {
	if r == nil {
		return RuntimeDescription{Module: ModuleDescription{Metadata: map[string]string{}}}
	}
	return RuntimeDescription{
		Module: ModuleDescription{
			Name:     r.moduleName,
			Version:  r.moduleVersion,
			Metadata: copyStringMap(r.moduleMetadata),
			Queries:  describeQueryDeclarations(r.moduleQueries),
			Views:    describeViewDeclarations(r.moduleViews),
		},
		Health: r.Health(),
	}
}

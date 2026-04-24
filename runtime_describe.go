package shunter

import "github.com/ponchione/shunter/schema"

// ModuleDescription is a detached snapshot of authored module identity metadata.
type ModuleDescription struct {
	Name     string
	Version  string
	Metadata map[string]string
}

// RuntimeDescription is a detached snapshot of V1 runtime diagnostics.
type RuntimeDescription struct {
	Module ModuleDescription
	Health RuntimeHealth
}

// Describe returns a detached snapshot of the authored module identity metadata.
func (m *Module) Describe() ModuleDescription {
	if m == nil {
		return ModuleDescription{Metadata: map[string]string{}}
	}
	return ModuleDescription{
		Name:     m.name,
		Version:  m.version,
		Metadata: m.MetadataMap(),
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
		},
		Health: r.Health(),
	}
}

package shunter

import "github.com/ponchione/shunter/schema"

// ModuleDescription is a detached snapshot of authored module identity and
// declaration metadata.
type ModuleDescription struct {
	Name              string
	Version           string
	Metadata          map[string]string
	Queries           []QueryDescription
	Views             []ViewDescription
	VisibilityFilters []VisibilityFilterDescription
	Migration         MigrationMetadata
	TableMigrations   map[string]MigrationMetadata
}

// QueryDescription is a detached declaration summary for a named read query.
type QueryDescription struct {
	Name        string             `json:"name"`
	SQL         string             `json:"sql,omitempty"`
	Parameters  *ProductSchema     `json:"parameters,omitempty"`
	RowSchema   *ProductSchema     `json:"row_schema,omitempty"`
	ResultShape *ReadResultShape   `json:"result_shape,omitempty"`
	Permissions PermissionMetadata `json:"-"`
	ReadModel   ReadModelMetadata  `json:"-"`
	Migration   MigrationMetadata  `json:"-"`
}

// ViewDescription is a detached declaration summary for a named live view or
// subscription.
type ViewDescription struct {
	Name        string             `json:"name"`
	SQL         string             `json:"sql,omitempty"`
	Parameters  *ProductSchema     `json:"parameters,omitempty"`
	RowSchema   *ProductSchema     `json:"row_schema,omitempty"`
	ResultShape *ReadResultShape   `json:"result_shape,omitempty"`
	Permissions PermissionMetadata `json:"-"`
	ReadModel   ReadModelMetadata  `json:"-"`
	Migration   MigrationMetadata  `json:"-"`
}

// ProductSchema is the exported shape of one BSATN product row.
type ProductSchema = schema.ProductSchemaExport

// ProductColumn is one exported product-schema column.
type ProductColumn = schema.ProductColumnExport

const (
	// ReadResultShapeTable is a table-shaped declared read result.
	ReadResultShapeTable = "table"
	// ReadResultShapeProjection is a projection-shaped declared read result.
	ReadResultShapeProjection = "projection"
	// ReadResultShapeAggregate is a single-row aggregate declared read result.
	ReadResultShapeAggregate = "aggregate"
)

// ReadResultShape records the typed row-shape class for an executable declared
// query or view.
type ReadResultShape struct {
	Kind               string `json:"kind"`
	Table              string `json:"table,omitempty"`
	UsesCallerIdentity bool   `json:"uses_caller_identity,omitempty"`
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
		Name:              m.name,
		Version:           m.version,
		Metadata:          m.MetadataMap(),
		Queries:           describeQueryDeclarations(m.queries),
		Views:             describeViewDeclarations(m.views),
		VisibilityFilters: describeVisibilityFilterDeclarations(m.visibilityFilters),
		Migration:         copyMigrationMetadata(m.migration),
		TableMigrations:   copyMigrationMetadataMap(m.tableMigrations),
	}
}

// ExportSchema returns a detached schema snapshot for diagnostics and tooling.
func (r *Runtime) ExportSchema() *schema.SchemaExport {
	if r == nil || r.engine == nil {
		return &schema.SchemaExport{}
	}
	out := copySchemaExport(r.engine.ExportSchema())
	out = applyReducerProductSchemas(out, r.module.reducerDeclarations())
	return &out
}

// Describe returns a detached snapshot of module identity and runtime health.
func (r *Runtime) Describe() RuntimeDescription {
	if r == nil {
		return RuntimeDescription{Module: ModuleDescription{Metadata: map[string]string{}}}
	}
	return RuntimeDescription{
		Module: r.module.describe(),
		Health: r.Health(),
	}
}

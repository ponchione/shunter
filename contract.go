package shunter

import (
	"encoding/json"
	"strings"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
)

const (
	// ModuleContractVersion is the canonical contract artifact version.
	ModuleContractVersion uint32 = 1

	// ModuleContractFormat identifies the JSON artifact consumed by tooling.
	ModuleContractFormat = "shunter.module_contract"

	// DefaultContractSnapshotFilename is the recommended committed snapshot path.
	DefaultContractSnapshotFilename = "shunter.contract.json"
)

// ModuleContract is the canonical full module contract artifact.
type ModuleContract struct {
	ContractVersion   uint32                        `json:"contract_version"`
	Module            ModuleContractIdentity        `json:"module"`
	Schema            schema.SchemaExport           `json:"schema"`
	Procedures        []ProcedureDescription        `json:"procedures"`
	Queries           []QueryDescription            `json:"queries"`
	Views             []ViewDescription             `json:"views"`
	VisibilityFilters []VisibilityFilterDescription `json:"visibility_filters"`
	Permissions       PermissionContract            `json:"permissions"`
	ReadModel         ReadModelContract             `json:"read_model"`
	Migrations        MigrationContract             `json:"migrations"`
	Codegen           CodegenContractMetadata       `json:"codegen"`
}

// ModuleContractIdentity is the module identity section of a contract.
type ModuleContractIdentity struct {
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Metadata map[string]string `json:"metadata"`
}

// PermissionContract records passive permission metadata for exported surfaces.
type PermissionContract struct {
	Reducers   []PermissionContractDeclaration `json:"reducers"`
	Procedures []PermissionContractDeclaration `json:"procedures"`
	Queries    []PermissionContractDeclaration `json:"queries"`
	Views      []PermissionContractDeclaration `json:"views"`
}

// PermissionContractDeclaration describes required permission tags for one
// exported surface.
type PermissionContractDeclaration struct {
	Name     string   `json:"name"`
	Required []string `json:"required"`
}

// ReadModelContract records passive read-model metadata for exported read
// surfaces.
type ReadModelContract struct {
	Declarations []ReadModelContractDeclaration `json:"declarations"`
}

// ReadModelContractDeclaration describes read-model tags for one query or view.
type ReadModelContractDeclaration struct {
	Surface string   `json:"surface"`
	Name    string   `json:"name"`
	Tables  []string `json:"tables"`
	Tags    []string `json:"tags"`
}

// MigrationContract records descriptive migration metadata for review tooling.
type MigrationContract struct {
	Module       MigrationMetadata              `json:"module"`
	Declarations []MigrationContractDeclaration `json:"declarations"`
}

// MigrationContractDeclaration records descriptive migration metadata for one
// exported declaration.
type MigrationContractDeclaration struct {
	Surface  string            `json:"surface"`
	Name     string            `json:"name"`
	Metadata MigrationMetadata `json:"metadata"`
}

// CodegenContractMetadata records stable export metadata for later codegen.
type CodegenContractMetadata struct {
	ContractFormat          string `json:"contract_format"`
	ContractVersion         uint32 `json:"contract_version"`
	DefaultSnapshotFilename string `json:"default_snapshot_filename"`
}

// ExportContract returns a detached full module contract snapshot.
func (r *Runtime) ExportContract() ModuleContract {
	if r == nil {
		return emptyModuleContract()
	}

	desc := r.Describe()
	procedures := normalizeProcedureDescriptions(desc.Module.Procedures)
	queries := copyQueryDescriptions(desc.Module.Queries)
	views := copyViewDescriptions(desc.Module.Views)
	schemaExport := copySchemaExport(r.ExportSchema())
	reducers := r.module.reducerDeclarations()
	schemaExport = applyReducerProductSchemas(schemaExport, reducers)
	queries = withDeclaredReadResultMetadata(queries, schemaExport)
	views = withDeclaredViewResultMetadata(views, schemaExport)
	return ModuleContract{
		ContractVersion: ModuleContractVersion,
		Module: ModuleContractIdentity{
			Name:     desc.Module.Name,
			Version:  desc.Module.Version,
			Metadata: copyStringMap(desc.Module.Metadata),
		},
		Schema:            schemaExport,
		Procedures:        procedures,
		Queries:           queries,
		Views:             views,
		VisibilityFilters: normalizeVisibilityFilterDescriptions(desc.Module.VisibilityFilters),
		Permissions:       buildPermissionContract(reducers, procedures, queries, views),
		ReadModel:         buildReadModelContract(queries, views),
		Migrations:        buildMigrationContract(schemaExport, desc.Module.Migration, desc.Module.TableMigrations, queries, views),
		Codegen:           defaultCodegenContractMetadata(),
	}
}

// ExportContractJSON returns deterministic, review-friendly canonical JSON.
func (r *Runtime) ExportContractJSON() ([]byte, error) {
	return r.ExportContract().MarshalCanonicalJSON()
}

// MarshalCanonicalJSON returns deterministic, review-friendly canonical JSON.
func (c ModuleContract) MarshalCanonicalJSON() ([]byte, error) {
	c = normalizeModuleContract(c)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	return data, nil
}

func normalizeModuleContract(c ModuleContract) ModuleContract {
	if c.Module.Metadata == nil {
		c.Module.Metadata = map[string]string{}
	}
	c.Schema = normalizeSchemaExport(c.Schema)
	c.Procedures = normalizeProcedureDescriptions(c.Procedures)
	c.Queries = copyQueryDescriptions(c.Queries)
	c.Views = copyViewDescriptions(c.Views)
	c.VisibilityFilters = normalizeVisibilityFilterDescriptions(c.VisibilityFilters)
	c.Permissions.Reducers = normalizePermissionContractDeclarations(c.Permissions.Reducers)
	c.Permissions.Procedures = normalizePermissionContractDeclarations(c.Permissions.Procedures)
	c.Permissions.Queries = normalizePermissionContractDeclarations(c.Permissions.Queries)
	c.Permissions.Views = normalizePermissionContractDeclarations(c.Permissions.Views)
	c.ReadModel.Declarations = normalizeReadModelContractDeclarations(c.ReadModel.Declarations)
	c.Migrations.Module = normalizeMigrationMetadata(c.Migrations.Module)
	c.Migrations.Declarations = normalizeMigrationContractDeclarations(c.Migrations.Declarations)
	return c
}

func normalizePermissionContractDeclarations(in []PermissionContractDeclaration) []PermissionContractDeclaration {
	out := normalizeSlice(in)
	for i := range out {
		out[i].Required = normalizeStringSlice(out[i].Required)
	}
	return out
}

func normalizeReadModelContractDeclarations(in []ReadModelContractDeclaration) []ReadModelContractDeclaration {
	out := normalizeSlice(in)
	for i := range out {
		out[i].Tables = normalizeStringSlice(out[i].Tables)
		out[i].Tags = normalizeStringSlice(out[i].Tags)
	}
	return out
}

func normalizeMigrationContractDeclarations(in []MigrationContractDeclaration) []MigrationContractDeclaration {
	out := normalizeSlice(in)
	for i := range out {
		out[i].Metadata = normalizeMigrationMetadata(out[i].Metadata)
	}
	return out
}

func emptyModuleContract() ModuleContract {
	return ModuleContract{
		ContractVersion: ModuleContractVersion,
		Module: ModuleContractIdentity{
			Metadata: map[string]string{},
		},
		Schema: schema.SchemaExport{
			Tables:   []schema.TableExport{},
			Reducers: []schema.ReducerExport{},
		},
		Procedures:        []ProcedureDescription{},
		Queries:           []QueryDescription{},
		Views:             []ViewDescription{},
		VisibilityFilters: []VisibilityFilterDescription{},
		Permissions:       emptyPermissionContract(),
		ReadModel:         emptyReadModelContract(),
		Migrations:        emptyMigrationContract(),
		Codegen:           defaultCodegenContractMetadata(),
	}
}

func emptyPermissionContract() PermissionContract {
	return PermissionContract{
		Reducers:   []PermissionContractDeclaration{},
		Procedures: []PermissionContractDeclaration{},
		Queries:    []PermissionContractDeclaration{},
		Views:      []PermissionContractDeclaration{},
	}
}

func emptyReadModelContract() ReadModelContract {
	return ReadModelContract{
		Declarations: []ReadModelContractDeclaration{},
	}
}

func buildPermissionContract(reducers []ReducerDeclaration, procedures []ProcedureDescription, queries []QueryDescription, views []ViewDescription) PermissionContract {
	out := emptyPermissionContract()
	for _, reducer := range reducers {
		out.Reducers = appendPermissionContractDeclaration(out.Reducers, reducer.Name, reducer.Permissions)
	}
	for _, procedure := range procedures {
		out.Procedures = appendPermissionContractDeclaration(out.Procedures, procedure.Name, procedure.Permissions)
	}
	for _, query := range queries {
		out.Queries = appendPermissionContractDeclaration(out.Queries, query.Name, query.Permissions)
	}
	for _, view := range views {
		out.Views = appendPermissionContractDeclaration(out.Views, view.Name, view.Permissions)
	}
	return out
}

func appendPermissionContractDeclaration(out []PermissionContractDeclaration, name string, metadata PermissionMetadata) []PermissionContractDeclaration {
	if !hasPermissionMetadata(metadata) {
		return out
	}
	return append(out, PermissionContractDeclaration{
		Name:     name,
		Required: normalizeStringSlice(metadata.Required),
	})
}

func buildReadModelContract(queries []QueryDescription, views []ViewDescription) ReadModelContract {
	out := emptyReadModelContract()
	for _, query := range queries {
		out.Declarations = appendReadModelContractDeclaration(out.Declarations, ReadModelSurfaceQuery, query.Name, query.ReadModel)
	}
	for _, view := range views {
		out.Declarations = appendReadModelContractDeclaration(out.Declarations, ReadModelSurfaceView, view.Name, view.ReadModel)
	}
	return out
}

func appendReadModelContractDeclaration(out []ReadModelContractDeclaration, surface, name string, metadata ReadModelMetadata) []ReadModelContractDeclaration {
	if !hasReadModelMetadata(metadata) {
		return out
	}
	return append(out, ReadModelContractDeclaration{
		Surface: surface,
		Name:    name,
		Tables:  normalizeStringSlice(metadata.Tables),
		Tags:    normalizeStringSlice(metadata.Tags),
	})
}

func emptyMigrationContract() MigrationContract {
	return MigrationContract{
		Module:       normalizeMigrationMetadata(MigrationMetadata{}),
		Declarations: []MigrationContractDeclaration{},
	}
}

func buildMigrationContract(schemaExport schema.SchemaExport, module MigrationMetadata, tableMigrations map[string]MigrationMetadata, queries []QueryDescription, views []ViewDescription) MigrationContract {
	out := emptyMigrationContract()
	out.Module = normalizeMigrationMetadata(module)

	for _, table := range schemaExport.Tables {
		if metadata, ok := tableMigrations[table.Name]; ok {
			out.Declarations = appendMigrationContractDeclaration(out.Declarations, MigrationSurfaceTable, table.Name, metadata)
		}
	}

	for _, query := range queries {
		out.Declarations = appendMigrationContractDeclaration(out.Declarations, MigrationSurfaceQuery, query.Name, query.Migration)
	}
	for _, view := range views {
		out.Declarations = appendMigrationContractDeclaration(out.Declarations, MigrationSurfaceView, view.Name, view.Migration)
	}

	return out
}

func appendMigrationContractDeclaration(out []MigrationContractDeclaration, surface, name string, metadata MigrationMetadata) []MigrationContractDeclaration {
	if !hasMigrationMetadata(metadata) {
		return out
	}
	return append(out, migrationContractDeclaration(surface, name, metadata))
}

func migrationContractDeclaration(surface, name string, metadata MigrationMetadata) MigrationContractDeclaration {
	return MigrationContractDeclaration{
		Surface:  surface,
		Name:     name,
		Metadata: normalizeMigrationMetadata(metadata),
	}
}

func normalizeMigrationMetadata(in MigrationMetadata) MigrationMetadata {
	out := copyMigrationMetadata(in)
	if out.Classifications == nil {
		out.Classifications = []MigrationClassification{}
	}
	return out
}

func defaultCodegenContractMetadata() CodegenContractMetadata {
	return CodegenContractMetadata{
		ContractFormat:          ModuleContractFormat,
		ContractVersion:         ModuleContractVersion,
		DefaultSnapshotFilename: DefaultContractSnapshotFilename,
	}
}

func applyReducerProductSchemas(schemaExport schema.SchemaExport, reducers []ReducerDeclaration) schema.SchemaExport {
	if len(reducers) == 0 || len(schemaExport.Reducers) == 0 {
		return schemaExport
	}
	byName := make(map[string]ReducerDeclaration, len(reducers))
	for _, reducer := range reducers {
		byName[reducer.Name] = reducer
	}
	for i := range schemaExport.Reducers {
		reducer := &schemaExport.Reducers[i]
		if reducer.Lifecycle {
			continue
		}
		declaration, ok := byName[reducer.Name]
		if !ok {
			continue
		}
		reducer.Args = copyProductSchemaPtr(declaration.Args)
		reducer.Result = copyProductSchemaPtr(declaration.Result)
	}
	return schemaExport
}

func withDeclaredReadResultMetadata(queries []QueryDescription, schemaExport schema.SchemaExport) []QueryDescription {
	if len(queries) == 0 {
		return queries
	}
	lookup := newContractSchemaLookup(schemaExport)
	out := make([]QueryDescription, len(queries))
	for i, query := range queries {
		out[i] = copyQueryDescription(query)
		rowSchema, resultShape := declaredReadResultMetadata(query.SQL, query.Parameters, lookup)
		out[i].RowSchema = rowSchema
		out[i].ResultShape = resultShape
	}
	return out
}

func withDeclaredViewResultMetadata(views []ViewDescription, schemaExport schema.SchemaExport) []ViewDescription {
	if len(views) == 0 {
		return views
	}
	lookup := newContractSchemaLookup(schemaExport)
	out := make([]ViewDescription, len(views))
	for i, view := range views {
		out[i] = copyViewDescription(view)
		rowSchema, resultShape := declaredReadResultMetadata(view.SQL, view.Parameters, lookup)
		out[i].RowSchema = rowSchema
		out[i].ResultShape = resultShape
	}
	return out
}

func declaredReadResultMetadata(sqlText string, parameters *ProductSchema, lookup contractSchemaLookup) (*ProductSchema, *ReadResultShape) {
	if strings.TrimSpace(sqlText) == "" {
		return nil, nil
	}
	compiled, err := compileDeclaredReadSQLTemplate(sqlText, lookup, declaredReadSQLValidation, parameters)
	if err != nil {
		return nil, nil
	}
	columns := compiled.ResultColumns(lookup)
	rowSchema := productSchemaForColumnSchemas(columns)
	return rowSchema, readResultShapeForCompiled(compiled)
}

func productSchemaForColumnSchemas(columns []schema.ColumnSchema) *ProductSchema {
	product := &ProductSchema{Columns: make([]ProductColumn, len(columns))}
	for i, column := range columns {
		product.Columns[i] = ProductColumn{
			Name:     column.Name,
			Type:     schema.ValueKindExportString(column.Type),
			Nullable: column.Nullable,
		}
	}
	return product
}

type declaredReadResultMetadataSource interface {
	TableName() string
	HasAggregate() bool
	SubscriptionProjection() []subscription.ProjectionColumn
	UsesCallerIdentity() bool
}

func readResultShapeForCompiled(compiled declaredReadResultMetadataSource) *ReadResultShape {
	kind := ReadResultShapeTable
	switch {
	case compiled.HasAggregate():
		kind = ReadResultShapeAggregate
	case len(compiled.SubscriptionProjection()) != 0:
		kind = ReadResultShapeProjection
	}
	return &ReadResultShape{
		Kind:               kind,
		Table:              compiled.TableName(),
		UsesCallerIdentity: compiled.UsesCallerIdentity(),
	}
}

func copySchemaExport(in *schema.SchemaExport) schema.SchemaExport {
	if in == nil {
		return normalizeSchemaExport(schema.SchemaExport{})
	}
	return normalizeSchemaExport(*in)
}

func normalizeSchemaExport(in schema.SchemaExport) schema.SchemaExport {
	out := schema.SchemaExport{
		Version:  in.Version,
		Tables:   make([]schema.TableExport, len(in.Tables)),
		Reducers: make([]schema.ReducerExport, len(in.Reducers)),
	}
	for i, table := range in.Tables {
		out.Tables[i] = copyTableExport(table)
	}
	for i, reducer := range in.Reducers {
		out.Reducers[i] = copyReducerExport(reducer)
	}
	return out
}

func copyTableExport(in schema.TableExport) schema.TableExport {
	out := schema.TableExport{
		ID:         in.ID,
		Name:       in.Name,
		IsEvent:    in.IsEvent,
		Columns:    make([]schema.ColumnExport, len(in.Columns)),
		Indexes:    make([]schema.IndexExport, len(in.Indexes)),
		ReadPolicy: normalizeSchemaReadPolicy(in.ReadPolicy),
	}
	copy(out.Columns, in.Columns)
	for i, idx := range in.Indexes {
		out.Indexes[i] = schema.IndexExport{
			ID:             idx.ID,
			Name:           idx.Name,
			Columns:        normalizeStringSlice(idx.Columns),
			ColumnOrdinals: normalizeSlice(idx.ColumnOrdinals),
			Unique:         idx.Unique,
			Primary:        idx.Primary,
		}
	}
	return out
}

func copyReducerExport(in schema.ReducerExport) schema.ReducerExport {
	return schema.ReducerExport{
		Name:      in.Name,
		Lifecycle: in.Lifecycle,
		Args:      copyProductSchemaPtr(in.Args),
		Result:    copyProductSchemaPtr(in.Result),
	}
}

func normalizeSchemaReadPolicy(in schema.ReadPolicy) schema.ReadPolicy {
	return schema.ReadPolicy{
		Access:      in.Access,
		Permissions: normalizeStringSlice(in.Permissions),
	}
}

func copyQueryDescriptions(in []QueryDescription) []QueryDescription {
	return mapSlice(in, copyQueryDescription)
}

func normalizeProcedureDescriptions(in []ProcedureDescription) []ProcedureDescription {
	return mapSlice(in, copyProcedureDescription)
}

func copyViewDescriptions(in []ViewDescription) []ViewDescription {
	return mapSlice(in, copyViewDescription)
}

func copyProcedureDescription(procedure ProcedureDescription) ProcedureDescription {
	return ProcedureDescription{
		Name:        procedure.Name,
		Args:        copyProductSchemaPtr(procedure.Args),
		Result:      copyProductSchemaPtr(procedure.Result),
		Permissions: copyPermissionMetadata(procedure.Permissions),
	}
}

func copyQueryDescription(query QueryDescription) QueryDescription {
	return QueryDescription{
		Name:        query.Name,
		SQL:         query.SQL,
		Parameters:  copyProductSchemaPtr(query.Parameters),
		RowSchema:   copyProductSchemaPtr(query.RowSchema),
		ResultShape: copyReadResultShapePtr(query.ResultShape),
		Permissions: copyPermissionMetadata(query.Permissions),
		ReadModel:   copyReadModelMetadata(query.ReadModel),
		Migration:   copyMigrationMetadata(query.Migration),
	}
}

func copyViewDescription(view ViewDescription) ViewDescription {
	return ViewDescription{
		Name:        view.Name,
		SQL:         view.SQL,
		Parameters:  copyProductSchemaPtr(view.Parameters),
		RowSchema:   copyProductSchemaPtr(view.RowSchema),
		ResultShape: copyReadResultShapePtr(view.ResultShape),
		Permissions: copyPermissionMetadata(view.Permissions),
		ReadModel:   copyReadModelMetadata(view.ReadModel),
		Migration:   copyMigrationMetadata(view.Migration),
	}
}

func copyReadResultShapePtr(in *ReadResultShape) *ReadResultShape {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

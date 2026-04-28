package shunter

import (
	"encoding/json"
	"sort"

	"github.com/ponchione/shunter/schema"
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
	ContractVersion uint32                  `json:"contract_version"`
	Module          ModuleContractIdentity  `json:"module"`
	Schema          schema.SchemaExport     `json:"schema"`
	Queries         []QueryDescription      `json:"queries"`
	Views           []ViewDescription       `json:"views"`
	Permissions     PermissionContract      `json:"permissions"`
	ReadModel       ReadModelContract       `json:"read_model"`
	Migrations      MigrationContract       `json:"migrations"`
	Codegen         CodegenContractMetadata `json:"codegen"`
}

// ModuleContractIdentity is the module identity section of a contract.
type ModuleContractIdentity struct {
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Metadata map[string]string `json:"metadata"`
}

// PermissionContract records passive permission metadata for exported surfaces.
type PermissionContract struct {
	Reducers []PermissionContractDeclaration `json:"reducers"`
	Queries  []PermissionContractDeclaration `json:"queries"`
	Views    []PermissionContractDeclaration `json:"views"`
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
	queries := copyQueryDescriptions(desc.Module.Queries)
	views := copyViewDescriptions(desc.Module.Views)
	schemaExport := copySchemaExport(r.ExportSchema())
	return ModuleContract{
		ContractVersion: ModuleContractVersion,
		Module: ModuleContractIdentity{
			Name:     desc.Module.Name,
			Version:  desc.Module.Version,
			Metadata: copyStringMap(desc.Module.Metadata),
		},
		Schema:      schemaExport,
		Queries:     queries,
		Views:       views,
		Permissions: buildPermissionContract(r.moduleReducers, queries, views),
		ReadModel:   buildReadModelContract(queries, views),
		Migrations:  buildMigrationContract(schemaExport, desc.Module.Migration, desc.Module.TableMigrations, queries, views),
		Codegen:     defaultCodegenContractMetadata(),
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
	if c.Queries == nil {
		c.Queries = []QueryDescription{}
	}
	if c.Views == nil {
		c.Views = []ViewDescription{}
	}
	if c.Permissions.Reducers == nil {
		c.Permissions.Reducers = []PermissionContractDeclaration{}
	}
	for i := range c.Permissions.Reducers {
		c.Permissions.Reducers[i].Required = normalizeStringSlice(c.Permissions.Reducers[i].Required)
	}
	if c.Permissions.Queries == nil {
		c.Permissions.Queries = []PermissionContractDeclaration{}
	}
	for i := range c.Permissions.Queries {
		c.Permissions.Queries[i].Required = normalizeStringSlice(c.Permissions.Queries[i].Required)
	}
	if c.Permissions.Views == nil {
		c.Permissions.Views = []PermissionContractDeclaration{}
	}
	for i := range c.Permissions.Views {
		c.Permissions.Views[i].Required = normalizeStringSlice(c.Permissions.Views[i].Required)
	}
	if c.ReadModel.Declarations == nil {
		c.ReadModel.Declarations = []ReadModelContractDeclaration{}
	}
	for i := range c.ReadModel.Declarations {
		c.ReadModel.Declarations[i].Tables = normalizeStringSlice(c.ReadModel.Declarations[i].Tables)
		c.ReadModel.Declarations[i].Tags = normalizeStringSlice(c.ReadModel.Declarations[i].Tags)
	}
	if c.Migrations.Declarations == nil {
		c.Migrations.Declarations = []MigrationContractDeclaration{}
	}
	c.Migrations.Module = normalizeMigrationMetadata(c.Migrations.Module)
	for i := range c.Migrations.Declarations {
		c.Migrations.Declarations[i].Metadata = normalizeMigrationMetadata(c.Migrations.Declarations[i].Metadata)
	}
	return c
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
		Queries:     []QueryDescription{},
		Views:       []ViewDescription{},
		Permissions: emptyPermissionContract(),
		ReadModel:   emptyReadModelContract(),
		Migrations:  emptyMigrationContract(),
		Codegen:     defaultCodegenContractMetadata(),
	}
}

func emptyPermissionContract() PermissionContract {
	return PermissionContract{
		Reducers: []PermissionContractDeclaration{},
		Queries:  []PermissionContractDeclaration{},
		Views:    []PermissionContractDeclaration{},
	}
}

func emptyReadModelContract() ReadModelContract {
	return ReadModelContract{
		Declarations: []ReadModelContractDeclaration{},
	}
}

func buildPermissionContract(reducers []ReducerDeclaration, queries []QueryDescription, views []ViewDescription) PermissionContract {
	out := emptyPermissionContract()
	for _, reducer := range reducers {
		if !hasPermissionMetadata(reducer.Permissions) {
			continue
		}
		out.Reducers = append(out.Reducers, PermissionContractDeclaration{
			Name:     reducer.Name,
			Required: normalizeStringSlice(reducer.Permissions.Required),
		})
	}
	for _, query := range queries {
		if !hasPermissionMetadata(query.Permissions) {
			continue
		}
		out.Queries = append(out.Queries, PermissionContractDeclaration{
			Name:     query.Name,
			Required: normalizeStringSlice(query.Permissions.Required),
		})
	}
	for _, view := range views {
		if !hasPermissionMetadata(view.Permissions) {
			continue
		}
		out.Views = append(out.Views, PermissionContractDeclaration{
			Name:     view.Name,
			Required: normalizeStringSlice(view.Permissions.Required),
		})
	}
	return out
}

func buildReadModelContract(queries []QueryDescription, views []ViewDescription) ReadModelContract {
	out := emptyReadModelContract()
	for _, query := range queries {
		if !hasReadModelMetadata(query.ReadModel) {
			continue
		}
		out.Declarations = append(out.Declarations, ReadModelContractDeclaration{
			Surface: ReadModelSurfaceQuery,
			Name:    query.Name,
			Tables:  normalizeStringSlice(query.ReadModel.Tables),
			Tags:    normalizeStringSlice(query.ReadModel.Tags),
		})
	}
	for _, view := range views {
		if !hasReadModelMetadata(view.ReadModel) {
			continue
		}
		out.Declarations = append(out.Declarations, ReadModelContractDeclaration{
			Surface: ReadModelSurfaceView,
			Name:    view.Name,
			Tables:  normalizeStringSlice(view.ReadModel.Tables),
			Tags:    normalizeStringSlice(view.ReadModel.Tags),
		})
	}
	return out
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

	seenTables := make(map[string]struct{}, len(schemaExport.Tables))
	for _, table := range schemaExport.Tables {
		seenTables[table.Name] = struct{}{}
		if metadata, ok := tableMigrations[table.Name]; ok && hasMigrationMetadata(metadata) {
			out.Declarations = append(out.Declarations, migrationContractDeclaration(MigrationSurfaceTable, table.Name, metadata))
		}
	}

	var extraTables []string
	for name, metadata := range tableMigrations {
		if _, ok := seenTables[name]; ok || !hasMigrationMetadata(metadata) {
			continue
		}
		extraTables = append(extraTables, name)
	}
	sort.Strings(extraTables)
	for _, name := range extraTables {
		out.Declarations = append(out.Declarations, migrationContractDeclaration(MigrationSurfaceTable, name, tableMigrations[name]))
	}

	for _, query := range queries {
		if hasMigrationMetadata(query.Migration) {
			out.Declarations = append(out.Declarations, migrationContractDeclaration(MigrationSurfaceQuery, query.Name, query.Migration))
		}
	}
	for _, view := range views {
		if hasMigrationMetadata(view.Migration) {
			out.Declarations = append(out.Declarations, migrationContractDeclaration(MigrationSurfaceView, view.Name, view.Migration))
		}
	}

	return out
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

func copySchemaExport(in *schema.SchemaExport) schema.SchemaExport {
	if in == nil {
		return normalizeSchemaExport(schema.SchemaExport{})
	}

	out := schema.SchemaExport{
		Version:  in.Version,
		Tables:   make([]schema.TableExport, len(in.Tables)),
		Reducers: make([]schema.ReducerExport, len(in.Reducers)),
	}
	for i, table := range in.Tables {
		out.Tables[i] = copyTableExport(table)
	}
	copy(out.Reducers, in.Reducers)
	return out
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
	copy(out.Reducers, in.Reducers)
	if out.Tables == nil {
		out.Tables = []schema.TableExport{}
	}
	if out.Reducers == nil {
		out.Reducers = []schema.ReducerExport{}
	}
	return out
}

func copyTableExport(in schema.TableExport) schema.TableExport {
	out := schema.TableExport{
		Name:    in.Name,
		Columns: make([]schema.ColumnExport, len(in.Columns)),
		Indexes: make([]schema.IndexExport, len(in.Indexes)),
	}
	copy(out.Columns, in.Columns)
	for i, idx := range in.Indexes {
		columns := append([]string(nil), idx.Columns...)
		if columns == nil {
			columns = []string{}
		}
		out.Indexes[i] = schema.IndexExport{
			Name:    idx.Name,
			Columns: columns,
			Unique:  idx.Unique,
			Primary: idx.Primary,
		}
	}
	if out.Columns == nil {
		out.Columns = []schema.ColumnExport{}
	}
	if out.Indexes == nil {
		out.Indexes = []schema.IndexExport{}
	}
	return out
}

func copyQueryDescriptions(in []QueryDescription) []QueryDescription {
	if len(in) == 0 {
		return []QueryDescription{}
	}
	out := make([]QueryDescription, len(in))
	for i, query := range in {
		out[i] = QueryDescription{
			Name:        query.Name,
			Permissions: copyPermissionMetadata(query.Permissions),
			ReadModel:   copyReadModelMetadata(query.ReadModel),
			Migration:   copyMigrationMetadata(query.Migration),
		}
	}
	return out
}

func copyViewDescriptions(in []ViewDescription) []ViewDescription {
	if len(in) == 0 {
		return []ViewDescription{}
	}
	out := make([]ViewDescription, len(in))
	for i, view := range in {
		out[i] = ViewDescription{
			Name:        view.Name,
			Permissions: copyPermissionMetadata(view.Permissions),
			ReadModel:   copyReadModelMetadata(view.ReadModel),
			Migration:   copyMigrationMetadata(view.Migration),
		}
	}
	return out
}

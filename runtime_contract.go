package shunter

import (
	"encoding/json"

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

// PermissionContract reserves permission declarations for later V1.5 slices.
type PermissionContract struct {
	Reducers []PermissionContractDeclaration `json:"reducers"`
	Queries  []PermissionContractDeclaration `json:"queries"`
	Views    []PermissionContractDeclaration `json:"views"`
}

// PermissionContractDeclaration is a reserved permission declaration slot.
type PermissionContractDeclaration struct {
	Name string `json:"name"`
}

// ReadModelContract reserves read-model declarations for later V1.5 slices.
type ReadModelContract struct {
	Declarations []ReadModelContractDeclaration `json:"declarations"`
}

// ReadModelContractDeclaration is a reserved read-model declaration slot.
type ReadModelContractDeclaration struct {
	Name string `json:"name"`
}

// MigrationContract reserves migration declarations for later V1.5 slices.
type MigrationContract struct {
	Declarations []MigrationContractDeclaration `json:"declarations"`
}

// MigrationContractDeclaration is a reserved migration declaration slot.
type MigrationContractDeclaration struct {
	Name string `json:"name"`
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
	return ModuleContract{
		ContractVersion: ModuleContractVersion,
		Module: ModuleContractIdentity{
			Name:     desc.Module.Name,
			Version:  desc.Module.Version,
			Metadata: copyStringMap(desc.Module.Metadata),
		},
		Schema:      copySchemaExport(r.ExportSchema()),
		Queries:     copyQueryDescriptions(desc.Module.Queries),
		Views:       copyViewDescriptions(desc.Module.Views),
		Permissions: emptyPermissionContract(),
		ReadModel:   emptyReadModelContract(),
		Migrations:  emptyMigrationContract(),
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
	if c.Permissions.Queries == nil {
		c.Permissions.Queries = []PermissionContractDeclaration{}
	}
	if c.Permissions.Views == nil {
		c.Permissions.Views = []PermissionContractDeclaration{}
	}
	if c.ReadModel.Declarations == nil {
		c.ReadModel.Declarations = []ReadModelContractDeclaration{}
	}
	if c.Migrations.Declarations == nil {
		c.Migrations.Declarations = []MigrationContractDeclaration{}
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

func emptyMigrationContract() MigrationContract {
	return MigrationContract{
		Declarations: []MigrationContractDeclaration{},
	}
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
	copy(out, in)
	return out
}

func copyViewDescriptions(in []ViewDescription) []ViewDescription {
	if len(in) == 0 {
		return []ViewDescription{}
	}
	out := make([]ViewDescription, len(in))
	copy(out, in)
	return out
}

package shunter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ponchione/shunter/schema"
)

// ValidateModuleContract verifies that a detached ModuleContract is a
// canonical, internally consistent Shunter contract artifact.
func ValidateModuleContract(contract ModuleContract) error {
	return contract.Validate()
}

// Validate verifies that c is a canonical, internally consistent Shunter
// contract artifact.
func (c ModuleContract) Validate() error {
	var errs []error
	if c.ContractVersion != ModuleContractVersion {
		errs = append(errs, fmt.Errorf("contract_version = %d, want %d", c.ContractVersion, ModuleContractVersion))
	}
	if strings.TrimSpace(c.Module.Name) == "" {
		errs = append(errs, fmt.Errorf("module.name must not be empty"))
	}
	if c.Codegen.ContractFormat != ModuleContractFormat {
		errs = append(errs, fmt.Errorf("codegen.contract_format = %q, want %q", c.Codegen.ContractFormat, ModuleContractFormat))
	}
	if c.Codegen.ContractVersion != ModuleContractVersion {
		errs = append(errs, fmt.Errorf("codegen.contract_version = %d, want %d", c.Codegen.ContractVersion, ModuleContractVersion))
	}
	if c.Codegen.DefaultSnapshotFilename != DefaultContractSnapshotFilename {
		errs = append(errs, fmt.Errorf("codegen.default_snapshot_filename = %q, want %q", c.Codegen.DefaultSnapshotFilename, DefaultContractSnapshotFilename))
	}
	for key := range c.Module.Metadata {
		if strings.TrimSpace(key) == "" {
			errs = append(errs, fmt.Errorf("module.metadata key must not be empty"))
		}
	}

	tableNames := validateContractTables(c.Schema.Tables, &errs)
	reducerNames := validateContractReducers(c.Schema.Reducers, &errs)
	queryNames, viewNames := validateContractReadDeclarations(c.Queries, c.Views, &errs)

	validatePermissionContract("reducer", c.Permissions.Reducers, reducerNames, &errs)
	validatePermissionContract("query", c.Permissions.Queries, queryNames, &errs)
	validatePermissionContract("view", c.Permissions.Views, viewNames, &errs)
	validateReadModelContract(c.ReadModel.Declarations, tableNames, queryNames, viewNames, &errs)
	validateMigrationMetadata("migrations.module", c.Migrations.Module, &errs)
	validateMigrationContract(c.Migrations.Declarations, tableNames, queryNames, viewNames, &errs)

	return errors.Join(errs...)
}

func validateContractTables(tables []schema.TableExport, errs *[]error) map[string]struct{} {
	names := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		if !validateContractName("schema.tables", table.Name, names, errs) {
			continue
		}
		columnNames := make(map[string]struct{}, len(table.Columns))
		for _, column := range table.Columns {
			validateContractName("schema.tables."+table.Name+".columns", column.Name, columnNames, errs)
			if strings.TrimSpace(column.Type) == "" {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s.columns.%s type must not be empty", table.Name, column.Name))
			}
		}
		indexNames := make(map[string]struct{}, len(table.Indexes))
		for _, index := range table.Indexes {
			if !validateContractName("schema.tables."+table.Name+".indexes", index.Name, indexNames, errs) {
				continue
			}
			if len(index.Columns) == 0 {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s columns must not be empty", table.Name, index.Name))
			}
			for _, column := range index.Columns {
				if strings.TrimSpace(column) == "" {
					*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s column must not be empty", table.Name, index.Name))
					continue
				}
				if _, ok := columnNames[column]; !ok {
					*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s references unknown column %q", table.Name, index.Name, column))
				}
			}
		}
	}
	return names
}

func validateContractReducers(reducers []schema.ReducerExport, errs *[]error) map[string]struct{} {
	names := make(map[string]struct{}, len(reducers))
	for _, reducer := range reducers {
		validateContractName("schema.reducers", reducer.Name, names, errs)
	}
	return names
}

func validateContractReadDeclarations(queries []QueryDescription, views []ViewDescription, errs *[]error) (map[string]struct{}, map[string]struct{}) {
	queriesByName := make(map[string]struct{}, len(queries))
	viewsByName := make(map[string]struct{}, len(views))
	readNamespace := make(map[string]struct{}, len(queries)+len(views))
	for _, query := range queries {
		if validateContractName("queries", query.Name, queriesByName, errs) {
			validateContractName("read declarations", query.Name, readNamespace, errs)
		}
	}
	for _, view := range views {
		if validateContractName("views", view.Name, viewsByName, errs) {
			validateContractName("read declarations", view.Name, readNamespace, errs)
		}
	}
	return queriesByName, viewsByName
}

func validatePermissionContract(surface string, declarations []PermissionContractDeclaration, declaredNames map[string]struct{}, errs *[]error) {
	seen := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		if !validateContractName("permissions."+surface, declaration.Name, seen, errs) {
			continue
		}
		if _, ok := declaredNames[declaration.Name]; !ok {
			*errs = append(*errs, fmt.Errorf("permissions.%s.%s references unknown %s", surface, declaration.Name, surface))
		}
		for _, required := range declaration.Required {
			if strings.TrimSpace(required) == "" {
				*errs = append(*errs, fmt.Errorf("permissions.%s.%s requirement must not be empty", surface, declaration.Name))
			}
		}
	}
}

func validateReadModelContract(declarations []ReadModelContractDeclaration, tableNames, queryNames, viewNames map[string]struct{}, errs *[]error) {
	seen := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		switch declaration.Surface {
		case ReadModelSurfaceQuery:
			if _, ok := queryNames[declaration.Name]; !ok {
				*errs = append(*errs, fmt.Errorf("read_model.%s.%s references unknown query", declaration.Surface, declaration.Name))
			}
		case ReadModelSurfaceView:
			if _, ok := viewNames[declaration.Name]; !ok {
				*errs = append(*errs, fmt.Errorf("read_model.%s.%s references unknown view", declaration.Surface, declaration.Name))
			}
		default:
			*errs = append(*errs, fmt.Errorf("read_model surface %q is invalid", declaration.Surface))
		}
		if !validateContractName("read_model."+declaration.Surface, declaration.Name, seen, errs) {
			continue
		}
		for _, table := range declaration.Tables {
			if strings.TrimSpace(table) == "" {
				*errs = append(*errs, fmt.Errorf("read_model.%s.%s table must not be empty", declaration.Surface, declaration.Name))
				continue
			}
			if _, ok := tableNames[table]; !ok {
				*errs = append(*errs, fmt.Errorf("read_model.%s.%s references unknown table %q", declaration.Surface, declaration.Name, table))
			}
		}
		for _, tag := range declaration.Tags {
			if strings.TrimSpace(tag) == "" {
				*errs = append(*errs, fmt.Errorf("read_model.%s.%s tag must not be empty", declaration.Surface, declaration.Name))
			}
		}
	}
}

func validateMigrationContract(declarations []MigrationContractDeclaration, tableNames, queryNames, viewNames map[string]struct{}, errs *[]error) {
	seen := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		switch declaration.Surface {
		case MigrationSurfaceTable:
			if _, ok := tableNames[declaration.Name]; !ok {
				*errs = append(*errs, fmt.Errorf("migrations.%s.%s references unknown table", declaration.Surface, declaration.Name))
			}
		case MigrationSurfaceQuery:
			if _, ok := queryNames[declaration.Name]; !ok {
				*errs = append(*errs, fmt.Errorf("migrations.%s.%s references unknown query", declaration.Surface, declaration.Name))
			}
		case MigrationSurfaceView:
			if _, ok := viewNames[declaration.Name]; !ok {
				*errs = append(*errs, fmt.Errorf("migrations.%s.%s references unknown view", declaration.Surface, declaration.Name))
			}
		default:
			*errs = append(*errs, fmt.Errorf("migrations surface %q is invalid", declaration.Surface))
		}
		if validateContractName("migrations."+declaration.Surface, declaration.Name, seen, errs) {
			validateMigrationMetadata("migrations."+declaration.Surface+"."+declaration.Name, declaration.Metadata, errs)
		}
	}
}

func validateMigrationMetadata(path string, metadata MigrationMetadata, errs *[]error) {
	switch metadata.Compatibility {
	case "", MigrationCompatibilityCompatible, MigrationCompatibilityBreaking, MigrationCompatibilityUnknown:
	default:
		*errs = append(*errs, fmt.Errorf("%s.compatibility = %q is invalid", path, metadata.Compatibility))
	}
	for _, classification := range metadata.Classifications {
		switch classification {
		case MigrationClassificationAdditive,
			MigrationClassificationDeprecated,
			MigrationClassificationDataRewriteNeeded,
			MigrationClassificationManualReviewNeeded:
		default:
			*errs = append(*errs, fmt.Errorf("%s.classifications contains invalid value %q", path, classification))
		}
	}
}

func validateContractName(path, name string, seen map[string]struct{}, errs *[]error) bool {
	if strings.TrimSpace(name) == "" {
		*errs = append(*errs, fmt.Errorf("%s name must not be empty", path))
		return false
	}
	if _, ok := seen[name]; ok {
		*errs = append(*errs, fmt.Errorf("%s name %q is duplicated", path, name))
		return false
	}
	seen[name] = struct{}{}
	return true
}

package shunter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
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
	validateContractDeclarationSQL(c.Schema, c.Queries, c.Views, &errs)
	validateVisibilityFilterContract(c.VisibilityFilters, newContractSchemaLookup(c.Schema), &errs)

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
		if err := schema.ValidateReadPolicy(table.ReadPolicy); err != nil {
			*errs = append(*errs, fmt.Errorf("schema.tables.%s.read_policy invalid: %v", table.Name, err))
		}
		columnNames := make(map[string]struct{}, len(table.Columns))
		for _, column := range table.Columns {
			validateContractName("schema.tables."+table.Name+".columns", column.Name, columnNames, errs)
			if strings.TrimSpace(column.Type) == "" {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s.columns.%s type must not be empty", table.Name, column.Name))
			} else if _, ok := valueKindFromExportString(column.Type); !ok {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s.columns.%s type %q is invalid", table.Name, column.Name, column.Type))
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
			indexColumns := make(map[string]struct{}, len(index.Columns))
			for _, column := range index.Columns {
				if strings.TrimSpace(column) == "" {
					*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s column must not be empty", table.Name, index.Name))
					continue
				}
				if _, exists := indexColumns[column]; exists {
					*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s duplicate index column %q", table.Name, index.Name, column))
				}
				indexColumns[column] = struct{}{}
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

func validateContractDeclarationSQL(schemaExport schema.SchemaExport, queries []QueryDescription, views []ViewDescription, errs *[]error) {
	lookup := newContractSchemaLookup(schemaExport)
	for _, query := range queries {
		if strings.TrimSpace(query.SQL) == "" {
			continue
		}
		err := protocol.ValidateSQLQueryString(query.SQL, lookup, protocol.SQLQueryValidationOptions{
			AllowLimit:      true,
			AllowProjection: true,
			AllowOrderBy:    true,
			AllowOffset:     true,
		})
		if err != nil {
			*errs = append(*errs, fmt.Errorf("queries.%s.sql invalid: %v", query.Name, err))
		}
	}
	for _, view := range views {
		if strings.TrimSpace(view.SQL) == "" {
			continue
		}
		var caller types.Identity
		compiled, err := protocol.CompileSQLQueryString(view.SQL, lookup, &caller, protocol.SQLQueryValidationOptions{
			AllowLimit:      true,
			AllowProjection: true,
			AllowOrderBy:    true,
		})
		if err != nil {
			*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, err))
			continue
		}
		if aggregate := compiled.SubscriptionAggregate(); aggregate != nil {
			if err := subscription.ValidateAggregate(compiled.Predicate(), aggregate, lookup); err != nil {
				*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, err))
			}
			if compiled.HasOrderBy() {
				*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, fmt.Errorf("%w: live ORDER BY views do not support aggregate views", subscription.ErrInvalidPredicate)))
			}
			if compiled.HasLimit() {
				*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, fmt.Errorf("%w: live LIMIT views do not support aggregate views", subscription.ErrInvalidPredicate)))
			}
			if err := subscription.ValidateOrderBy(compiled.Predicate(), compiled.SubscriptionOrderBy(), aggregate, lookup); err != nil {
				*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, err))
			}
			if err := subscription.ValidateLimit(compiled.Predicate(), compiled.SubscriptionLimit(), aggregate, lookup); err != nil {
				*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, err))
			}
			continue
		}
		if err := subscription.ValidateProjection(compiled.Predicate(), compiled.SubscriptionProjection(), lookup); err != nil {
			*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, err))
		}
		if err := subscription.ValidateOrderBy(compiled.Predicate(), compiled.SubscriptionOrderBy(), compiled.SubscriptionAggregate(), lookup); err != nil {
			*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, err))
		}
		if err := subscription.ValidateLimit(compiled.Predicate(), compiled.SubscriptionLimit(), compiled.SubscriptionAggregate(), lookup); err != nil {
			*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, err))
		}
	}
}

func validateVisibilityFilterContract(filters []VisibilityFilterDescription, lookup contractSchemaLookup, errs *[]error) {
	seen := make(map[string]struct{}, len(filters))
	for _, filter := range filters {
		if !validateContractName("visibility_filters", filter.Name, seen, errs) {
			continue
		}
		expected, err := visibilityFilterDescription(VisibilityFilterDeclaration{
			Name: filter.Name,
			SQL:  filter.SQL,
		}, lookup)
		if err != nil {
			*errs = append(*errs, fmt.Errorf("visibility_filters.%s.sql invalid: %v", filter.Name, err))
			continue
		}
		if filter.ReturnTable != expected.ReturnTable {
			*errs = append(*errs, fmt.Errorf("visibility_filters.%s return_table = %q, want %q", filter.Name, filter.ReturnTable, expected.ReturnTable))
		}
		if filter.ReturnTableID != expected.ReturnTableID {
			*errs = append(*errs, fmt.Errorf("visibility_filters.%s return_table_id = %d, want %d", filter.Name, filter.ReturnTableID, expected.ReturnTableID))
		}
		if filter.UsesCallerIdentity != expected.UsesCallerIdentity {
			*errs = append(*errs, fmt.Errorf("visibility_filters.%s uses_caller_identity = %t, want %t", filter.Name, filter.UsesCallerIdentity, expected.UsesCallerIdentity))
		}
	}
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
		isReadPermission := surface == ReadModelSurfaceQuery || surface == ReadModelSurfaceView
		if isReadPermission && len(declaration.Required) == 0 {
			*errs = append(*errs, fmt.Errorf("permissions.%s.%s requirements must not be empty", surface, declaration.Name))
			continue
		}
		validatePermissionRequirements("permissions."+surface+"."+declaration.Name, declaration.Required, isReadPermission, errs)
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
		if validateContractSurfaceName("migrations."+declaration.Surface, declaration.Surface, declaration.Name, seen, errs) {
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

func validateContractSurfaceName(path, surface, name string, seen map[string]struct{}, errs *[]error) bool {
	if strings.TrimSpace(name) == "" {
		*errs = append(*errs, fmt.Errorf("%s name must not be empty", path))
		return false
	}
	key := surface + "\x00" + name
	if _, ok := seen[key]; ok {
		*errs = append(*errs, fmt.Errorf("%s name %q is duplicated", path, name))
		return false
	}
	seen[key] = struct{}{}
	return true
}

type contractSchemaLookup struct {
	tables []schema.TableSchema
	byID   map[schema.TableID]int
	byName map[string]int
}

func newContractSchemaLookup(schemaExport schema.SchemaExport) contractSchemaLookup {
	lookup := contractSchemaLookup{
		tables: make([]schema.TableSchema, 0, len(schemaExport.Tables)),
		byID:   make(map[schema.TableID]int, len(schemaExport.Tables)),
		byName: make(map[string]int, len(schemaExport.Tables)),
	}
	for i, table := range schemaExport.Tables {
		tableID := schema.TableID(i)
		ts := schema.TableSchema{
			ID:         tableID,
			Name:       table.Name,
			Columns:    make([]schema.ColumnSchema, len(table.Columns)),
			Indexes:    make([]schema.IndexSchema, 0, len(table.Indexes)),
			ReadPolicy: copyContractReadPolicy(table.ReadPolicy),
		}
		columnByName := make(map[string]int, len(table.Columns))
		for j, column := range table.Columns {
			kind, _ := valueKindFromExportString(column.Type)
			ts.Columns[j] = schema.ColumnSchema{
				Index:    j,
				Name:     column.Name,
				Type:     kind,
				Nullable: column.Nullable,
			}
			if _, exists := columnByName[column.Name]; !exists {
				columnByName[column.Name] = j
			}
		}
		for j, index := range table.Indexes {
			columns := make([]int, 0, len(index.Columns))
			for _, columnName := range index.Columns {
				if columnIndex, ok := columnByName[columnName]; ok {
					columns = append(columns, columnIndex)
				}
			}
			ts.Indexes = append(ts.Indexes, schema.NewIndexSchema(schema.IndexID(j), index.Name, columns, index.Unique, index.Primary))
		}
		lookup.byID[tableID] = len(lookup.tables)
		if _, exists := lookup.byName[table.Name]; !exists {
			lookup.byName[table.Name] = len(lookup.tables)
		}
		lookup.tables = append(lookup.tables, ts)
	}
	return lookup
}

func (l contractSchemaLookup) Table(id schema.TableID) (*schema.TableSchema, bool) {
	i, ok := l.byID[id]
	if !ok {
		return nil, false
	}
	ts := cloneContractTableSchema(l.tables[i])
	return &ts, true
}

func (l contractSchemaLookup) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	i, ok := l.byName[name]
	if !ok {
		return 0, nil, false
	}
	ts := cloneContractTableSchema(l.tables[i])
	return ts.ID, &ts, true
}

func (l contractSchemaLookup) TableExists(table schema.TableID) bool {
	_, ok := l.byID[table]
	return ok
}

func (l contractSchemaLookup) TableName(table schema.TableID) string {
	i, ok := l.byID[table]
	if !ok {
		return ""
	}
	return l.tables[i].Name
}

func (l contractSchemaLookup) ColumnExists(table schema.TableID, col types.ColID) bool {
	i, ok := l.byID[table]
	return ok && int(col) >= 0 && int(col) < len(l.tables[i].Columns)
}

func (l contractSchemaLookup) ColumnType(table schema.TableID, col types.ColID) schema.ValueKind {
	if !l.ColumnExists(table, col) {
		return 0
	}
	return l.tables[l.byID[table]].Columns[int(col)].Type
}

func (l contractSchemaLookup) HasIndex(table schema.TableID, col types.ColID) bool {
	i, ok := l.byID[table]
	if !ok {
		return false
	}
	for _, index := range l.tables[i].Indexes {
		if len(index.Columns) == 1 && index.Columns[0] == int(col) {
			return true
		}
	}
	return false
}

func (l contractSchemaLookup) ColumnCount(table schema.TableID) int {
	i, ok := l.byID[table]
	if !ok {
		return 0
	}
	return len(l.tables[i].Columns)
}

func cloneContractTableSchema(in schema.TableSchema) schema.TableSchema {
	out := schema.TableSchema{
		ID:         in.ID,
		Name:       in.Name,
		Columns:    append([]schema.ColumnSchema(nil), in.Columns...),
		Indexes:    make([]schema.IndexSchema, len(in.Indexes)),
		ReadPolicy: copyContractReadPolicy(in.ReadPolicy),
	}
	for i, index := range in.Indexes {
		out.Indexes[i] = schema.NewIndexSchema(index.ID, index.Name, append([]int(nil), index.Columns...), index.Unique, index.Primary)
	}
	return out
}

func copyContractReadPolicy(in schema.ReadPolicy) schema.ReadPolicy {
	return schema.ReadPolicy{
		Access:      in.Access,
		Permissions: append([]string(nil), in.Permissions...),
	}
}

func valueKindFromExportString(value string) (schema.ValueKind, bool) {
	switch value {
	case "bool":
		return schema.KindBool, true
	case "int8":
		return schema.KindInt8, true
	case "uint8":
		return schema.KindUint8, true
	case "int16":
		return schema.KindInt16, true
	case "uint16":
		return schema.KindUint16, true
	case "int32":
		return schema.KindInt32, true
	case "uint32":
		return schema.KindUint32, true
	case "int64":
		return schema.KindInt64, true
	case "uint64":
		return schema.KindUint64, true
	case "float32":
		return schema.KindFloat32, true
	case "float64":
		return schema.KindFloat64, true
	case "string":
		return schema.KindString, true
	case "bytes":
		return schema.KindBytes, true
	case "int128":
		return schema.KindInt128, true
	case "uint128":
		return schema.KindUint128, true
	case "int256":
		return schema.KindInt256, true
	case "uint256":
		return schema.KindUint256, true
	case "timestamp":
		return schema.KindTimestamp, true
	case "arrayString":
		return schema.KindArrayString, true
	case "uuid":
		return schema.KindUUID, true
	case "duration":
		return schema.KindDuration, true
	case "json":
		return schema.KindJSON, true
	default:
		return 0, false
	}
}

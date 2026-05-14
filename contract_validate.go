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
	tableIDs := make(map[schema.TableID]struct{}, len(tables))
	validateTableIDs := contractTablesHaveExplicitIDs(tables)
	for _, table := range tables {
		if !validateContractName("schema.tables", table.Name, names, errs) {
			continue
		}
		if validateTableIDs {
			if _, exists := tableIDs[table.ID]; exists {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s id %d is duplicated", table.Name, table.ID))
			}
			tableIDs[table.ID] = struct{}{}
		}
		if err := schema.ValidateReadPolicy(table.ReadPolicy); err != nil {
			*errs = append(*errs, fmt.Errorf("schema.tables.%s.read_policy invalid: %v", table.Name, err))
		}
		columnNames := make(map[string]struct{}, len(table.Columns))
		columnIndexes := make(map[int]struct{}, len(table.Columns))
		columnNameByIndex := make(map[int]string, len(table.Columns))
		validateColumnIndexes := contractColumnsHaveExplicitIndexes(table.Columns)
		for position, column := range table.Columns {
			validateContractName("schema.tables."+table.Name+".columns", column.Name, columnNames, errs)
			resolvedColumnIndex := position
			if validateColumnIndexes {
				if column.Index < 0 || column.Index >= len(table.Columns) {
					*errs = append(*errs, fmt.Errorf("schema.tables.%s.columns.%s index %d is outside column range", table.Name, column.Name, column.Index))
				} else if _, exists := columnIndexes[column.Index]; exists {
					*errs = append(*errs, fmt.Errorf("schema.tables.%s.columns.%s index %d is duplicated", table.Name, column.Name, column.Index))
				} else {
					resolvedColumnIndex = column.Index
				}
				columnIndexes[column.Index] = struct{}{}
			}
			columnNameByIndex[resolvedColumnIndex] = column.Name
			if strings.TrimSpace(column.Type) == "" {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s.columns.%s type must not be empty", table.Name, column.Name))
			} else if _, ok := valueKindFromExportString(column.Type); !ok {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s.columns.%s type %q is invalid", table.Name, column.Name, column.Type))
			}
		}
		indexNames := make(map[string]struct{}, len(table.Indexes))
		indexIDs := make(map[schema.IndexID]struct{}, len(table.Indexes))
		validateIndexIDs := contractIndexesHaveExplicitIDs(table.Indexes)
		for _, index := range table.Indexes {
			if !validateContractName("schema.tables."+table.Name+".indexes", index.Name, indexNames, errs) {
				continue
			}
			if validateIndexIDs {
				if _, exists := indexIDs[index.ID]; exists {
					*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s id %d is duplicated", table.Name, index.Name, index.ID))
				}
				indexIDs[index.ID] = struct{}{}
			}
			if len(index.Columns) == 0 {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s columns must not be empty", table.Name, index.Name))
			}
			if len(index.ColumnOrdinals) != 0 && len(index.ColumnOrdinals) != len(index.Columns) {
				*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s column_ordinals length = %d, want %d", table.Name, index.Name, len(index.ColumnOrdinals), len(index.Columns)))
			}
			indexColumns := make(map[string]struct{}, len(index.Columns))
			for i, column := range index.Columns {
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
				if len(index.ColumnOrdinals) != 0 {
					ordinal := index.ColumnOrdinals[i]
					if ordinal < 0 || ordinal >= len(table.Columns) {
						*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s column_ordinals[%d] = %d is outside column range", table.Name, index.Name, i, ordinal))
					} else if columnNameByIndex[ordinal] != column {
						*errs = append(*errs, fmt.Errorf("schema.tables.%s.indexes.%s column_ordinals[%d] = %d references %q, want %q", table.Name, index.Name, i, ordinal, columnNameByIndex[ordinal], column))
					}
				}
			}
		}
	}
	return names
}

func validateContractReducers(reducers []schema.ReducerExport, errs *[]error) map[string]struct{} {
	names := make(map[string]struct{}, len(reducers))
	for _, reducer := range reducers {
		if validateContractName("schema.reducers", reducer.Name, names, errs) {
			validateProductSchema("schema.reducers."+reducer.Name+".args", reducer.Args, errs)
			validateProductSchema("schema.reducers."+reducer.Name+".result", reducer.Result, errs)
		}
	}
	return names
}

func validateProductSchema(path string, product *ProductSchema, errs *[]error) {
	if product == nil {
		return
	}
	columnNames := make(map[string]struct{}, len(product.Columns))
	for _, column := range product.Columns {
		validateContractName(path+".columns", column.Name, columnNames, errs)
		if strings.TrimSpace(column.Type) == "" {
			*errs = append(*errs, fmt.Errorf("%s.columns.%s type must not be empty", path, column.Name))
		} else if _, ok := valueKindFromExportString(column.Type); !ok {
			*errs = append(*errs, fmt.Errorf("%s.columns.%s type %q is invalid", path, column.Name, column.Type))
		}
	}
}

func validateParameterProductSchema(path string, product *ProductSchema, errs *[]error) {
	validateProductSchema(path, product, errs)
	if product == nil {
		return
	}
	for _, column := range product.Columns {
		if column.Name == "sender" {
			*errs = append(*errs, fmt.Errorf("%s.columns.%s name %q is reserved", path, column.Name, column.Name))
		}
		if column.Nullable {
			*errs = append(*errs, fmt.Errorf("%s.columns.%s nullable parameters are not supported", path, column.Name))
		}
	}
}

func validateContractReadDeclarations(queries []QueryDescription, views []ViewDescription, errs *[]error) (map[string]struct{}, map[string]struct{}) {
	queriesByName := make(map[string]struct{}, len(queries))
	viewsByName := make(map[string]struct{}, len(views))
	readNamespace := make(map[string]struct{}, len(queries)+len(views))
	for _, query := range queries {
		if validateContractName("queries", query.Name, queriesByName, errs) {
			validateContractName("read declarations", query.Name, readNamespace, errs)
		}
		validateParameterProductSchema("queries."+query.Name+".parameters", query.Parameters, errs)
	}
	for _, view := range views {
		if validateContractName("views", view.Name, viewsByName, errs) {
			validateContractName("read declarations", view.Name, readNamespace, errs)
		}
		validateParameterProductSchema("views."+view.Name+".parameters", view.Parameters, errs)
	}
	return queriesByName, viewsByName
}

func validateContractDeclarationSQL(schemaExport schema.SchemaExport, queries []QueryDescription, views []ViewDescription, errs *[]error) {
	lookup := newContractSchemaLookup(schemaExport)
	for _, query := range queries {
		if strings.TrimSpace(query.SQL) == "" {
			validateDeclaredReadMetadataAbsent("queries."+query.Name, query.RowSchema, query.ResultShape, errs)
			continue
		}
		compiled, err := compileDeclaredReadSQLTemplate(query.SQL, lookup, declaredReadSQLValidation, query.Parameters)
		if err != nil {
			*errs = append(*errs, fmt.Errorf("queries.%s.sql invalid: %v", query.Name, err))
			continue
		}
		validateDeclaredReadMetadata("queries."+query.Name, compiled, lookup, query.RowSchema, query.ResultShape, errs)
	}
	for _, view := range views {
		if strings.TrimSpace(view.SQL) == "" {
			validateDeclaredReadMetadataAbsent("views."+view.Name, view.RowSchema, view.ResultShape, errs)
			continue
		}
		compiled, err := compileDeclaredReadSQLTemplate(view.SQL, lookup, declaredReadSQLValidation, view.Parameters)
		if err != nil {
			*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, err))
			continue
		}
		validateDeclaredReadMetadata("views."+view.Name, compiled, lookup, view.RowSchema, view.ResultShape, errs)
		if aggregate := compiled.SubscriptionAggregate(); aggregate != nil {
			appendViewSQLValidationError(errs, view.Name, subscription.ValidateAggregate(compiled.Predicate(), aggregate, lookup))
			if compiled.HasOrderBy() {
				*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, fmt.Errorf("%w: live ORDER BY views do not support aggregate views", subscription.ErrInvalidPredicate)))
			}
			if compiled.HasLimit() {
				*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, fmt.Errorf("%w: live LIMIT views do not support aggregate views", subscription.ErrInvalidPredicate)))
			}
			if compiled.HasOffset() {
				*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", view.Name, fmt.Errorf("%w: live OFFSET views do not support aggregate views", subscription.ErrInvalidPredicate)))
			}
			appendViewSQLValidationError(errs, view.Name, subscription.ValidateOrderBy(compiled.Predicate(), compiled.SubscriptionOrderBy(), aggregate, lookup))
			appendViewSQLValidationError(errs, view.Name, subscription.ValidateLimit(compiled.Predicate(), compiled.SubscriptionLimit(), aggregate, lookup))
			appendViewSQLValidationError(errs, view.Name, subscription.ValidateOffset(compiled.Predicate(), compiled.SubscriptionOffset(), aggregate, lookup))
			continue
		}
		appendViewSQLValidationError(errs, view.Name, subscription.ValidateProjection(compiled.Predicate(), compiled.SubscriptionProjection(), lookup))
		appendViewSQLValidationError(errs, view.Name, subscription.ValidateOrderBy(compiled.Predicate(), compiled.SubscriptionOrderBy(), compiled.SubscriptionAggregate(), lookup))
		appendViewSQLValidationError(errs, view.Name, subscription.ValidateLimit(compiled.Predicate(), compiled.SubscriptionLimit(), compiled.SubscriptionAggregate(), lookup))
		appendViewSQLValidationError(errs, view.Name, subscription.ValidateOffset(compiled.Predicate(), compiled.SubscriptionOffset(), compiled.SubscriptionAggregate(), lookup))
	}
}

func appendViewSQLValidationError(errs *[]error, viewName string, err error) {
	if err != nil {
		*errs = append(*errs, fmt.Errorf("views.%s.sql invalid: %v", viewName, err))
	}
}

func validateDeclaredReadMetadataAbsent(path string, rowSchema *ProductSchema, resultShape *ReadResultShape, errs *[]error) {
	if rowSchema != nil {
		*errs = append(*errs, fmt.Errorf("%s.row_schema must be omitted for metadata-only declarations", path))
	}
	if resultShape != nil {
		*errs = append(*errs, fmt.Errorf("%s.result_shape must be omitted for metadata-only declarations", path))
	}
}

func validateDeclaredReadMetadata(path string, compiled declaredReadValidationMetadataSource, lookup contractSchemaLookup, rowSchema *ProductSchema, resultShape *ReadResultShape, errs *[]error) {
	if rowSchema == nil && resultShape == nil {
		return
	}
	if rowSchema == nil {
		*errs = append(*errs, fmt.Errorf("%s.row_schema must be present when result_shape is present", path))
		return
	}
	if resultShape == nil {
		*errs = append(*errs, fmt.Errorf("%s.result_shape must be present when row_schema is present", path))
		return
	}
	validateProductSchema(path+".row_schema", rowSchema, errs)
	expectedRow := productSchemaForColumnSchemas(compiled.ResultColumns(lookup))
	if !productSchemasEqual(rowSchema, expectedRow) {
		*errs = append(*errs, fmt.Errorf("%s.row_schema does not match compiled SQL result columns", path))
	}
	expectedShape := readResultShapeForCompiled(compiled)
	if *resultShape != *expectedShape {
		*errs = append(*errs, fmt.Errorf("%s.result_shape = %#v, want %#v", path, *resultShape, *expectedShape))
	}
}

type declaredReadValidationMetadataSource interface {
	declaredReadResultMetadataSource
	ResultColumns(protocol.SchemaLookup) []schema.ColumnSchema
}

func productSchemasEqual(a, b *ProductSchema) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.Columns) != len(b.Columns) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return false
		}
	}
	return true
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
	return validateContractKeyedName(path, name, name, seen, errs)
}

func validateContractSurfaceName(path, surface, name string, seen map[string]struct{}, errs *[]error) bool {
	return validateContractKeyedName(path, name, surface+"\x00"+name, seen, errs)
}

func validateContractKeyedName(path, name, key string, seen map[string]struct{}, errs *[]error) bool {
	if strings.TrimSpace(name) == "" {
		*errs = append(*errs, fmt.Errorf("%s name must not be empty", path))
		return false
	}
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
	useTableIDs := contractTablesHaveExplicitIDs(schemaExport.Tables)
	for i, table := range schemaExport.Tables {
		tableID := contractTableID(table, i, useTableIDs)
		useColumnIndexes := contractColumnsHaveExplicitIndexes(table.Columns)
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
			columnIndex := contractColumnIndex(column, j, len(table.Columns), useColumnIndexes)
			ts.Columns[columnIndex] = schema.ColumnSchema{
				Index:         columnIndex,
				Name:          column.Name,
				Type:          kind,
				Nullable:      column.Nullable,
				AutoIncrement: column.AutoIncrement,
			}
			if _, exists := columnByName[column.Name]; !exists {
				columnByName[column.Name] = columnIndex
			}
		}
		useIndexIDs := contractIndexesHaveExplicitIDs(table.Indexes)
		for j, index := range table.Indexes {
			columns := contractIndexColumnOrdinals(index, columnByName, len(table.Columns))
			indexID := schema.IndexID(j)
			if useIndexIDs {
				indexID = index.ID
			}
			ts.Indexes = append(ts.Indexes, schema.NewIndexSchema(indexID, index.Name, columns, index.Unique, index.Primary))
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

func contractTablesHaveExplicitIDs(tables []schema.TableExport) bool {
	for _, table := range tables {
		if table.ID != 0 {
			return true
		}
	}
	return len(tables) <= 1
}

func contractColumnsHaveExplicitIndexes(columns []schema.ColumnExport) bool {
	for _, column := range columns {
		if column.Index != 0 {
			return true
		}
	}
	return len(columns) <= 1
}

func contractIndexesHaveExplicitIDs(indexes []schema.IndexExport) bool {
	for _, index := range indexes {
		if index.ID != 0 {
			return true
		}
	}
	return len(indexes) <= 1
}

func contractTableID(table schema.TableExport, position int, explicit bool) schema.TableID {
	if explicit {
		return table.ID
	}
	return schema.TableID(position)
}

func contractColumnIndex(column schema.ColumnExport, position, columnCount int, explicit bool) int {
	if explicit && column.Index >= 0 && column.Index < columnCount {
		return column.Index
	}
	return position
}

func contractIndexColumnOrdinals(index schema.IndexExport, columnByName map[string]int, columnCount int) []int {
	if len(index.ColumnOrdinals) == len(index.Columns) {
		out := make([]int, 0, len(index.ColumnOrdinals))
		for _, ordinal := range index.ColumnOrdinals {
			if ordinal >= 0 && ordinal < columnCount {
				out = append(out, ordinal)
			}
		}
		return out
	}
	out := make([]int, 0, len(index.Columns))
	for _, columnName := range index.Columns {
		if columnIndex, ok := columnByName[columnName]; ok {
			out = append(out, columnIndex)
		}
	}
	return out
}

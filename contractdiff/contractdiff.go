package contractdiff

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

var ErrInvalidContractJSON = errors.New("invalid module contract JSON")

type ChangeKind string

const (
	ChangeKindAdditive ChangeKind = "additive"
	ChangeKindBreaking ChangeKind = "breaking"
	ChangeKindMetadata ChangeKind = "metadata"
)

type Surface string

const (
	SurfaceContract          Surface = "contract"
	SurfaceModule            Surface = "module"
	SurfaceSchema            Surface = "schema"
	SurfaceTable             Surface = "table"
	SurfaceTableReadPolicy   Surface = "table_read_policy"
	SurfaceColumn            Surface = "column"
	SurfaceIndex             Surface = "index"
	SurfaceReducer           Surface = "reducer"
	SurfaceQuery             Surface = "query"
	SurfaceView              Surface = "view"
	SurfaceVisibilityFilter  Surface = "visibility_filter"
	SurfacePermission        Surface = "permission"
	SurfaceReadModel         Surface = "read_model"
	SurfaceMigrationMetadata Surface = "migration_metadata"
)

type Change struct {
	Kind    ChangeKind
	Surface Surface
	Name    string
	Detail  string
}

type Report struct {
	Changes []Change
}

func CompareJSON(oldData, currentData []byte) (Report, error) {
	old, err := decodeContractJSON("previous", oldData)
	if err != nil {
		return Report{}, err
	}
	current, err := decodeContractJSON("current", currentData)
	if err != nil {
		return Report{}, err
	}
	return Compare(old, current), nil
}

func decodeContractJSON(label string, data []byte) (shunter.ModuleContract, error) {
	var contract shunter.ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return shunter.ModuleContract{}, fmt.Errorf("%w: %s contract: %v", ErrInvalidContractJSON, label, err)
	}
	if err := shunter.ValidateModuleContract(contract); err != nil {
		return shunter.ModuleContract{}, fmt.Errorf("%w: %s contract: %v", ErrInvalidContractJSON, label, err)
	}
	return contract, nil
}

func Compare(old, current shunter.ModuleContract) Report {
	var out Report
	compareVersions(&out, old, current)
	compareModuleMetadata(&out, old.Module, current.Module)
	compareTables(&out, old.Schema.Tables, current.Schema.Tables)
	compareReducers(&out, old.Schema.Reducers, current.Schema.Reducers)
	compareNamedQueries(&out, SurfaceQuery, old.Queries, current.Queries)
	compareNamedViews(&out, old.Views, current.Views)
	compareVisibilityFilters(&out, old.VisibilityFilters, current.VisibilityFilters)
	comparePermissions(&out, old.Permissions, current.Permissions)
	compareReadModels(&out, old.ReadModel.Declarations, current.ReadModel.Declarations)
	compareMigrations(&out, old.Migrations, current.Migrations)
	sortChanges(out.Changes)
	return out
}

func (r Report) Text() string {
	if len(r.Changes) == 0 {
		return "No contract changes.\n"
	}
	var b strings.Builder
	for _, change := range r.Changes {
		fmt.Fprintf(&b, "%s %s %s: %s\n", change.Kind, change.Surface, change.Name, change.Detail)
	}
	return b.String()
}

func compareVersions(out *Report, old, current shunter.ModuleContract) {
	if old.ContractVersion != current.ContractVersion {
		out.add(ChangeKindMetadata, SurfaceContract, "contract", fmt.Sprintf("contract version changed from %d to %d", old.ContractVersion, current.ContractVersion))
	}
	if old.Module.Name != current.Module.Name {
		out.add(ChangeKindMetadata, SurfaceModule, nonEmptyName(current.Module.Name, old.Module.Name), fmt.Sprintf("module name changed from %q to %q", old.Module.Name, current.Module.Name))
	}
	if old.Module.Version != current.Module.Version {
		out.add(ChangeKindMetadata, SurfaceModule, nonEmptyName(current.Module.Name, old.Module.Name), fmt.Sprintf("module version changed from %q to %q", old.Module.Version, current.Module.Version))
	}
	if old.Schema.Version != current.Schema.Version {
		out.add(ChangeKindMetadata, SurfaceSchema, "schema", fmt.Sprintf("schema version changed from %d to %d", old.Schema.Version, current.Schema.Version))
	}
}

func compareModuleMetadata(out *Report, old, current shunter.ModuleContractIdentity) {
	moduleName := nonEmptyName(current.Name, old.Name)
	for key, currentValue := range current.Metadata {
		oldValue, ok := old.Metadata[key]
		name := moduleName + ".metadata." + key
		if !ok {
			out.add(ChangeKindMetadata, SurfaceModule, name, fmt.Sprintf("module metadata %q added", key))
			continue
		}
		if oldValue != currentValue {
			out.add(ChangeKindMetadata, SurfaceModule, name, fmt.Sprintf("module metadata %q changed", key))
		}
	}
	for key := range old.Metadata {
		if _, ok := current.Metadata[key]; !ok {
			out.add(ChangeKindMetadata, SurfaceModule, moduleName+".metadata."+key, fmt.Sprintf("module metadata %q removed", key))
		}
	}
}

func compareTables(out *Report, oldTables, currentTables []schema.TableExport) {
	oldByName := tableMap(oldTables)
	currentByName := tableMap(currentTables)

	for name, current := range currentByName {
		old, ok := oldByName[name]
		if !ok {
			out.add(ChangeKindAdditive, SurfaceTable, name, "table added")
			continue
		}
		compareTableReadPolicy(out, old.Name, old.ReadPolicy, current.ReadPolicy)
		compareColumns(out, old.Name, old.Columns, current.Columns)
		compareIndexes(out, old.Name, old.Indexes, current.Indexes)
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindBreaking, SurfaceTable, name, "table removed")
		}
	}
}

func compareTableReadPolicy(out *Report, tableName string, oldPolicy, currentPolicy schema.ReadPolicy) {
	if readPoliciesSemanticallyEqual(oldPolicy, currentPolicy) {
		return
	}
	oldSignature := readPolicySignature(oldPolicy)
	currentSignature := readPolicySignature(currentPolicy)
	out.add(tableReadPolicyChangeKind(oldPolicy, currentPolicy), SurfaceTableReadPolicy, tableName, fmt.Sprintf("read policy changed from %s to %s", oldSignature, currentSignature))
}

func tableReadPolicyChangeKind(oldPolicy, currentPolicy schema.ReadPolicy) ChangeKind {
	if oldPolicy.Access == schema.TableAccessPermissioned && currentPolicy.Access == schema.TableAccessPermissioned {
		if stringSliceSubset(currentPolicy.Permissions, oldPolicy.Permissions) {
			return ChangeKindAdditive
		}
		return ChangeKindBreaking
	}
	oldRank := tableAccessRank(oldPolicy)
	currentRank := tableAccessRank(currentPolicy)
	if currentRank > oldRank {
		return ChangeKindAdditive
	}
	return ChangeKindBreaking
}

func tableAccessRank(policy schema.ReadPolicy) int {
	switch policy.Access {
	case schema.TableAccessPrivate:
		return 0
	case schema.TableAccessPermissioned:
		return 1
	case schema.TableAccessPublic:
		return 2
	default:
		return 0
	}
}

func compareIndexes(out *Report, tableName string, oldIndexes, currentIndexes []schema.IndexExport) {
	oldByName := indexMap(oldIndexes)
	currentByName := indexMap(currentIndexes)
	for name, current := range currentByName {
		old, ok := oldByName[name]
		indexName := tableName + "." + name
		if !ok {
			out.add(ChangeKindAdditive, SurfaceIndex, indexName, "index added")
			continue
		}
		if !indexesEqual(old, current) {
			out.add(ChangeKindBreaking, SurfaceIndex, indexName, fmt.Sprintf("index changed from %s to %s", indexSignature(old), indexSignature(current)))
		}
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindBreaking, SurfaceIndex, tableName+"."+name, "index removed")
		}
	}
}

func compareColumns(out *Report, tableName string, oldColumns, currentColumns []schema.ColumnExport) {
	oldByName := columnMap(oldColumns)
	currentByName := columnMap(currentColumns)
	for name, current := range currentByName {
		old, ok := oldByName[name]
		columnName := tableName + "." + name
		if !ok {
			out.add(ChangeKindAdditive, SurfaceColumn, columnName, fmt.Sprintf("column added with type %s", current.Type))
			continue
		}
		if old.Type != current.Type {
			out.add(ChangeKindBreaking, SurfaceColumn, columnName, fmt.Sprintf("column type changed from %s to %s", old.Type, current.Type))
		}
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindBreaking, SurfaceColumn, tableName+"."+name, "column removed")
		}
	}
}

func compareReducers(out *Report, oldReducers, currentReducers []schema.ReducerExport) {
	oldByName := reducerMap(oldReducers)
	currentByName := reducerMap(currentReducers)
	for name, current := range currentByName {
		old, ok := oldByName[name]
		if !ok {
			out.add(ChangeKindAdditive, SurfaceReducer, name, "reducer added")
			continue
		}
		if old.Lifecycle != current.Lifecycle {
			out.add(ChangeKindBreaking, SurfaceReducer, name, fmt.Sprintf("lifecycle flag changed from %t to %t", old.Lifecycle, current.Lifecycle))
		}
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindBreaking, SurfaceReducer, name, "reducer removed")
		}
	}
}

func compareNamedQueries(out *Report, surface Surface, oldQueries, currentQueries []shunter.QueryDescription) {
	oldByName := queryMap(oldQueries)
	currentByName := queryMap(currentQueries)
	for name, current := range currentByName {
		old, ok := oldByName[name]
		if !ok {
			out.add(ChangeKindAdditive, surface, name, "query added")
			continue
		}
		compareNamedReadSQL(out, ChangeKindAdditive, ChangeKindBreaking, surface, name, "query", old.SQL, current.SQL)
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindBreaking, surface, name, "query removed")
		}
	}
}

func compareNamedViews(out *Report, oldViews, currentViews []shunter.ViewDescription) {
	oldByName := viewMap(oldViews)
	currentByName := viewMap(currentViews)
	for name, current := range currentByName {
		old, ok := oldByName[name]
		if !ok {
			out.add(ChangeKindAdditive, SurfaceView, name, "view added")
			continue
		}
		compareNamedReadSQL(out, ChangeKindAdditive, ChangeKindBreaking, SurfaceView, name, "view", old.SQL, current.SQL)
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindBreaking, SurfaceView, name, "view removed")
		}
	}
}

func compareNamedReadSQL(out *Report, addedKind, changedKind ChangeKind, surface Surface, name, label, oldSQL, currentSQL string) {
	oldHasSQL := strings.TrimSpace(oldSQL) != ""
	currentHasSQL := strings.TrimSpace(currentSQL) != ""
	switch {
	case !oldHasSQL && currentHasSQL:
		out.add(addedKind, surface, name, label+" SQL added")
	case oldHasSQL && !currentHasSQL:
		out.add(changedKind, surface, name, label+" SQL removed")
	case oldHasSQL && currentHasSQL && oldSQL != currentSQL:
		out.add(changedKind, surface, name, label+" SQL changed")
	}
}

func compareVisibilityFilters(out *Report, oldFilters, currentFilters []shunter.VisibilityFilterDescription) {
	oldByName := visibilityFilterMap(oldFilters)
	currentByName := visibilityFilterMap(currentFilters)
	for name, current := range currentByName {
		old, ok := oldByName[name]
		if !ok {
			out.add(ChangeKindBreaking, SurfaceVisibilityFilter, name, "visibility filter added")
			continue
		}
		if !visibilityFiltersEqual(old, current) {
			out.add(ChangeKindBreaking, SurfaceVisibilityFilter, name, "visibility filter changed")
		}
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindBreaking, SurfaceVisibilityFilter, name, "visibility filter removed")
		}
	}
}

func comparePermissions(out *Report, oldPermissions, currentPermissions shunter.PermissionContract) {
	comparePermissionCategory(out, "reducer", oldPermissions.Reducers, currentPermissions.Reducers)
	comparePermissionCategory(out, "query", oldPermissions.Queries, currentPermissions.Queries)
	comparePermissionCategory(out, "view", oldPermissions.Views, currentPermissions.Views)
}

func comparePermissionCategory(out *Report, category string, oldDeclarations, currentDeclarations []shunter.PermissionContractDeclaration) {
	oldByName := permissionMap(oldDeclarations)
	currentByName := permissionMap(currentDeclarations)
	for name, current := range currentByName {
		old, ok := oldByName[name]
		fullName := category + "." + name
		if !ok {
			out.add(permissionChangeKind(category, nil, current.Required), SurfacePermission, fullName, "permission requirements added")
			continue
		}
		if !unorderedStringSlicesEqual(old.Required, current.Required) {
			out.add(permissionChangeKind(category, old.Required, current.Required), SurfacePermission, fullName, "permission requirements changed")
		}
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(permissionChangeKind(category, oldByName[name].Required, nil), SurfacePermission, category+"."+name, "permission requirements removed")
		}
	}
}

func permissionChangeKind(category string, oldRequired, currentRequired []string) ChangeKind {
	if category == "reducer" {
		return ChangeKindMetadata
	}
	if stringSliceSubset(currentRequired, oldRequired) {
		return ChangeKindAdditive
	}
	return ChangeKindBreaking
}

func compareReadModels(out *Report, oldDeclarations, currentDeclarations []shunter.ReadModelContractDeclaration) {
	oldByName := readModelMap(oldDeclarations)
	currentByName := readModelMap(currentDeclarations)
	for name, current := range currentByName {
		old, ok := oldByName[name]
		if !ok {
			out.add(ChangeKindMetadata, SurfaceReadModel, name, "read model metadata added")
			continue
		}
		if !orderedStringSlicesEqual(old.Tables, current.Tables) || !orderedStringSlicesEqual(old.Tags, current.Tags) {
			out.add(ChangeKindMetadata, SurfaceReadModel, name, "read model metadata changed")
		}
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindMetadata, SurfaceReadModel, name, "read model metadata removed")
		}
	}
}

func compareMigrations(out *Report, oldMigrations, currentMigrations shunter.MigrationContract) {
	if !migrationMetadataEqual(oldMigrations.Module, currentMigrations.Module) {
		out.add(ChangeKindMetadata, SurfaceMigrationMetadata, "module", "module migration metadata changed")
	}

	oldByName := migrationDeclarationMap(oldMigrations.Declarations)
	currentByName := migrationDeclarationMap(currentMigrations.Declarations)
	for key, current := range currentByName {
		old, ok := oldByName[key]
		name := migrationDeclarationDisplayName(key)
		if !ok {
			out.add(ChangeKindMetadata, SurfaceMigrationMetadata, name, "migration metadata added")
			continue
		}
		if !migrationMetadataEqual(old.Metadata, current.Metadata) {
			out.add(ChangeKindMetadata, SurfaceMigrationMetadata, name, "migration metadata changed")
		}
	}
	for key := range oldByName {
		if _, ok := currentByName[key]; !ok {
			out.add(ChangeKindMetadata, SurfaceMigrationMetadata, migrationDeclarationDisplayName(key), "migration metadata removed")
		}
	}
}

func (r *Report) add(kind ChangeKind, surface Surface, name, detail string) {
	r.Changes = append(r.Changes, Change{Kind: kind, Surface: surface, Name: name, Detail: detail})
}

func sortChanges(changes []Change) {
	sort.SliceStable(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Detail < b.Detail
	})
}

func tableMap(tables []schema.TableExport) map[string]schema.TableExport {
	return mapBy(tables, func(table schema.TableExport) string { return table.Name })
}

func columnMap(columns []schema.ColumnExport) map[string]schema.ColumnExport {
	return mapBy(columns, func(column schema.ColumnExport) string { return column.Name })
}

func reducerMap(reducers []schema.ReducerExport) map[string]schema.ReducerExport {
	return mapBy(reducers, func(reducer schema.ReducerExport) string { return reducer.Name })
}

func indexMap(indexes []schema.IndexExport) map[string]schema.IndexExport {
	return mapBy(indexes, func(index schema.IndexExport) string { return index.Name })
}

func queryMap(queries []shunter.QueryDescription) map[string]shunter.QueryDescription {
	return mapBy(queries, func(query shunter.QueryDescription) string { return query.Name })
}

func viewMap(views []shunter.ViewDescription) map[string]shunter.ViewDescription {
	return mapBy(views, func(view shunter.ViewDescription) string { return view.Name })
}

func visibilityFilterMap(filters []shunter.VisibilityFilterDescription) map[string]shunter.VisibilityFilterDescription {
	return mapBy(filters, func(filter shunter.VisibilityFilterDescription) string { return filter.Name })
}

func permissionMap(declarations []shunter.PermissionContractDeclaration) map[string]shunter.PermissionContractDeclaration {
	return mapBy(declarations, func(declaration shunter.PermissionContractDeclaration) string { return declaration.Name })
}

func readModelMap(declarations []shunter.ReadModelContractDeclaration) map[string]shunter.ReadModelContractDeclaration {
	return mapBy(declarations, func(declaration shunter.ReadModelContractDeclaration) string {
		return declaration.Surface + "." + declaration.Name
	})
}

func mapBy[T any](values []T, key func(T) string) map[string]T {
	out := make(map[string]T, len(values))
	for _, value := range values {
		out[key(value)] = value
	}
	return out
}

func stringSliceSubset(values, allowed []string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := allowedSet[value]; !ok {
			return false
		}
	}
	return true
}

func orderedStringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func unorderedStringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		if counts[value] == 0 {
			return false
		}
		counts[value]--
		if counts[value] == 0 {
			delete(counts, value)
		}
	}
	return len(counts) == 0
}

func readPoliciesSemanticallyEqual(oldPolicy, currentPolicy schema.ReadPolicy) bool {
	if oldPolicy.Access != currentPolicy.Access {
		return false
	}
	if oldPolicy.Access != schema.TableAccessPermissioned {
		return true
	}
	return unorderedStringSlicesEqual(oldPolicy.Permissions, currentPolicy.Permissions)
}

func indexesEqual(old, current schema.IndexExport) bool {
	return old.Unique == current.Unique &&
		old.Primary == current.Primary &&
		orderedStringSlicesEqual(old.Columns, current.Columns)
}

func indexSignature(index schema.IndexExport) string {
	return fmt.Sprintf("columns=%q unique=%t primary=%t", index.Columns, index.Unique, index.Primary)
}

func readPolicySignature(policy schema.ReadPolicy) string {
	if policy.Access != schema.TableAccessPermissioned {
		return policy.Access.String()
	}
	return fmt.Sprintf("%s(%s)", policy.Access, strings.Join(policy.Permissions, ","))
}

func visibilityFiltersEqual(old, current shunter.VisibilityFilterDescription) bool {
	return old.SQL == current.SQL &&
		old.ReturnTable == current.ReturnTable &&
		old.ReturnTableID == current.ReturnTableID &&
		old.UsesCallerIdentity == current.UsesCallerIdentity
}

func migrationDeclarationMap(declarations []shunter.MigrationContractDeclaration) map[string]shunter.MigrationContractDeclaration {
	out := make(map[string]shunter.MigrationContractDeclaration, len(declarations))
	for _, declaration := range declarations {
		out[migrationDeclarationKey(declaration.Surface, declaration.Name)] = declaration
	}
	return out
}

func migrationDeclarationKey(surface, name string) string {
	return surface + "\x00" + name
}

func migrationDeclarationDisplayName(key string) string {
	surface, name, ok := strings.Cut(key, "\x00")
	if !ok {
		return key
	}
	return surface + "." + name
}

func migrationMetadataEqual(old, current shunter.MigrationMetadata) bool {
	return old.ModuleVersion == current.ModuleVersion &&
		old.SchemaVersion == current.SchemaVersion &&
		old.ContractVersion == current.ContractVersion &&
		old.PreviousVersion == current.PreviousVersion &&
		old.Compatibility == current.Compatibility &&
		old.Notes == current.Notes &&
		orderedMigrationClassificationsEqual(old.Classifications, current.Classifications)
}

func orderedMigrationClassificationsEqual(a, b []shunter.MigrationClassification) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func nonEmptyName(first, fallback string) string {
	if first != "" {
		return first
	}
	if fallback != "" {
		return fallback
	}
	return "module"
}

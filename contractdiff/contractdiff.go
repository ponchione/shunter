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
	SurfaceContract   Surface = "contract"
	SurfaceModule     Surface = "module"
	SurfaceSchema     Surface = "schema"
	SurfaceTable      Surface = "table"
	SurfaceColumn     Surface = "column"
	SurfaceReducer    Surface = "reducer"
	SurfaceQuery      Surface = "query"
	SurfaceView       Surface = "view"
	SurfacePermission Surface = "permission"
	SurfaceReadModel  Surface = "read_model"
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
	var old shunter.ModuleContract
	if err := json.Unmarshal(oldData, &old); err != nil {
		return Report{}, fmt.Errorf("%w: previous contract: %v", ErrInvalidContractJSON, err)
	}
	var current shunter.ModuleContract
	if err := json.Unmarshal(currentData, &current); err != nil {
		return Report{}, fmt.Errorf("%w: current contract: %v", ErrInvalidContractJSON, err)
	}
	return Compare(old, current), nil
}

func Compare(old, current shunter.ModuleContract) Report {
	var out Report
	compareVersions(&out, old, current)
	compareTables(&out, old.Schema.Tables, current.Schema.Tables)
	compareReducers(&out, old.Schema.Reducers, current.Schema.Reducers)
	compareNamedQueries(&out, SurfaceQuery, old.Queries, current.Queries)
	compareNamedViews(&out, old.Views, current.Views)
	comparePermissions(&out, old.Permissions, current.Permissions)
	compareReadModels(&out, old.ReadModel.Declarations, current.ReadModel.Declarations)
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

func compareTables(out *Report, oldTables, currentTables []schema.TableExport) {
	oldByName := tableMap(oldTables)
	currentByName := tableMap(currentTables)

	for name, current := range currentByName {
		old, ok := oldByName[name]
		if !ok {
			out.add(ChangeKindAdditive, SurfaceTable, name, "table added")
			continue
		}
		compareColumns(out, old.Name, old.Columns, current.Columns)
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindBreaking, SurfaceTable, name, "table removed")
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
	oldNames := querySet(oldQueries)
	currentNames := querySet(currentQueries)
	for name := range currentNames {
		if _, ok := oldNames[name]; !ok {
			out.add(ChangeKindAdditive, surface, name, "query added")
		}
	}
	for name := range oldNames {
		if _, ok := currentNames[name]; !ok {
			out.add(ChangeKindBreaking, surface, name, "query removed")
		}
	}
}

func compareNamedViews(out *Report, oldViews, currentViews []shunter.ViewDescription) {
	oldNames := viewSet(oldViews)
	currentNames := viewSet(currentViews)
	for name := range currentNames {
		if _, ok := oldNames[name]; !ok {
			out.add(ChangeKindAdditive, SurfaceView, name, "view added")
		}
	}
	for name := range oldNames {
		if _, ok := currentNames[name]; !ok {
			out.add(ChangeKindBreaking, SurfaceView, name, "view removed")
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
			out.add(ChangeKindMetadata, SurfacePermission, fullName, "permission metadata added")
			continue
		}
		if stringSliceSignature(old.Required) != stringSliceSignature(current.Required) {
			out.add(ChangeKindMetadata, SurfacePermission, fullName, "permission requirements changed")
		}
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindMetadata, SurfacePermission, category+"."+name, "permission metadata removed")
		}
	}
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
		if stringSliceSignature(old.Tables) != stringSliceSignature(current.Tables) || stringSliceSignature(old.Tags) != stringSliceSignature(current.Tags) {
			out.add(ChangeKindMetadata, SurfaceReadModel, name, "read model metadata changed")
		}
	}
	for name := range oldByName {
		if _, ok := currentByName[name]; !ok {
			out.add(ChangeKindMetadata, SurfaceReadModel, name, "read model metadata removed")
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
	out := make(map[string]schema.TableExport, len(tables))
	for _, table := range tables {
		out[table.Name] = table
	}
	return out
}

func columnMap(columns []schema.ColumnExport) map[string]schema.ColumnExport {
	out := make(map[string]schema.ColumnExport, len(columns))
	for _, column := range columns {
		out[column.Name] = column
	}
	return out
}

func reducerMap(reducers []schema.ReducerExport) map[string]schema.ReducerExport {
	out := make(map[string]schema.ReducerExport, len(reducers))
	for _, reducer := range reducers {
		out[reducer.Name] = reducer
	}
	return out
}

func querySet(queries []shunter.QueryDescription) map[string]struct{} {
	out := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		out[query.Name] = struct{}{}
	}
	return out
}

func viewSet(views []shunter.ViewDescription) map[string]struct{} {
	out := make(map[string]struct{}, len(views))
	for _, view := range views {
		out[view.Name] = struct{}{}
	}
	return out
}

func permissionMap(declarations []shunter.PermissionContractDeclaration) map[string]shunter.PermissionContractDeclaration {
	out := make(map[string]shunter.PermissionContractDeclaration, len(declarations))
	for _, declaration := range declarations {
		out[declaration.Name] = declaration
	}
	return out
}

func readModelMap(declarations []shunter.ReadModelContractDeclaration) map[string]shunter.ReadModelContractDeclaration {
	out := make(map[string]shunter.ReadModelContractDeclaration, len(declarations))
	for _, declaration := range declarations {
		out[declaration.Surface+"."+declaration.Name] = declaration
	}
	return out
}

func stringSliceSignature(values []string) string {
	return strings.Join(values, "\x00")
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

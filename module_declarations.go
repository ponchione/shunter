package shunter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
)

var (
	ErrEmptyDeclarationName     = errors.New("declaration name must not be empty")
	ErrDuplicateDeclarationName = errors.New("duplicate declaration name")
	ErrInvalidModuleMetadata    = errors.New("invalid module metadata")
	ErrUnknownTableMigration    = errors.New("table migration metadata references unknown table")

	// ErrInvalidDeclarationSQL reports SQL metadata that cannot be compiled
	// against the built module schema.
	ErrInvalidDeclarationSQL = errors.New("invalid declaration SQL")
)

const (
	// ReadModelSurfaceQuery identifies read-model metadata attached to a query.
	ReadModelSurfaceQuery = "query"

	// ReadModelSurfaceView identifies read-model metadata attached to a view.
	ReadModelSurfaceView = "view"

	// MigrationSurfaceTable identifies migration metadata attached to a table.
	MigrationSurfaceTable = "table"

	// MigrationSurfaceQuery identifies migration metadata attached to a query.
	MigrationSurfaceQuery = "query"

	// MigrationSurfaceView identifies migration metadata attached to a view.
	MigrationSurfaceView = "view"
)

// MigrationCompatibility describes author-declared migration compatibility.
type MigrationCompatibility string

const (
	MigrationCompatibilityCompatible MigrationCompatibility = "compatible"
	MigrationCompatibilityBreaking   MigrationCompatibility = "breaking"
	MigrationCompatibilityUnknown    MigrationCompatibility = "unknown"
)

// MigrationClassification describes an author-declared migration change class.
type MigrationClassification string

const (
	MigrationClassificationAdditive           MigrationClassification = "additive"
	MigrationClassificationDeprecated         MigrationClassification = "deprecated"
	MigrationClassificationDataRewriteNeeded  MigrationClassification = "data-rewrite-needed"
	MigrationClassificationManualReviewNeeded MigrationClassification = "manual-review-needed"
)

// MigrationMetadata describes schema/module evolution for review tooling.
type MigrationMetadata struct {
	ModuleVersion   string                    `json:"module_version"`
	SchemaVersion   uint32                    `json:"schema_version"`
	ContractVersion uint32                    `json:"contract_version"`
	PreviousVersion string                    `json:"previous_version"`
	Compatibility   MigrationCompatibility    `json:"compatibility"`
	Classifications []MigrationClassification `json:"classifications"`
	Notes           string                    `json:"notes"`
}

// PermissionMetadata describes passive permission tags required to access an
// exported reducer, query, or view.
type PermissionMetadata struct {
	Required []string
}

// ReadModelMetadata describes passive read-model tags for an exported query or
// view.
type ReadModelMetadata struct {
	Tables []string
	Tags   []string
}

// ReducerDeclaration records module-owned metadata for a named reducer.
type ReducerDeclaration struct {
	Name        string
	Permissions PermissionMetadata
}

type reducerOptions struct {
	permissions PermissionMetadata
}

// ReducerOption configures reducer declaration metadata.
type ReducerOption func(*reducerOptions)

// WithReducerPermissions attaches passive permission metadata to a reducer.
func WithReducerPermissions(metadata PermissionMetadata) ReducerOption {
	return func(o *reducerOptions) {
		o.permissions = copyPermissionMetadata(metadata)
	}
}

// QueryDeclaration declares a named request/response-style read surface owned
// by a module.
type QueryDeclaration struct {
	Name string
	// SQL optionally binds the declaration to an executable OneOffQuery SQL
	// string. Empty SQL leaves the declaration metadata-only.
	SQL         string
	Permissions PermissionMetadata
	ReadModel   ReadModelMetadata
	Migration   MigrationMetadata
}

// ViewDeclaration declares a named live view/subscription surface owned by a
// module.
type ViewDeclaration struct {
	Name string
	// SQL optionally binds the declaration to an executable subscription SQL
	// string. Empty SQL leaves the declaration metadata-only.
	SQL         string
	Permissions PermissionMetadata
	ReadModel   ReadModelMetadata
	Migration   MigrationMetadata
}

// Query registers a named read query declaration and returns the receiver for
// fluent module declarations.
func (m *Module) Query(decl QueryDeclaration) *Module {
	m.queries = append(m.queries, copyQueryDeclaration(decl))
	return m
}

// View registers a named live view/subscription declaration and returns the
// receiver for fluent module declarations.
func (m *Module) View(decl ViewDeclaration) *Module {
	m.views = append(m.views, copyViewDeclaration(decl))
	return m
}

func validateModuleDeclarations(m *Module) error {
	names := make(map[string]struct{}, len(m.queries)+len(m.views))
	for _, query := range m.queries {
		name := strings.TrimSpace(query.Name)
		if name == "" {
			return fmt.Errorf("%w: query", ErrEmptyDeclarationName)
		}
		if _, ok := names[name]; ok {
			return fmt.Errorf("%w: query %q", ErrDuplicateDeclarationName, query.Name)
		}
		names[name] = struct{}{}
	}

	for _, view := range m.views {
		name := strings.TrimSpace(view.Name)
		if name == "" {
			return fmt.Errorf("%w: view", ErrEmptyDeclarationName)
		}
		if _, ok := names[name]; ok {
			return fmt.Errorf("%w: view %q", ErrDuplicateDeclarationName, view.Name)
		}
		names[name] = struct{}{}
	}

	return validateModuleVisibilityFilterNames(m)
}

func validateModuleDeclarationSQL(m *Module, sl protocol.SchemaLookup) error {
	for _, spec := range declaredReadSpecs(m.queries, m.views) {
		if strings.TrimSpace(spec.SQL) == "" {
			continue
		}
		err := protocol.ValidateSQLQueryString(spec.SQL, sl, spec.Validation)
		if err != nil {
			return fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, err)
		}
	}
	return nil
}

func validateModuleMetadata(m *Module, reg schema.SchemaRegistry) error {
	var errs []error
	for key := range m.metadata {
		if strings.TrimSpace(key) == "" {
			errs = append(errs, fmt.Errorf("module.metadata key must not be empty"))
		}
	}

	validateMigrationMetadata("migrations.module", m.migration, &errs)
	for tableName, metadata := range m.tableMigrations {
		validateMigrationMetadata("migrations.table."+tableName, metadata, &errs)
	}
	for _, reducer := range m.reducers {
		validateAuthoredPermissionMetadata("permissions.reducer."+reducer.Name, reducer.Permissions, &errs)
	}
	for _, query := range m.queries {
		validateAuthoredPermissionMetadata("permissions.query."+query.Name, query.Permissions, &errs)
		validateAuthoredReadModelMetadata("read_model.query."+query.Name, query.ReadModel, reg, &errs)
		validateMigrationMetadata("migrations.query."+query.Name, query.Migration, &errs)
	}
	for _, view := range m.views {
		validateAuthoredPermissionMetadata("permissions.view."+view.Name, view.Permissions, &errs)
		validateAuthoredReadModelMetadata("read_model.view."+view.Name, view.ReadModel, reg, &errs)
		validateMigrationMetadata("migrations.view."+view.Name, view.Migration, &errs)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w: %v", ErrInvalidModuleMetadata, errors.Join(errs...))
	}
	return nil
}

func validateAuthoredPermissionMetadata(path string, metadata PermissionMetadata, errs *[]error) {
	validatePermissionRequirements(path, metadata.Required, isReadPermissionMetadataPath(path), errs)
}

func validatePermissionRequirements(path string, required []string, rejectDuplicates bool, errs *[]error) {
	seen := make(map[string]struct{}, len(required))
	for _, required := range required {
		if strings.TrimSpace(required) == "" {
			*errs = append(*errs, fmt.Errorf("%s requirement must not be empty", path))
			continue
		}
		if rejectDuplicates {
			if _, exists := seen[required]; exists {
				*errs = append(*errs, fmt.Errorf("%s requirement %q is duplicated", path, required))
				continue
			}
		}
		seen[required] = struct{}{}
	}
}

func isReadPermissionMetadataPath(path string) bool {
	return strings.Contains(path, "permissions.query.") ||
		strings.Contains(path, "permissions.view.")
}

func validateAuthoredReadModelMetadata(path string, metadata ReadModelMetadata, reg schema.SchemaRegistry, errs *[]error) {
	for _, table := range metadata.Tables {
		if strings.TrimSpace(table) == "" {
			*errs = append(*errs, fmt.Errorf("%s table must not be empty", path))
			continue
		}
		if reg == nil {
			continue
		}
		_, tableSchema, ok := reg.TableByName(table)
		if !ok || tableSchema == nil || tableSchema.Name != table {
			*errs = append(*errs, fmt.Errorf("%s references unknown table %q", path, table))
		}
	}
	for _, tag := range metadata.Tags {
		if strings.TrimSpace(tag) == "" {
			*errs = append(*errs, fmt.Errorf("%s tag must not be empty", path))
		}
	}
}

func validateModuleTableMigrations(m *Module, reg schema.SchemaRegistry) error {
	for tableName := range m.tableMigrations {
		if strings.TrimSpace(tableName) == "" {
			return fmt.Errorf("%w: empty table name", ErrUnknownTableMigration)
		}
		_, table, ok := reg.TableByName(tableName)
		if !ok || table == nil || table.Name != tableName {
			return fmt.Errorf("%w: %q", ErrUnknownTableMigration, tableName)
		}
	}
	return nil
}

func copyQueryDeclaration(query QueryDeclaration) QueryDeclaration {
	return QueryDeclaration{
		Name:        query.Name,
		SQL:         query.SQL,
		Permissions: copyPermissionMetadata(query.Permissions),
		ReadModel:   copyReadModelMetadata(query.ReadModel),
		Migration:   copyMigrationMetadata(query.Migration),
	}
}

func copyViewDeclaration(view ViewDeclaration) ViewDeclaration {
	return ViewDeclaration{
		Name:        view.Name,
		SQL:         view.SQL,
		Permissions: copyPermissionMetadata(view.Permissions),
		ReadModel:   copyReadModelMetadata(view.ReadModel),
		Migration:   copyMigrationMetadata(view.Migration),
	}
}

func copyQueryDeclarations(in []QueryDeclaration) []QueryDeclaration {
	if len(in) == 0 {
		return nil
	}
	out := make([]QueryDeclaration, len(in))
	for i, query := range in {
		out[i] = copyQueryDeclaration(query)
	}
	return out
}

func copyViewDeclarations(in []ViewDeclaration) []ViewDeclaration {
	if len(in) == 0 {
		return nil
	}
	out := make([]ViewDeclaration, len(in))
	for i, view := range in {
		out[i] = copyViewDeclaration(view)
	}
	return out
}

func describeQueryDeclarations(in []QueryDeclaration) []QueryDescription {
	if len(in) == 0 {
		return nil
	}
	out := make([]QueryDescription, len(in))
	for i, query := range in {
		out[i] = QueryDescription{
			Name:        query.Name,
			SQL:         query.SQL,
			Permissions: copyPermissionMetadata(query.Permissions),
			ReadModel:   copyReadModelMetadata(query.ReadModel),
			Migration:   copyMigrationMetadata(query.Migration),
		}
	}
	return out
}

func describeViewDeclarations(in []ViewDeclaration) []ViewDescription {
	if len(in) == 0 {
		return nil
	}
	out := make([]ViewDescription, len(in))
	for i, view := range in {
		out[i] = ViewDescription{
			Name:        view.Name,
			SQL:         view.SQL,
			Permissions: copyPermissionMetadata(view.Permissions),
			ReadModel:   copyReadModelMetadata(view.ReadModel),
			Migration:   copyMigrationMetadata(view.Migration),
		}
	}
	return out
}

func copyReducerDeclarations(in []ReducerDeclaration) []ReducerDeclaration {
	if len(in) == 0 {
		return nil
	}
	out := make([]ReducerDeclaration, len(in))
	for i, reducer := range in {
		out[i] = ReducerDeclaration{
			Name:        reducer.Name,
			Permissions: copyPermissionMetadata(reducer.Permissions),
		}
	}
	return out
}

func copyPermissionMetadata(in PermissionMetadata) PermissionMetadata {
	return PermissionMetadata{Required: copyStringSlice(in.Required)}
}

func copyReadModelMetadata(in ReadModelMetadata) ReadModelMetadata {
	return ReadModelMetadata{
		Tables: copyStringSlice(in.Tables),
		Tags:   copyStringSlice(in.Tags),
	}
}

func copyMigrationMetadata(in MigrationMetadata) MigrationMetadata {
	return MigrationMetadata{
		ModuleVersion:   in.ModuleVersion,
		SchemaVersion:   in.SchemaVersion,
		ContractVersion: in.ContractVersion,
		PreviousVersion: in.PreviousVersion,
		Compatibility:   in.Compatibility,
		Classifications: copyMigrationClassifications(in.Classifications),
		Notes:           in.Notes,
	}
}

func copyMigrationClassifications(in []MigrationClassification) []MigrationClassification {
	if len(in) == 0 {
		return nil
	}
	out := make([]MigrationClassification, len(in))
	copy(out, in)
	return out
}

func copyMigrationMetadataMap(in map[string]MigrationMetadata) map[string]MigrationMetadata {
	if len(in) == 0 {
		return map[string]MigrationMetadata{}
	}
	out := make(map[string]MigrationMetadata, len(in))
	for k, v := range in {
		out[k] = copyMigrationMetadata(v)
	}
	return out
}

func hasPermissionMetadata(in PermissionMetadata) bool {
	return len(in.Required) > 0
}

func hasReadModelMetadata(in ReadModelMetadata) bool {
	return len(in.Tables) > 0 || len(in.Tags) > 0
}

func hasMigrationMetadata(in MigrationMetadata) bool {
	return in.ModuleVersion != "" ||
		in.SchemaVersion != 0 ||
		in.ContractVersion != 0 ||
		in.PreviousVersion != "" ||
		in.Compatibility != "" ||
		len(in.Classifications) > 0 ||
		in.Notes != ""
}

func copyStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	return copyStringSlice(in)
}

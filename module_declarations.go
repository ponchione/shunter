package shunter

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrEmptyDeclarationName     = errors.New("declaration name must not be empty")
	ErrDuplicateDeclarationName = errors.New("duplicate declaration name")
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
	Name        string
	Permissions PermissionMetadata
	ReadModel   ReadModelMetadata
	Migration   MigrationMetadata
}

// ViewDeclaration declares a named live view/subscription surface owned by a
// module.
type ViewDeclaration struct {
	Name        string
	Permissions PermissionMetadata
	ReadModel   ReadModelMetadata
	Migration   MigrationMetadata
}

// Query registers a named read query declaration and returns the receiver for
// fluent module declarations.
func (m *Module) Query(decl QueryDeclaration) *Module {
	m.queries = append(m.queries, decl)
	return m
}

// View registers a named live view/subscription declaration and returns the
// receiver for fluent module declarations.
func (m *Module) View(decl ViewDeclaration) *Module {
	m.views = append(m.views, decl)
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

	return nil
}

func copyQueryDeclarations(in []QueryDeclaration) []QueryDeclaration {
	if len(in) == 0 {
		return nil
	}
	out := make([]QueryDeclaration, len(in))
	for i, query := range in {
		out[i] = QueryDeclaration{
			Name:        query.Name,
			Permissions: copyPermissionMetadata(query.Permissions),
			ReadModel:   copyReadModelMetadata(query.ReadModel),
			Migration:   copyMigrationMetadata(query.Migration),
		}
	}
	return out
}

func copyViewDeclarations(in []ViewDeclaration) []ViewDeclaration {
	if len(in) == 0 {
		return nil
	}
	out := make([]ViewDeclaration, len(in))
	for i, view := range in {
		out[i] = ViewDeclaration{
			Name:        view.Name,
			Permissions: copyPermissionMetadata(view.Permissions),
			ReadModel:   copyReadModelMetadata(view.ReadModel),
			Migration:   copyMigrationMetadata(view.Migration),
		}
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

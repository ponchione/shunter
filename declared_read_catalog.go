package shunter

import (
	"fmt"
	"strings"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type declaredReadKind string

const (
	declaredReadKindQuery declaredReadKind = "query"
	declaredReadKindView  declaredReadKind = "view"
)

type declaredReadCatalog struct {
	entries map[string]declaredReadEntry
}

type declaredReadEntry struct {
	Name               string
	Kind               declaredReadKind
	SQL                string
	Permissions        PermissionMetadata
	ReadModel          ReadModelMetadata
	Migration          MigrationMetadata
	ReferencedTables   []schema.TableID
	UsesCallerIdentity bool

	compiled *protocol.CompiledSQLQuery
}

type declaredReadSpec struct {
	Name        string
	Kind        declaredReadKind
	SQL         string
	Permissions PermissionMetadata
	ReadModel   ReadModelMetadata
	Migration   MigrationMetadata
	Validation  protocol.SQLQueryValidationOptions
}

func newDeclaredReadCatalog(queries []QueryDeclaration, views []ViewDeclaration, sl protocol.SchemaLookup) (*declaredReadCatalog, error) {
	catalog := &declaredReadCatalog{entries: make(map[string]declaredReadEntry, len(queries)+len(views))}
	for _, spec := range declaredReadSpecs(queries, views) {
		entry, err := declaredReadCatalogEntry(spec, sl)
		if err != nil {
			return nil, err
		}
		catalog.entries[entry.Name] = entry
	}
	return catalog, nil
}

func declaredReadSpecs(queries []QueryDeclaration, views []ViewDeclaration) []declaredReadSpec {
	specs := make([]declaredReadSpec, 0, len(queries)+len(views))
	for _, query := range queries {
		specs = append(specs, declaredReadSpec{
			Name:        query.Name,
			Kind:        declaredReadKindQuery,
			SQL:         query.SQL,
			Permissions: query.Permissions,
			ReadModel:   query.ReadModel,
			Migration:   query.Migration,
			Validation: protocol.SQLQueryValidationOptions{
				AllowLimit:      true,
				AllowProjection: true,
				AllowOrderBy:    true,
				AllowOffset:     true,
			},
		})
	}
	for _, view := range views {
		specs = append(specs, declaredReadSpec{
			Name:        view.Name,
			Kind:        declaredReadKindView,
			SQL:         view.SQL,
			Permissions: view.Permissions,
			ReadModel:   view.ReadModel,
			Migration:   view.Migration,
			Validation: protocol.SQLQueryValidationOptions{
				AllowLimit:      false,
				AllowProjection: true,
				AllowOrderBy:    true,
			},
		})
	}
	return specs
}

func declaredReadCatalogEntry(spec declaredReadSpec, sl protocol.SchemaLookup) (declaredReadEntry, error) {
	entry := declaredReadEntry{
		Name:        spec.Name,
		Kind:        spec.Kind,
		SQL:         spec.SQL,
		Permissions: copyPermissionMetadata(spec.Permissions),
		ReadModel:   copyReadModelMetadata(spec.ReadModel),
		Migration:   copyMigrationMetadata(spec.Migration),
	}
	if strings.TrimSpace(spec.SQL) == "" {
		return entry, nil
	}
	compiled, err := compileDeclaredReadSQL(spec.SQL, sl, spec.Validation)
	if err != nil {
		return declaredReadEntry{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, err)
	}
	if spec.Kind == declaredReadKindView {
		if aggregate := compiled.SubscriptionAggregate(); aggregate != nil {
			if err := subscription.ValidateAggregate(compiled.Predicate(), aggregate, sl); err != nil {
				return declaredReadEntry{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, err)
			}
			if compiled.HasOrderBy() {
				return declaredReadEntry{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, fmt.Errorf("%w: live ORDER BY views do not support aggregate views", subscription.ErrInvalidPredicate))
			}
		} else if err := subscription.ValidateProjection(compiled.Predicate(), compiled.SubscriptionProjection(), sl); err != nil {
			return declaredReadEntry{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, err)
		}
		if err := subscription.ValidateOrderBy(compiled.Predicate(), compiled.SubscriptionOrderBy(), compiled.SubscriptionAggregate(), sl); err != nil {
			return declaredReadEntry{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, err)
		}
	}
	entry.attachCompiled(compiled)
	return entry, nil
}

func compileDeclaredReadSQL(sqlText string, sl protocol.SchemaLookup, opts protocol.SQLQueryValidationOptions) (protocol.CompiledSQLQuery, error) {
	var caller types.Identity
	return protocol.CompileSQLQueryString(sqlText, sl, &caller, opts)
}

func (e *declaredReadEntry) attachCompiled(compiled protocol.CompiledSQLQuery) {
	copied := compiled.Copy()
	e.compiled = &copied
	e.ReferencedTables = copied.ReferencedTables()
	e.UsesCallerIdentity = copied.UsesCallerIdentity()
}

func (c *declaredReadCatalog) lookup(name string) (declaredReadEntry, bool) {
	if c == nil {
		return declaredReadEntry{}, false
	}
	entry, ok := c.entries[name]
	if !ok {
		return declaredReadEntry{}, false
	}
	return copyDeclaredReadEntry(entry), true
}

func copyDeclaredReadEntry(in declaredReadEntry) declaredReadEntry {
	out := declaredReadEntry{
		Name:               in.Name,
		Kind:               in.Kind,
		SQL:                in.SQL,
		Permissions:        copyPermissionMetadata(in.Permissions),
		ReadModel:          copyReadModelMetadata(in.ReadModel),
		Migration:          copyMigrationMetadata(in.Migration),
		ReferencedTables:   copyTableIDSlice(in.ReferencedTables),
		UsesCallerIdentity: in.UsesCallerIdentity,
	}
	if in.compiled != nil {
		compiled := in.compiled.Copy()
		out.compiled = &compiled
	}
	return out
}

func copyTableIDSlice(in []schema.TableID) []schema.TableID {
	if len(in) == 0 {
		return nil
	}
	out := make([]schema.TableID, len(in))
	copy(out, in)
	return out
}

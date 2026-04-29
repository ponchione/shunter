package shunter

import (
	"fmt"
	"strings"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
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

func newDeclaredReadCatalog(queries []QueryDeclaration, views []ViewDeclaration, sl protocol.SchemaLookup) (*declaredReadCatalog, error) {
	catalog := &declaredReadCatalog{entries: make(map[string]declaredReadEntry, len(queries)+len(views))}
	for _, query := range queries {
		entry, err := declaredQueryCatalogEntry(query, sl)
		if err != nil {
			return nil, err
		}
		catalog.entries[entry.Name] = entry
	}
	for _, view := range views {
		entry, err := declaredViewCatalogEntry(view, sl)
		if err != nil {
			return nil, err
		}
		catalog.entries[entry.Name] = entry
	}
	return catalog, nil
}

func declaredQueryCatalogEntry(query QueryDeclaration, sl protocol.SchemaLookup) (declaredReadEntry, error) {
	entry := declaredReadEntry{
		Name:        query.Name,
		Kind:        declaredReadKindQuery,
		SQL:         query.SQL,
		Permissions: copyPermissionMetadata(query.Permissions),
		ReadModel:   copyReadModelMetadata(query.ReadModel),
		Migration:   copyMigrationMetadata(query.Migration),
	}
	if strings.TrimSpace(query.SQL) == "" {
		return entry, nil
	}
	compiled, err := compileDeclaredReadSQL(query.SQL, sl, protocol.SQLQueryValidationOptions{
		AllowLimit:      true,
		AllowProjection: true,
	})
	if err != nil {
		return declaredReadEntry{}, fmt.Errorf("%w: query %q: %v", ErrInvalidDeclarationSQL, query.Name, err)
	}
	entry.attachCompiled(compiled)
	return entry, nil
}

func declaredViewCatalogEntry(view ViewDeclaration, sl protocol.SchemaLookup) (declaredReadEntry, error) {
	entry := declaredReadEntry{
		Name:        view.Name,
		Kind:        declaredReadKindView,
		SQL:         view.SQL,
		Permissions: copyPermissionMetadata(view.Permissions),
		ReadModel:   copyReadModelMetadata(view.ReadModel),
		Migration:   copyMigrationMetadata(view.Migration),
	}
	if strings.TrimSpace(view.SQL) == "" {
		return entry, nil
	}
	compiled, err := compileDeclaredReadSQL(view.SQL, sl, protocol.SQLQueryValidationOptions{
		AllowLimit:      false,
		AllowProjection: false,
	})
	if err != nil {
		return declaredReadEntry{}, fmt.Errorf("%w: view %q: %v", ErrInvalidDeclarationSQL, view.Name, err)
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

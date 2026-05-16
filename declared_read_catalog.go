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
	Parameters         *ProductSchema
	Permissions        PermissionMetadata
	ReadModel          ReadModelMetadata
	Migration          MigrationMetadata
	ReferencedTables   []schema.TableID
	UsesCallerIdentity bool

	compiled *protocol.CompiledSQLQuery
	template *protocol.CompiledSQLQueryTemplate
}

type declaredReadSpec struct {
	Name        string
	Kind        declaredReadKind
	SQL         string
	Parameters  *ProductSchema
	Permissions PermissionMetadata
	ReadModel   ReadModelMetadata
	Migration   MigrationMetadata
	Validation  protocol.SQLQueryValidationOptions
}

var declaredReadSQLValidation = protocol.SQLQueryValidationOptions{
	AllowLimit:      true,
	AllowProjection: true,
	AllowOrderBy:    true,
	AllowOffset:     true,
}

func newDeclaredReadCatalog(queries []queryDeclaration, views []viewDeclaration, sl protocol.SchemaLookup) (*declaredReadCatalog, error) {
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

func declaredReadSpecs(queries []queryDeclaration, views []viewDeclaration) []declaredReadSpec {
	specs := make([]declaredReadSpec, 0, len(queries)+len(views))
	for _, query := range queries {
		specs = append(specs, declaredReadSpec{
			Name:        query.Name,
			Kind:        declaredReadKindQuery,
			SQL:         query.SQL,
			Parameters:  copyProductSchemaPtr(query.Parameters),
			Permissions: query.Permissions,
			ReadModel:   query.ReadModel,
			Migration:   query.Migration,
			Validation:  declaredReadSQLValidation,
		})
	}
	for _, view := range views {
		specs = append(specs, declaredReadSpec{
			Name:        view.Name,
			Kind:        declaredReadKindView,
			SQL:         view.SQL,
			Parameters:  copyProductSchemaPtr(view.Parameters),
			Permissions: view.Permissions,
			ReadModel:   view.ReadModel,
			Migration:   view.Migration,
			Validation:  declaredReadSQLValidation,
		})
	}
	return specs
}

func declaredReadCatalogEntry(spec declaredReadSpec, sl protocol.SchemaLookup) (declaredReadEntry, error) {
	entry := declaredReadEntry{
		Name:        spec.Name,
		Kind:        spec.Kind,
		SQL:         spec.SQL,
		Parameters:  copyProductSchemaPtr(spec.Parameters),
		Permissions: copyPermissionMetadata(spec.Permissions),
		ReadModel:   copyReadModelMetadata(spec.ReadModel),
		Migration:   copyMigrationMetadata(spec.Migration),
	}
	if strings.TrimSpace(spec.SQL) == "" {
		return entry, nil
	}
	if declaredReadHasAppParameters(spec.Parameters) {
		template, err := compileDeclaredReadSQLTemplate(spec.SQL, sl, spec.Validation, spec.Parameters)
		if err != nil {
			return declaredReadEntry{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, err)
		}
		if spec.Kind == declaredReadKindView {
			if err := validateDeclaredViewSQL(template, sl); err != nil {
				return declaredReadEntry{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, err)
			}
		}
		entry.ReferencedTables = template.ReferencedTables()
		entry.UsesCallerIdentity = template.UsesCallerIdentity()
		templateCopy := template.Copy()
		entry.template = &templateCopy
		return entry, nil
	}
	compiled, err := compileDeclaredReadSQL(spec.SQL, sl, spec.Validation)
	if err != nil {
		return declaredReadEntry{}, fmt.Errorf("%w: %s %q: %v", ErrInvalidDeclarationSQL, spec.Kind, spec.Name, err)
	}
	if spec.Kind == declaredReadKindView {
		if err := validateDeclaredViewSQL(compiled, sl); err != nil {
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

func compileDeclaredReadSQLTemplate(sqlText string, sl protocol.SchemaLookup, opts protocol.SQLQueryValidationOptions, parameters *ProductSchema) (protocol.CompiledSQLQueryTemplate, error) {
	var caller types.Identity
	sqlParameters, err := declaredReadSQLParameters(parameters)
	if err != nil {
		return protocol.CompiledSQLQueryTemplate{}, err
	}
	return protocol.CompileSQLQueryTemplateString(sqlText, sl, &caller, opts, sqlParameters)
}

type declaredViewSQLSource interface {
	Predicate() subscription.Predicate
	SubscriptionAggregate() *subscription.Aggregate
	HasOrderBy() bool
	HasLimit() bool
	HasOffset() bool
	SubscriptionProjection() []subscription.ProjectionColumn
	SubscriptionOrderBy() []subscription.OrderByColumn
	SubscriptionLimit() *uint64
	SubscriptionOffset() *uint64
}

func validateDeclaredViewSQL(compiled declaredViewSQLSource, sl protocol.SchemaLookup) error {
	if aggregate := compiled.SubscriptionAggregate(); aggregate != nil {
		if err := subscription.ValidateAggregate(compiled.Predicate(), aggregate, sl); err != nil {
			return err
		}
		if compiled.HasOrderBy() {
			return fmt.Errorf("%w: live ORDER BY views do not support aggregate views", subscription.ErrInvalidPredicate)
		}
		if compiled.HasLimit() {
			return fmt.Errorf("%w: live LIMIT views do not support aggregate views", subscription.ErrInvalidPredicate)
		}
		if compiled.HasOffset() {
			return fmt.Errorf("%w: live OFFSET views do not support aggregate views", subscription.ErrInvalidPredicate)
		}
		return nil
	}
	if err := subscription.ValidateProjection(compiled.Predicate(), compiled.SubscriptionProjection(), sl); err != nil {
		return err
	}
	if err := subscription.ValidateOrderBy(compiled.Predicate(), compiled.SubscriptionOrderBy(), compiled.SubscriptionAggregate(), sl); err != nil {
		return err
	}
	if err := subscription.ValidateLimit(compiled.Predicate(), compiled.SubscriptionLimit(), compiled.SubscriptionAggregate(), sl); err != nil {
		return err
	}
	if err := subscription.ValidateOffset(compiled.Predicate(), compiled.SubscriptionOffset(), compiled.SubscriptionAggregate(), sl); err != nil {
		return err
	}
	return nil
}

func declaredReadHasAppParameters(parameters *ProductSchema) bool {
	return parameters != nil && len(parameters.Columns) != 0
}

func declaredReadSQLParameters(parameters *ProductSchema) ([]protocol.SQLQueryParameter, error) {
	if !declaredReadHasAppParameters(parameters) {
		return nil, nil
	}
	out := make([]protocol.SQLQueryParameter, len(parameters.Columns))
	for i, column := range parameters.Columns {
		kind, ok := valueKindFromExportString(column.Type)
		if !ok {
			return nil, fmt.Errorf("parameter %q type %q is invalid", column.Name, column.Type)
		}
		out[i] = protocol.SQLQueryParameter{Name: column.Name, Type: kind}
	}
	return out, nil
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
		Parameters:         copyProductSchemaPtr(in.Parameters),
		Permissions:        copyPermissionMetadata(in.Permissions),
		ReadModel:          copyReadModelMetadata(in.ReadModel),
		Migration:          copyMigrationMetadata(in.Migration),
		ReferencedTables:   copySlice(in.ReferencedTables),
		UsesCallerIdentity: in.UsesCallerIdentity,
	}
	if in.compiled != nil {
		compiled := in.compiled.Copy()
		out.compiled = &compiled
	}
	if in.template != nil {
		template := in.template.Copy()
		out.template = &template
	}
	return out
}

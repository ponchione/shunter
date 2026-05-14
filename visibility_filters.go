package shunter

import (
	"fmt"
	"strings"

	"github.com/ponchione/shunter/protocol"
	querysql "github.com/ponchione/shunter/query/sql"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// VisibilityFilterDeclaration declares a row-level visibility filter for one
// returned table. Validated filter metadata is applied to raw one-off reads,
// raw subscriptions, declared queries, and declared views.
type VisibilityFilterDeclaration struct {
	Name string
	SQL  string
}

// VisibilityFilterDescription is validated visibility-filter metadata exported
// through runtime descriptions and module contracts.
type VisibilityFilterDescription struct {
	Name               string         `json:"name"`
	SQL                string         `json:"sql"`
	ReturnTable        string         `json:"return_table"`
	ReturnTableID      schema.TableID `json:"return_table_id"`
	UsesCallerIdentity bool           `json:"uses_caller_identity"`
}

// VisibilityFilter registers a row-level visibility filter declaration and
// returns the receiver for fluent module declarations.
func (m *Module) VisibilityFilter(decl VisibilityFilterDeclaration) *Module {
	m.visibilityFilters = append(m.visibilityFilters, copyVisibilityFilterDeclaration(decl))
	return m
}

func validateModuleVisibilityFilterNames(m *Module) error {
	names := make(map[string]struct{}, len(m.visibilityFilters))
	for _, filter := range m.visibilityFilters {
		name := strings.TrimSpace(filter.Name)
		if name == "" {
			return fmt.Errorf("%w: visibility filter", ErrEmptyDeclarationName)
		}
		if _, ok := names[name]; ok {
			return fmt.Errorf("%w: visibility filter %q", ErrDuplicateDeclarationName, filter.Name)
		}
		names[name] = struct{}{}
	}
	return nil
}

func validateModuleVisibilityFilters(m *Module, sl protocol.SchemaLookup) ([]VisibilityFilterDescription, error) {
	if len(m.visibilityFilters) == 0 {
		return nil, nil
	}
	out := make([]VisibilityFilterDescription, len(m.visibilityFilters))
	for i, filter := range m.visibilityFilters {
		desc, err := visibilityFilterDescription(filter, sl)
		if err != nil {
			return nil, err
		}
		out[i] = desc
	}
	return out, nil
}

func visibilityFilterDescription(filter VisibilityFilterDeclaration, sl protocol.SchemaLookup) (VisibilityFilterDescription, error) {
	if strings.TrimSpace(filter.SQL) == "" {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: SQL must not be empty", ErrInvalidDeclarationSQL, filter.Name)
	}
	var caller types.Identity
	compiled, err := protocol.CompileSQLQueryString(filter.SQL, sl, &caller, protocol.SQLQueryValidationOptions{
		AllowLimit:      false,
		AllowProjection: false,
	})
	if err != nil {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: %v", ErrInvalidDeclarationSQL, filter.Name, err)
	}

	stmt, err := querysql.Parse(filter.SQL)
	if err != nil {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: %v", ErrInvalidDeclarationSQL, filter.Name, err)
	}
	if stmt.Join != nil {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: joins are not supported", ErrInvalidDeclarationSQL, filter.Name)
	}
	if len(stmt.ProjectionColumns) != 0 || stmt.Aggregate != nil {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: column projections are not supported", ErrInvalidDeclarationSQL, filter.Name)
	}
	if stmt.HasLimit || stmt.UnsupportedLimit || stmt.InvalidLimit != nil || stmt.Limit != nil {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: LIMIT is not supported", ErrInvalidDeclarationSQL, filter.Name)
	}

	returnTable := compiled.TableName()
	if strings.TrimSpace(returnTable) == "" {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: return table could not be resolved", ErrInvalidDeclarationSQL, filter.Name)
	}
	returnTableID, ts, ok := sl.TableByName(returnTable)
	if !ok || ts == nil || ts.Name != returnTable {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: return table %q is unknown", ErrInvalidDeclarationSQL, filter.Name, returnTable)
	}
	referenced := compiled.ReferencedTables()
	if len(referenced) != 1 || referenced[0] != returnTableID {
		return VisibilityFilterDescription{}, fmt.Errorf("%w: visibility filter %q: SQL must reference exactly the return table", ErrInvalidDeclarationSQL, filter.Name)
	}

	return VisibilityFilterDescription{
		Name:               filter.Name,
		SQL:                filter.SQL,
		ReturnTable:        returnTable,
		ReturnTableID:      returnTableID,
		UsesCallerIdentity: compiled.UsesCallerIdentity(),
	}, nil
}

func copyVisibilityFilterDeclaration(filter VisibilityFilterDeclaration) VisibilityFilterDeclaration {
	return VisibilityFilterDeclaration{
		Name: filter.Name,
		SQL:  filter.SQL,
	}
}

func describeVisibilityFilterDeclarations(in []VisibilityFilterDeclaration) []VisibilityFilterDescription {
	if len(in) == 0 {
		return nil
	}
	out := make([]VisibilityFilterDescription, len(in))
	for i, filter := range in {
		out[i] = VisibilityFilterDescription{
			Name: filter.Name,
			SQL:  filter.SQL,
		}
	}
	return out
}

func copyVisibilityFilterDescription(filter VisibilityFilterDescription) VisibilityFilterDescription {
	return VisibilityFilterDescription{
		Name:               filter.Name,
		SQL:                filter.SQL,
		ReturnTable:        filter.ReturnTable,
		ReturnTableID:      filter.ReturnTableID,
		UsesCallerIdentity: filter.UsesCallerIdentity,
	}
}

func copyVisibilityFilterDescriptions(in []VisibilityFilterDescription) []VisibilityFilterDescription {
	if len(in) == 0 {
		return nil
	}
	out := make([]VisibilityFilterDescription, len(in))
	for i, filter := range in {
		out[i] = copyVisibilityFilterDescription(filter)
	}
	return out
}

func normalizeVisibilityFilterDescriptions(in []VisibilityFilterDescription) []VisibilityFilterDescription {
	if len(in) == 0 {
		return []VisibilityFilterDescription{}
	}
	return copyVisibilityFilterDescriptions(in)
}

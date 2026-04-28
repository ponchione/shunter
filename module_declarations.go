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

// QueryDeclaration declares a named request/response-style read surface owned
// by a module.
type QueryDeclaration struct {
	Name string
}

// ViewDeclaration declares a named live view/subscription surface owned by a
// module.
type ViewDeclaration struct {
	Name string
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
	copy(out, in)
	return out
}

func copyViewDeclarations(in []ViewDeclaration) []ViewDeclaration {
	if len(in) == 0 {
		return nil
	}
	out := make([]ViewDeclaration, len(in))
	copy(out, in)
	return out
}

func describeQueryDeclarations(in []QueryDeclaration) []QueryDescription {
	if len(in) == 0 {
		return nil
	}
	out := make([]QueryDescription, len(in))
	for i, query := range in {
		out[i] = QueryDescription{Name: query.Name}
	}
	return out
}

func describeViewDeclarations(in []ViewDeclaration) []ViewDescription {
	if len(in) == 0 {
		return nil
	}
	out := make([]ViewDescription, len(in))
	for i, view := range in {
		out[i] = ViewDescription{Name: view.Name}
	}
	return out
}

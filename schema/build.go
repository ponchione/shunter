package schema

import (
	"context"
	"errors"
)

// Engine holds an immutable SchemaRegistry and runtime configuration.
// Subsystem wiring happens at Start().
type Engine struct {
	registry SchemaRegistry
	opts     EngineOptions
}

// Registry returns the immutable schema registry.
func (e *Engine) Registry() SchemaRegistry { return e.registry }

// Start performs runtime initialization (stub — deferred to SPEC-002/003 integration).
func (e *Engine) Start(_ context.Context) error { return nil }

// Build validates all registrations, assigns stable IDs, and constructs the Engine.
func (b *Builder) Build(opts EngineOptions) (*Engine, error) {
	if b.built {
		return nil, ErrAlreadyBuilt
	}

	// Validate structure + reducer/schema rules on user registrations.
	var allErrs []error
	allErrs = append(allErrs, validateStructure(b)...)
	allErrs = append(allErrs, validateReducerAndSchemaRules(b)...)
	if len(allErrs) > 0 {
		return nil, errors.Join(allErrs...)
	}

	// Build against a temporary builder that includes system tables so they go
	// through the same registration and structural validation path.
	userTableCount := len(b.tables)
	builtBuilder := *b
	builtBuilder.tables = append([]TableDefinition(nil), b.tables...)
	builtBuilder.reducerOrder = append([]string(nil), b.reducerOrder...)
	registerSystemTables(&builtBuilder)
	if systemErrs := validateStructure(&builtBuilder); len(systemErrs) > 0 {
		return nil, errors.Join(systemErrs...)
	}

	// Build TableSchemas with assigned IDs.
	schemas := make([]TableSchema, len(builtBuilder.tables))
	for i, td := range builtBuilder.tables {
		ts := TableSchema{
			ID:   TableID(i),
			Name: td.Name,
		}

		// Build columns.
		ts.Columns = make([]ColumnSchema, len(td.Columns))
		for j, cd := range td.Columns {
			ts.Columns[j] = ColumnSchema{
				Index:         j,
				Name:          cd.Name,
				Type:          cd.Type,
				AutoIncrement: cd.AutoIncrement,
			}
		}

		// Synthesize primary index + assign IndexIDs.
		var idxID IndexID
		var pkColIdx int = -1
		for j, cd := range td.Columns {
			if cd.PrimaryKey {
				pkColIdx = j
				break
			}
		}
		if pkColIdx >= 0 {
			ts.Indexes = append(ts.Indexes, IndexSchema{
				ID:      idxID,
				Name:    "pk",
				Columns: []int{pkColIdx},
				Unique:  true,
				Primary: true,
			})
			idxID++
		}

		// Secondary indexes from IndexDefinitions.
		for _, id := range td.Indexes {
			cols := make([]int, len(id.Columns))
			for k, cn := range id.Columns {
				for j, cd := range td.Columns {
					if cd.Name == cn {
						cols[k] = j
						break
					}
				}
			}
			ts.Indexes = append(ts.Indexes, IndexSchema{
				ID:      idxID,
				Name:    id.Name,
				Columns: cols,
				Unique:  id.Unique,
			})
			idxID++
		}

		schemas[i] = ts
	}

	reg := newSchemaRegistry(schemas, userTableCount, &builtBuilder)
	b.built = true

	return &Engine{registry: reg, opts: opts}, nil
}

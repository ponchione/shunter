package schema

import "errors"

// Engine holds an immutable SchemaRegistry and runtime configuration.
// Subsystem wiring happens at Start().
type Engine struct {
	registry SchemaRegistry
	opts     EngineOptions
}

// Registry returns the immutable schema registry.
func (e *Engine) Registry() SchemaRegistry { return e.registry }

// Build validates all registrations, assigns stable IDs, constructs the Engine,
// and freezes the builder against later builds.
func (b *Builder) Build(opts EngineOptions) (*Engine, error) {
	if b.built {
		return nil, ErrAlreadyBuilt
	}
	engine, err := b.buildEngine(opts)
	if err != nil {
		return nil, err
	}
	b.built = true
	return engine, nil
}

// BuildPreview validates all registrations and constructs an Engine without
// freezing the builder. It is intended for higher-level validation that needs a
// schema registry before deciding whether to consume the module.
func (b *Builder) BuildPreview(opts EngineOptions) (*Engine, error) {
	if b.built {
		return nil, ErrAlreadyBuilt
	}
	return b.buildEngine(opts)
}

func (b *Builder) buildEngine(opts EngineOptions) (*Engine, error) {
	// Validate structure + reducer/schema rules on user registrations.
	var allErrs []error
	allErrs = append(allErrs, validateStructure(b)...)
	allErrs = append(allErrs, validateReducerAndSchemaRules(b)...)
	if len(allErrs) > 0 {
		return nil, errors.Join(allErrs...)
	}

	// Build against a temporary builder that includes system tables so they go
	// through the same registration and structural validation path.
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
				Nullable:      cd.Nullable,
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

	reg := newSchemaRegistry(schemas, &builtBuilder)

	return &Engine{registry: reg, opts: opts}, nil
}

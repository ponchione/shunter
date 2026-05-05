package schema

import "fmt"

// buildTableDefinition converts discovered field metadata into a TableDefinition.
func buildTableDefinition(typeName string, fields []fieldInfo, opts ...TableOption) (TableDefinition, error) {
	var o tableOptions
	for _, opt := range opts {
		opt(&o)
	}

	tableName := o.name
	if tableName == "" {
		tableName = ToSnakeCase(typeName)
	}
	readPolicy := ReadPolicy{}
	if o.readPolicy != nil {
		readPolicy = *o.readPolicy
	}

	columns := make([]ColumnDefinition, len(fields))
	for i, f := range fields {
		columns[i] = ColumnDefinition{
			Name:          f.ColumnName,
			Type:          f.Type,
			PrimaryKey:    f.Tags.PrimaryKey,
			Nullable:      f.Nullable,
			AutoIncrement: f.Tags.AutoIncrement,
		}
	}

	// Assemble index definitions from tag directives.
	// Named composite indexes group fields by index name.
	namedIndexes := make(map[string]*indexBuild)
	var namedOrder []string
	var indexes []IndexDefinition

	for _, f := range fields {
		if f.Tags.PrimaryKey {
			// No explicit IndexDefinition for PK — synthesized in Build().
			continue
		}

		if f.Tags.Unique && f.Tags.IndexName == "" && !f.Tags.Index {
			indexes = append(indexes, IndexDefinition{
				Name:    DefaultIndexName(f.ColumnName, false, true),
				Columns: []string{f.ColumnName},
				Unique:  true,
			})
		}

		if f.Tags.Index {
			indexes = append(indexes, IndexDefinition{
				Name:    DefaultIndexName(f.ColumnName, false, false),
				Columns: []string{f.ColumnName},
				Unique:  false,
			})
		}

		if f.Tags.IndexName != "" {
			ib, exists := namedIndexes[f.Tags.IndexName]
			if !exists {
				ib = &indexBuild{unique: f.Tags.Unique}
				namedIndexes[f.Tags.IndexName] = ib
				namedOrder = append(namedOrder, f.Tags.IndexName)
			} else if ib.unique != f.Tags.Unique {
				return TableDefinition{}, fmt.Errorf(
					"schema error: %s: mixed unique flags on composite index %q",
					typeName, f.Tags.IndexName,
				)
			}
			ib.columns = append(ib.columns, f.ColumnName)
		}
	}

	// Emit named composite indexes in field order.
	for _, name := range namedOrder {
		ib := namedIndexes[name]
		indexes = append(indexes, IndexDefinition{
			Name:    name,
			Columns: ib.columns,
			Unique:  ib.unique,
		})
	}

	return TableDefinition{
		Name:       tableName,
		Columns:    columns,
		Indexes:    indexes,
		ReadPolicy: readPolicy,
	}, nil
}

type indexBuild struct {
	columns []string
	unique  bool
}

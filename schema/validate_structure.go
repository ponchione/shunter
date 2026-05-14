package schema

import (
	"fmt"
	"regexp"
)

var (
	tableNamePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	columnNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// validateStructure checks all table/column/index structural rules.
// Returns all errors found in one pass.
func validateStructure(b *Builder) []error {
	var errs []error
	tableNames := make(map[string]bool)

	for _, t := range b.tables {
		// Table name.
		if t.Name == "" {
			errs = append(errs, ErrEmptyTableName)
			continue
		}
		if !tableNamePattern.MatchString(t.Name) {
			errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidTableName, t.Name))
		}
		if tableNames[t.Name] {
			errs = append(errs, fmt.Errorf("%w: %q", ErrDuplicateTableName, t.Name))
		}
		tableNames[t.Name] = true
		if err := ValidateReadPolicy(t.ReadPolicy); err != nil {
			errs = append(errs, fmt.Errorf("table %q read policy: %w", t.Name, err))
		}

		// Must have at least one column.
		if len(t.Columns) == 0 {
			errs = append(errs, fmt.Errorf("table %q: must have at least one column", t.Name))
			continue
		}

		// Column checks.
		colNames := make(map[string]bool)
		pkCount := 0
		autoIncrementCount := 0
		for _, c := range t.Columns {
			if c.Name == "" {
				errs = append(errs, fmt.Errorf("table %q: %w", t.Name, ErrEmptyColumnName))
				continue
			}
			if !columnNamePattern.MatchString(c.Name) {
				errs = append(errs, fmt.Errorf("table %q: %w: %q", t.Name, ErrInvalidColumnName, c.Name))
			}
			if colNames[c.Name] {
				errs = append(errs, fmt.Errorf("table %q: duplicate column name %q", t.Name, c.Name))
			}
			colNames[c.Name] = true
			if c.Type < KindBool || c.Type > KindJSON {
				errs = append(errs, fmt.Errorf("table %q column %q: invalid ValueKind %v", t.Name, c.Name, c.Type))
			}
			if c.PrimaryKey {
				pkCount++
			}
			if c.AutoIncrement {
				autoIncrementCount++
				if c.Nullable {
					errs = append(errs, fmt.Errorf("table %q column %q: %w", t.Name, c.Name, ErrNullableAutoIncrement))
				}
				if _, _, ok := AutoIncrementBounds(c.Type); !ok {
					errs = append(errs, fmt.Errorf("table %q column %q: %w", t.Name, c.Name, ErrAutoIncrementType))
				}
				if !c.PrimaryKey {
					// Check if column has a matching unique index.
					hasUnique := false
					for _, idx := range t.Indexes {
						if idx.Unique && len(idx.Columns) == 1 && idx.Columns[0] == c.Name {
							hasUnique = true
							break
						}
					}
					if !hasUnique {
						errs = append(errs, fmt.Errorf("table %q column %q: %w", t.Name, c.Name, ErrAutoIncrementRequiresKey))
					}
				}
			}
		}
		if pkCount > 1 {
			errs = append(errs, fmt.Errorf("table %q: %w", t.Name, ErrDuplicatePrimaryKey))
		}
		if autoIncrementCount > 1 {
			errs = append(errs, fmt.Errorf("table %q: %w", t.Name, ErrMultipleAutoIncrement))
		}

		// Index checks.
		idxNames := make(map[string]bool)
		pkColName := ""
		for _, c := range t.Columns {
			if c.PrimaryKey {
				pkColName = c.Name
				break
			}
		}

		for _, idx := range t.Indexes {
			if idx.Name == "" {
				errs = append(errs, fmt.Errorf("table %q: index name must not be empty", t.Name))
				continue
			}
			if pkColName != "" && idx.Name == "pk" {
				errs = append(errs, fmt.Errorf("table %q: duplicate index name %q", t.Name, idx.Name))
			}
			if idxNames[idx.Name] {
				errs = append(errs, fmt.Errorf("table %q: duplicate index name %q", t.Name, idx.Name))
			}
			idxNames[idx.Name] = true

			if len(idx.Columns) == 0 {
				errs = append(errs, fmt.Errorf("table %q index %q: must reference at least one column", t.Name, idx.Name))
				continue
			}
			idxColNames := make(map[string]bool, len(idx.Columns))
			for _, cn := range idx.Columns {
				if idxColNames[cn] {
					errs = append(errs, fmt.Errorf("table %q index %q: duplicate index column %q", t.Name, idx.Name, cn))
				}
				idxColNames[cn] = true
				if !colNames[cn] {
					errs = append(errs, fmt.Errorf("table %q index %q: %w: %q", t.Name, idx.Name, ErrColumnNotFound, cn))
				}
				if pkColName != "" && cn == pkColName {
					errs = append(errs, fmt.Errorf("table %q index %q: PK column %q must not appear in explicit index", t.Name, idx.Name, cn))
				}
			}
		}
	}

	return errs
}

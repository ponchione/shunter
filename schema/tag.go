package schema

import (
	"fmt"
	"strings"
)

// TagDirectives holds the parsed and validated directives from a shunter struct tag.
type TagDirectives struct {
	PrimaryKey    bool
	AutoIncrement bool
	Unique        bool
	Index         bool   // plain single-column index
	IndexName     string // non-empty when index:<name> present
	NameOverride  string // non-empty when name:<col> present
	Exclude       bool   // the "-" directive
}

// ParseTag parses a shunter:"..." tag string into validated TagDirectives.
// Validation is integrated: contradictions, duplicates, and unknown tokens all return errors.
func ParseTag(raw string) (*TagDirectives, error) {
	if raw == "" {
		return &TagDirectives{}, nil
	}

	td := &TagDirectives{}
	seen := make(map[string]bool)
	tokens := strings.Split(raw, ",")

	for _, tok := range tokens {
		// Determine the canonical key for duplicate detection.
		key, val, hasValue := strings.Cut(tok, ":")

		if seen[key] {
			return nil, fmt.Errorf("shunter: duplicate directive %q in tag %q", key, raw)
		}
		seen[key] = true

		switch {
		case tok == "primarykey":
			td.PrimaryKey = true
		case tok == "autoincrement":
			td.AutoIncrement = true
		case tok == "unique":
			td.Unique = true
		case tok == "index":
			td.Index = true
		case (key == "index" || key == "name") && hasValue:
			if val == "" {
				return nil, fmt.Errorf("shunter: empty value in directive %q in tag %q", tok, raw)
			}
			if key == "index" {
				td.IndexName = val
			} else {
				td.NameOverride = val
			}
		case tok == "-":
			td.Exclude = true
		default:
			return nil, fmt.Errorf("shunter: unknown directive %q in tag %q", tok, raw)
		}
	}

	// Validation: exclude must appear alone.
	if td.Exclude && len(tokens) > 1 {
		return nil, fmt.Errorf("shunter: exclude (-) must appear alone in tag %q", raw)
	}

	// Validation: primarykey cannot combine with index directives.
	if td.PrimaryKey && (td.Index || td.IndexName != "") {
		return nil, fmt.Errorf("shunter: primarykey cannot combine with index directives in tag %q", raw)
	}

	// Validation: plain index and named index cannot both appear.
	if td.Index && td.IndexName != "" {
		return nil, fmt.Errorf("shunter: plain index and index:<name> cannot both appear in tag %q", raw)
	}

	// Validation: unique already creates a single-column unique index.
	if td.Unique && td.Index {
		return nil, fmt.Errorf("shunter: unique cannot combine with plain index in tag %q", raw)
	}

	return td, nil
}

// DefaultIndexName returns the default index name for a column given its directives.
func DefaultIndexName(columnName string, isPK bool, isUnique bool) string {
	if isPK {
		return "pk"
	}
	if isUnique {
		return columnName + "_uniq"
	}
	return columnName + "_idx"
}

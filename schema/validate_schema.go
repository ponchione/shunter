package schema

import "fmt"

var reservedTableNames = map[string]bool{
	"sys_clients":   true,
	"sys_scheduled": true,
}

// validateReducerAndSchemaRules checks reducer registrations and top-level schema rules.
func validateReducerAndSchemaRules(b *Builder) []error {
	var errs []error

	// Schema version required.
	if !b.versionSet || b.version == 0 {
		errs = append(errs, ErrSchemaVersionNotSet)
	}

	// At least one user table.
	if len(b.tables) == 0 {
		errs = append(errs, ErrNoTables)
	}

	// Reserved table names.
	for _, t := range b.tables {
		if reservedTableNames[t.Name] {
			errs = append(errs, fmt.Errorf("%w: %q", ErrReservedTableName, t.Name))
		}
	}

	// Reducer name checks.
	for name, entry := range b.reducers {
		if name == "" {
			errs = append(errs, fmt.Errorf("reducer name must not be empty"))
		}
		if entry.count > 1 {
			errs = append(errs, fmt.Errorf("%w: %q", ErrDuplicateReducerName, name))
		}
		if entry.handler == nil {
			errs = append(errs, fmt.Errorf("reducer %q: handler must not be nil", name))
		}
		if name == "OnConnect" || name == "OnDisconnect" {
			errs = append(errs, fmt.Errorf("reducer name %q is reserved for lifecycle hooks", name))
		}
	}

	// Lifecycle handler checks.
	if b.onConnectRegistrations > 1 {
		errs = append(errs, fmt.Errorf("duplicate OnConnect registration"))
	}
	if b.onDisconnectRegistrations > 1 {
		errs = append(errs, fmt.Errorf("duplicate OnDisconnect registration"))
	}
	if b.onConnectRegistrations == 1 && b.onConnect == nil {
		errs = append(errs, fmt.Errorf("OnConnect handler must not be nil"))
	}
	if b.onDisconnectRegistrations == 1 && b.onDisconnect == nil {
		errs = append(errs, fmt.Errorf("OnDisconnect handler must not be nil"))
	}

	return errs
}

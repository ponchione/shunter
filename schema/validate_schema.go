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
			errs = append(errs, fmt.Errorf("%w: reducer %q", ErrNilReducerHandler, name))
		}
		if name == "OnConnect" || name == "OnDisconnect" {
			errs = append(errs, fmt.Errorf("%w: %q", ErrReservedReducerName, name))
		}
	}

	// Lifecycle handler checks.
	if b.onConnectRegistrations > 1 {
		errs = append(errs, fmt.Errorf("%w: OnConnect", ErrDuplicateLifecycleReducer))
	}
	if b.onDisconnectRegistrations > 1 {
		errs = append(errs, fmt.Errorf("%w: OnDisconnect", ErrDuplicateLifecycleReducer))
	}
	if b.onConnectRegistrations == 1 && b.onConnect == nil {
		errs = append(errs, fmt.Errorf("%w: OnConnect", ErrNilReducerHandler))
	}
	if b.onDisconnectRegistrations == 1 && b.onDisconnect == nil {
		errs = append(errs, fmt.Errorf("%w: OnDisconnect", ErrNilReducerHandler))
	}

	return errs
}

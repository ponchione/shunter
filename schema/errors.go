package schema

import "errors"

var (
	ErrEmptyTableName            = errors.New("table name must not be empty")
	ErrInvalidTableName          = errors.New("invalid table name")
	ErrDuplicateTableName        = errors.New("duplicate table name")
	ErrDuplicatePrimaryKey       = errors.New("at most one primarykey column per table")
	ErrAutoIncrementType         = errors.New("autoincrement requires an integer-typed column")
	ErrAutoIncrementRequiresKey  = errors.New("autoincrement requires primarykey or unique")
	ErrSequenceOverflow          = errors.New("autoincrement sequence overflow")
	ErrEmptyColumnName           = errors.New("column name must not be empty")
	ErrInvalidColumnName         = errors.New("invalid column name")
	ErrColumnNotFound            = errors.New("column not found")
	ErrNullableColumn            = errors.New("nullable columns are not supported in v1")
	ErrDuplicateReducerName      = errors.New("duplicate reducer name")
	ErrReservedReducerName       = errors.New("reducer name is reserved for lifecycle hooks")
	ErrNilReducerHandler         = errors.New("reducer handler must not be nil")
	ErrDuplicateLifecycleReducer = errors.New("duplicate lifecycle reducer registration")
	ErrSchemaVersionNotSet       = errors.New("SchemaVersion must be called with a value > 0")
	ErrReservedTableName         = errors.New("table name is reserved for system tables")
	ErrNoTables                  = errors.New("at least one user table must be registered")
	ErrAlreadyBuilt              = errors.New("builder has already been built")
)

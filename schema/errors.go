package schema

import "errors"

var (
	ErrEmptyTableName          = errors.New("table name must not be empty")
	ErrDuplicateTableName      = errors.New("duplicate table name")
	ErrDuplicatePrimaryKey     = errors.New("at most one primarykey column per table")
	ErrAutoIncrementType       = errors.New("autoincrement requires an integer-typed column")
	ErrAutoIncrementRequiresKey = errors.New("autoincrement requires primarykey or unique")
	ErrInvalidColumnName       = errors.New("invalid column name")
	ErrDuplicateReducerName    = errors.New("duplicate reducer name")
	ErrSchemaVersionNotSet     = errors.New("SchemaVersion must be called with a value > 0")
	ErrReservedTableName       = errors.New("table name is reserved for system tables")
	ErrNoTables                = errors.New("at least one user table must be registered")
	ErrAlreadyBuilt            = errors.New("builder has already been built")
)

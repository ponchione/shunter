package schema

import (
	"strings"

	"github.com/ponchione/shunter/types"
)

// ValueKind re-exports the store value kind enum into the schema package so
// schema contracts can talk about column kinds without forcing downstream
// packages to import both schema and types.
type ValueKind = types.ValueKind

// Re-export ValueKind constants for schema-facing APIs.
const (
	KindBool        = types.KindBool
	KindInt8        = types.KindInt8
	KindUint8       = types.KindUint8
	KindInt16       = types.KindInt16
	KindUint16      = types.KindUint16
	KindInt32       = types.KindInt32
	KindUint32      = types.KindUint32
	KindInt64       = types.KindInt64
	KindUint64      = types.KindUint64
	KindFloat32     = types.KindFloat32
	KindFloat64     = types.KindFloat64
	KindString      = types.KindString
	KindBytes       = types.KindBytes
	KindInt128      = types.KindInt128
	KindUint128     = types.KindUint128
	KindInt256      = types.KindInt256
	KindUint256     = types.KindUint256
	KindTimestamp   = types.KindTimestamp
	KindArrayString = types.KindArrayString
	KindUUID        = types.KindUUID
	KindDuration    = types.KindDuration
)

// TableID is a stable identifier for a table, assigned by the builder.
type TableID uint32

// IndexID is a stable identifier for an index, assigned by the builder.
type IndexID uint32

// TableSchema describes a registered table.
type TableSchema struct {
	ID         TableID        `json:"id"`
	Name       string         `json:"name"`
	Columns    []ColumnSchema `json:"columns"`
	Indexes    []IndexSchema  `json:"indexes"`
	ReadPolicy ReadPolicy     `json:"read_policy"`
}

// ColumnSchema describes a single column.
type ColumnSchema struct {
	Index         int       `json:"index"`
	Name          string    `json:"name"`
	Type          ValueKind `json:"type"`
	Nullable      bool      `json:"nullable"`
	AutoIncrement bool      `json:"auto_increment,omitempty"`
}

// IndexSchema describes a table index.
type IndexSchema struct {
	ID      IndexID `json:"id"`
	Name    string  `json:"name"`
	Columns []int   `json:"columns"`
	Unique  bool    `json:"unique"`
	Primary bool    `json:"primary"` // Primary implies Unique.
}

// NewIndexSchema constructs an IndexSchema while preserving the v1 invariant
// that a primary index is always unique.
func NewIndexSchema(id IndexID, name string, columns []int, unique bool, primary bool) IndexSchema {
	if primary {
		unique = true
	}
	return IndexSchema{
		ID:      id,
		Name:    name,
		Columns: columns,
		Unique:  unique,
		Primary: primary,
	}
}

// Column returns the column with the given name, or false if not found.
func (ts *TableSchema) Column(name string) (*ColumnSchema, bool) {
	for i := range ts.Columns {
		if ts.Columns[i].Name == name || strings.EqualFold(ts.Columns[i].Name, name) {
			return &ts.Columns[i], true
		}
	}
	return nil, false
}

// PrimaryIndex returns the primary index, or false if none is declared.
func (ts *TableSchema) PrimaryIndex() (*IndexSchema, bool) {
	for i := range ts.Indexes {
		if ts.Indexes[i].Primary {
			return &ts.Indexes[i], true
		}
	}
	return nil, false
}

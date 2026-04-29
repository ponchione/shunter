package protocol

import (
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

type authorizedSchemaLookup struct {
	base   SchemaLookup
	caller types.CallerContext
}

// NewAuthorizedSchemaLookup wraps base so raw external SQL only resolves
// tables the caller may read.
func NewAuthorizedSchemaLookup(base SchemaLookup, caller types.CallerContext) SchemaLookup {
	if base == nil {
		return nil
	}
	caller.Permissions = append([]string(nil), caller.Permissions...)
	return authorizedSchemaLookup{base: base, caller: caller}
}

func authorizedSchemaLookupForConn(base SchemaLookup, conn *Conn) SchemaLookup {
	return NewAuthorizedSchemaLookup(base, readCallerContext(conn))
}

func readCallerContext(conn *Conn) types.CallerContext {
	if conn == nil {
		return types.CallerContext{}
	}
	return types.CallerContext{
		Identity:            conn.Identity,
		ConnectionID:        conn.ID,
		Permissions:         append([]string(nil), conn.Permissions...),
		AllowAllPermissions: conn.AllowAllPermissions,
	}
}

func (l authorizedSchemaLookup) Table(id schema.TableID) (*schema.TableSchema, bool) {
	ts, ok := l.base.Table(id)
	if !ok || !callerCanReadTable(l.caller, ts) {
		return nil, false
	}
	return ts, true
}

func (l authorizedSchemaLookup) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	id, ts, ok := l.base.TableByName(name)
	if !ok || !callerCanReadTable(l.caller, ts) {
		return 0, nil, false
	}
	return id, ts, true
}

func (l authorizedSchemaLookup) TableExists(table schema.TableID) bool {
	_, ok := l.Table(table)
	return ok
}

func (l authorizedSchemaLookup) TableName(table schema.TableID) string {
	ts, ok := l.Table(table)
	if !ok {
		return ""
	}
	return ts.Name
}

func (l authorizedSchemaLookup) ColumnExists(table schema.TableID, col types.ColID) bool {
	ts, ok := l.Table(table)
	if !ok {
		return false
	}
	return int(col) >= 0 && int(col) < len(ts.Columns)
}

func (l authorizedSchemaLookup) ColumnType(table schema.TableID, col types.ColID) schema.ValueKind {
	ts, ok := l.Table(table)
	if !ok || int(col) < 0 || int(col) >= len(ts.Columns) {
		return 0
	}
	return ts.Columns[col].Type
}

func (l authorizedSchemaLookup) HasIndex(table schema.TableID, col types.ColID) bool {
	if !l.TableExists(table) {
		return false
	}
	return l.base.HasIndex(table, col)
}

func (l authorizedSchemaLookup) ColumnCount(table schema.TableID) int {
	ts, ok := l.Table(table)
	if !ok {
		return 0
	}
	return len(ts.Columns)
}

func (l authorizedSchemaLookup) IndexIDForColumn(table schema.TableID, col types.ColID) (schema.IndexID, bool) {
	if !l.TableExists(table) {
		return 0, false
	}
	resolver, ok := l.base.(schema.IndexResolver)
	if !ok {
		return 0, false
	}
	return resolver.IndexIDForColumn(table, col)
}

func callerCanReadTable(caller types.CallerContext, ts *schema.TableSchema) bool {
	if ts == nil {
		return false
	}
	if caller.AllowAllPermissions {
		return true
	}
	switch ts.ReadPolicy.Access {
	case schema.TableAccessPublic:
		return true
	case schema.TableAccessPermissioned:
		_, missing := types.MissingRequiredPermission(caller, ts.ReadPolicy.Permissions)
		return !missing
	case schema.TableAccessPrivate:
		return false
	default:
		return false
	}
}

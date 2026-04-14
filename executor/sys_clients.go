package executor

import "github.com/ponchione/shunter/schema"

// SysClientsTableName is the fixed name of the system table that tracks
// active client connections (SPEC-003 §10.2).
const SysClientsTableName = "sys_clients"

// Column positions inside a sys_clients row. Match the layout defined in
// `schema/system_tables.go`.
const (
	SysClientsColConnectionID = 0
	SysClientsColIdentity     = 1
	SysClientsColConnectedAt  = 2
)

// SysClientsTable returns the TableSchema for sys_clients from a built
// schema registry. Every `schema.Build` call registers the system table, so
// a false return indicates the registry was constructed by a path that
// skipped `registerSystemTables` — treat it as a programming error by the
// caller.
func SysClientsTable(reg schema.SchemaRegistry) (*schema.TableSchema, bool) {
	return reg.TableByName(SysClientsTableName)
}

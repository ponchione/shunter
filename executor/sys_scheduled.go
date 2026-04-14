package executor

import "github.com/ponchione/shunter/schema"

// SysScheduledTableName is the fixed name of the system table that stores
// pending scheduled-reducer rows (SPEC-003 §9.2).
const SysScheduledTableName = "sys_scheduled"

// Column positions inside a sys_scheduled row. Match the layout defined in
// `schema/system_tables.go`.
const (
	SysScheduledColScheduleID  = 0
	SysScheduledColReducerName = 1
	SysScheduledColArgs        = 2
	SysScheduledColNextRunAtNs = 3
	SysScheduledColRepeatNs    = 4
)

// SysScheduledTable returns the TableSchema for sys_scheduled from a built
// schema registry. Every `schema.Build` call registers the system table, so
// a false return indicates the registry was constructed by a path that
// skipped `registerSystemTables` — treat it as a programming error by the
// caller.
func SysScheduledTable(reg schema.SchemaRegistry) (*schema.TableSchema, bool) {
	return reg.TableByName(SysScheduledTableName)
}

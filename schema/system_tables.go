package schema

// registerSystemTables appends sys_clients and sys_scheduled to the builder.
func registerSystemTables(b *Builder) {
	b.TableDef(TableDefinition{
		Name: "sys_clients",
		Columns: []ColumnDefinition{
			{Name: "connection_id", Type: KindBytes, PrimaryKey: true},
			{Name: "identity", Type: KindBytes},
			{Name: "connected_at", Type: KindInt64},
		},
	})

	b.TableDef(TableDefinition{
		Name: "sys_scheduled",
		Columns: []ColumnDefinition{
			{Name: "schedule_id", Type: KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "reducer_name", Type: KindString},
			{Name: "args", Type: KindBytes},
			{Name: "next_run_at_ns", Type: KindInt64},
			{Name: "repeat_ns", Type: KindInt64},
		},
	})
}

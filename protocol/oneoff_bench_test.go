package protocol

import (
	"context"
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func BenchmarkExecuteCompiledSQLQueryCommonPaths(b *testing.B) {
	sl := newMockSchema("tasks", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint64},
		schema.ColumnSchema{Index: 1, Name: "status", Type: schema.KindString},
		schema.ColumnSchema{Index: 2, Name: "owner", Type: schema.KindString},
		schema.ColumnSchema{Index: 3, Name: "points", Type: schema.KindUint32},
	)
	rows := make([]types.ProductValue, 1024)
	for i := range rows {
		status := "closed"
		if i%3 == 0 {
			status = "open"
		}
		owner := fmt.Sprintf("owner-%02d", i%32)
		rows[i] = types.ProductValue{
			types.NewUint64(uint64(i + 1)),
			types.NewString(status),
			types.NewString(owner),
			types.NewUint32(uint32(i % 100)),
		}
	}
	state := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{1: rows}}}
	opts := SQLQueryValidationOptions{
		AllowLimit:      true,
		AllowProjection: true,
		AllowOrderBy:    true,
		AllowOffset:     true,
	}

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{
			name: "filter_limit",
			sql:  "SELECT * FROM tasks WHERE status = 'open' LIMIT 32",
		},
		{
			name: "projection_order_limit",
			sql:  "SELECT id, owner, points FROM tasks ORDER BY points DESC, id ASC LIMIT 32",
		},
		{
			name: "count_filter",
			sql:  "SELECT COUNT(*) AS n FROM tasks WHERE status = 'open'",
		},
		{
			name: "sum_filter",
			sql:  "SELECT SUM(points) AS total FROM tasks WHERE status = 'open'",
		},
	} {
		b.Run(tc.name, func(b *testing.B) {
			compiled, err := CompileSQLQueryString(tc.sql, sl, nil, opts)
			if err != nil {
				b.Fatalf("CompileSQLQueryString: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := ExecuteCompiledSQLQuery(context.Background(), compiled, state, sl)
				if err != nil {
					b.Fatalf("ExecuteCompiledSQLQuery: %v", err)
				}
				if len(result.Rows) == 0 {
					b.Fatal("ExecuteCompiledSQLQuery returned no rows")
				}
			}
		})
	}
}

func BenchmarkExecuteCompiledSQLQueryJoinReadShapes(b *testing.B) {
	sl, state := benchmarkReadSurfaceSchemaAndState()
	opts := SQLQueryValidationOptions{
		AllowLimit:      true,
		AllowProjection: true,
		AllowOrderBy:    true,
		AllowOffset:     true,
	}
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{
			name: "two_table_join_projection_order_limit",
			sql:  "SELECT o.id, u.name, o.total FROM orders o JOIN users u ON o.user_id = u.id WHERE u.active = TRUE ORDER BY o.total DESC LIMIT 64",
		},
		{
			name: "multi_way_join_count",
			sql:  "SELECT COUNT(*) AS n FROM orders o JOIN users u ON o.user_id = u.id JOIN teams team ON u.team_id = team.id WHERE team.active = TRUE",
		},
		{
			name: "multi_way_join_sum",
			sql:  "SELECT SUM(o.total) AS total FROM orders o JOIN users u ON o.user_id = u.id JOIN teams team ON u.team_id = team.id WHERE team.active = TRUE",
		},
	} {
		b.Run(tc.name, func(b *testing.B) {
			compiled, err := CompileSQLQueryString(tc.sql, sl, nil, opts)
			if err != nil {
				b.Fatalf("CompileSQLQueryString: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := ExecuteCompiledSQLQuery(context.Background(), compiled, state, sl)
				if err != nil {
					b.Fatalf("ExecuteCompiledSQLQuery: %v", err)
				}
				if len(result.Rows) == 0 {
					b.Fatal("ExecuteCompiledSQLQuery returned no rows")
				}
			}
		})
	}
}

func BenchmarkHandleSubscribeSingleAdmissionReadShapes(b *testing.B) {
	sl, _ := benchmarkReadSurfaceSchemaAndState()
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{
			name: "single_table_filter",
			sql:  "SELECT * FROM orders WHERE user_id = 17",
		},
		{
			name: "two_table_join",
			sql:  "SELECT o.* FROM orders o JOIN users u ON o.user_id = u.id WHERE u.active = TRUE",
		},
		{
			name: "multi_way_join",
			sql:  "SELECT o.* FROM orders o JOIN users u ON o.user_id = u.id JOIN teams team ON u.team_id = team.id WHERE team.active = TRUE",
		},
	} {
		b.Run(tc.name, func(b *testing.B) {
			executor := &mockSubExecutor{}
			conn := testConnDirect(nil)
			msg := &SubscribeSingleMsg{
				RequestID:   10,
				QueryID:     20,
				QueryString: tc.sql,
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				msg.QueryID = uint32(20 + i)
				handleSubscribeSingle(context.Background(), conn, msg, executor, sl)
				if req := executor.getRegisterSetReq(); req == nil {
					b.Fatal("executor did not receive RegisterSubscriptionSet")
				}
			}
		})
	}
}

func benchmarkReadSurfaceSchemaAndState() (SchemaLookup, CommittedStateAccess) {
	userColumns := []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint64},
		{Index: 1, Name: "team_id", Type: schema.KindUint64},
		{Index: 2, Name: "active", Type: schema.KindBool},
		{Index: 3, Name: "name", Type: schema.KindString},
	}
	teamColumns := []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint64},
		{Index: 1, Name: "active", Type: schema.KindBool},
	}
	orderColumns := []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: schema.KindUint64},
		{Index: 1, Name: "user_id", Type: schema.KindUint64},
		{Index: 2, Name: "total", Type: schema.KindUint64},
	}
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"users": {
			id: 1,
			schema: &schema.TableSchema{
				ID:      1,
				Name:    "users",
				Columns: userColumns,
				Indexes: []schema.IndexSchema{
					schema.NewIndexSchema(10, "users_id", []int{0}, true, true),
					schema.NewIndexSchema(11, "users_team_id", []int{1}, false, false),
				},
			},
		},
		"teams": {
			id: 2,
			schema: &schema.TableSchema{
				ID:      2,
				Name:    "teams",
				Columns: teamColumns,
				Indexes: []schema.IndexSchema{
					schema.NewIndexSchema(20, "teams_id", []int{0}, true, true),
					schema.NewIndexSchema(21, "teams_active", []int{1}, false, false),
				},
			},
		},
		"orders": {
			id: 3,
			schema: &schema.TableSchema{
				ID:      3,
				Name:    "orders",
				Columns: orderColumns,
				Indexes: []schema.IndexSchema{
					schema.NewIndexSchema(30, "orders_id", []int{0}, true, true),
					schema.NewIndexSchema(31, "orders_user_id", []int{1}, false, false),
				},
			},
		},
	}}
	users := make([]types.ProductValue, 256)
	for i := range users {
		teamID := uint64(i%32 + 1)
		users[i] = types.ProductValue{
			types.NewUint64(uint64(i + 1)),
			types.NewUint64(teamID),
			types.NewBool(i%5 != 0),
			types.NewString(fmt.Sprintf("user-%03d", i)),
		}
	}
	teams := make([]types.ProductValue, 32)
	for i := range teams {
		teams[i] = types.ProductValue{
			types.NewUint64(uint64(i + 1)),
			types.NewBool(i%4 != 0),
		}
	}
	orders := make([]types.ProductValue, 1024)
	for i := range orders {
		orders[i] = types.ProductValue{
			types.NewUint64(uint64(i + 1)),
			types.NewUint64(uint64(i%len(users) + 1)),
			types.NewUint64(uint64((i%97 + 1) * 10)),
		}
	}
	state := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: users,
		2: teams,
		3: orders,
	}}}
	return sl, state
}

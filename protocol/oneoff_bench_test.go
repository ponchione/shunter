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

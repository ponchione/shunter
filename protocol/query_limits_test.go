package protocol

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestNormalizeSQLQueryLimits(t *testing.T) {
	tests := []struct {
		name    string
		limits  SQLQueryLimits
		want    SQLQueryLimits
		wantErr bool
	}{
		{
			name: "defaults",
			want: SQLQueryLimits{MaxRows: DefaultSQLQueryMaxRows, MaxBytes: DefaultSQLQueryMaxBytes},
		},
		{
			name:   "explicit",
			limits: SQLQueryLimits{MaxRows: 12, MaxBytes: 34},
			want:   SQLQueryLimits{MaxRows: 12, MaxBytes: 34},
		},
		{name: "negative rows", limits: SQLQueryLimits{MaxRows: -1}, wantErr: true},
		{name: "negative bytes", limits: SQLQueryLimits{MaxBytes: -1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeSQLQueryLimits(tt.limits)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeSQLQueryLimits() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("NormalizeSQLQueryLimits() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestExecuteCompiledSQLQueryMatchesUpperUint64Literal(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   1,
		Name: "items",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint64},
		},
	}
	sl := newMockSchema("items", ts.ID, ts.Columns...)
	state := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ts.ID: {{types.NewUint64(18446744073709551615)}},
	}}}
	compiled, err := CompileSQLQueryString(
		"SELECT * FROM items WHERE id = 18446744073709551615",
		sl,
		nil,
		SQLQueryValidationOptions{},
	)
	if err != nil {
		t.Fatalf("CompileSQLQueryString: %v", err)
	}
	result, err := ExecuteCompiledSQLQueryWithLimits(
		context.Background(),
		compiled,
		state,
		sl,
		SQLQueryLimits{MaxRows: 1, MaxBytes: 1024},
	)
	if err != nil {
		t.Fatalf("ExecuteCompiledSQLQueryWithLimits: %v", err)
	}
	assertProductRowsEqual(t, result.Rows, []types.ProductValue{{types.NewUint64(18446744073709551615)}})
}

func TestExecuteCompiledSQLQueryWithLimits(t *testing.T) {
	ts := &schema.TableSchema{
		ID:   1,
		Name: "items",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
	sl := newMockSchema("items", ts.ID, ts.Columns...)
	state := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		ts.ID: {
			{types.NewUint32(5), types.NewString("five")},
			{types.NewUint32(4), types.NewString("four")},
			{types.NewUint32(3), types.NewString("three")},
			{types.NewUint32(2), types.NewString("two")},
			{types.NewUint32(1), types.NewString("one")},
		},
	}}}
	opts := SQLQueryValidationOptions{
		AllowLimit:      true,
		AllowProjection: true,
		AllowOrderBy:    true,
		AllowOffset:     true,
	}
	compile := func(t *testing.T, sql string) CompiledSQLQuery {
		t.Helper()
		compiled, err := CompileSQLQueryString(sql, sl, nil, opts)
		if err != nil {
			t.Fatalf("CompileSQLQueryString: %v", err)
		}
		return compiled
	}
	execute := func(t *testing.T, sql string, limits SQLQueryLimits) (SQLQueryResult, error) {
		t.Helper()
		return ExecuteCompiledSQLQueryWithLimits(context.Background(), compile(t, sql), state, sl, limits)
	}

	t.Run("row cap rejects unbounded result", func(t *testing.T) {
		_, err := execute(t, "SELECT * FROM items", SQLQueryLimits{MaxRows: 2, MaxBytes: 1 << 20})
		if !errors.Is(err, ErrSQLQueryResultLimit) {
			t.Fatalf("error = %v, want ErrSQLQueryResultLimit", err)
		}
	})

	t.Run("client limit within cap succeeds", func(t *testing.T) {
		result, err := execute(t, "SELECT * FROM items LIMIT 2", SQLQueryLimits{MaxRows: 2, MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("ExecuteCompiledSQLQueryWithLimits: %v", err)
		}
		if len(result.Rows) != 2 {
			t.Fatalf("row count = %d, want 2", len(result.Rows))
		}
	})

	t.Run("offset beyond cap remains a result-only limit", func(t *testing.T) {
		result, err := execute(t, "SELECT * FROM items LIMIT 1 OFFSET 3", SQLQueryLimits{MaxRows: 2, MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("ExecuteCompiledSQLQueryWithLimits: %v", err)
		}
		assertProductRowsEqual(t, result.Rows, []types.ProductValue{{types.NewUint32(2), types.NewString("two")}})
	})

	t.Run("offset at and above boundary", func(t *testing.T) {
		for _, offset := range []int{2, 3} {
			result, err := execute(t, fmt.Sprintf("SELECT * FROM items LIMIT 1 OFFSET %d", offset), SQLQueryLimits{MaxRows: 2, MaxBytes: 1 << 20})
			if err != nil {
				t.Fatalf("offset %d: ExecuteCompiledSQLQueryWithLimits: %v", offset, err)
			}
			if len(result.Rows) != 1 {
				t.Fatalf("offset %d: row count = %d, want 1", offset, len(result.Rows))
			}
		}
	})

	t.Run("limit zero accepts large offset without rows", func(t *testing.T) {
		result, err := execute(t, "SELECT * FROM items LIMIT 0 OFFSET 18446744073709551615", SQLQueryLimits{MaxRows: 2, MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("ExecuteCompiledSQLQueryWithLimits: %v", err)
		}
		if len(result.Rows) != 0 {
			t.Fatalf("row count = %d, want 0", len(result.Rows))
		}
	})

	t.Run("encoded byte cap is enforced", func(t *testing.T) {
		_, err := execute(t, "SELECT * FROM items LIMIT 1", SQLQueryLimits{MaxRows: 10, MaxBytes: 4})
		if !errors.Is(err, ErrSQLQueryResultLimit) {
			t.Fatalf("error = %v, want ErrSQLQueryResultLimit", err)
		}
	})

	t.Run("bounded order retains global best rows", func(t *testing.T) {
		result, err := execute(t, "SELECT id FROM items ORDER BY id ASC LIMIT 2", SQLQueryLimits{MaxRows: 2, MaxBytes: 1 << 20})
		if err != nil {
			t.Fatalf("ExecuteCompiledSQLQueryWithLimits: %v", err)
		}
		assertProductRowsEqual(t, result.Rows, []types.ProductValue{
			{types.NewUint32(1)},
			{types.NewUint32(2)},
		})
	})
}

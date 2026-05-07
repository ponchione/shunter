package queryplan

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/query/sql"
)

func TestBuildCapturesDerivedMetadata(t *testing.T) {
	plan, err := Build("SELECT * FROM users WHERE TRUE AND id = :sender ORDER BY id LIMIT 2 OFFSET 1", Options{
		AllowLimit:   true,
		AllowOrderBy: true,
		AllowOffset:  true,
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if plan.Statement.Table != "users" {
		t.Fatalf("Statement.Table = %q, want users", plan.Statement.Table)
	}
	if len(plan.OrderBy) != 1 || plan.OrderBy[0].Column != "id" {
		t.Fatalf("OrderBy = %+v, want id term", plan.OrderBy)
	}
	cmp, ok := plan.NormalizedPredicate.(sql.ComparisonPredicate)
	if !ok {
		t.Fatalf("NormalizedPredicate = %T, want ComparisonPredicate", plan.NormalizedPredicate)
	}
	if cmp.Filter.Literal.Kind != sql.LitSender {
		t.Fatalf("NormalizedPredicate literal kind = %v, want LitSender", cmp.Filter.Literal.Kind)
	}
	if !plan.UsesCallerIdentity {
		t.Fatal("UsesCallerIdentity = false, want true")
	}
}

func TestBuildFeatureGatesReturnUnsupportedFeature(t *testing.T) {
	cases := []struct {
		name  string
		query string
		opts  Options
	}{
		{
			name:  "limit",
			query: "SELECT * FROM users LIMIT 1",
			opts:  Options{},
		},
		{
			name:  "order_by",
			query: "SELECT * FROM users ORDER BY id",
			opts:  Options{AllowLimit: true, AllowOffset: true},
		},
		{
			name:  "offset",
			query: "SELECT * FROM users OFFSET 1",
			opts:  Options{AllowLimit: true, AllowOrderBy: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build(tc.query, tc.opts)
			if err == nil {
				t.Fatal("Build error = nil, want UnsupportedFeatureError")
			}
			var featureErr sql.UnsupportedFeatureError
			if !errors.As(err, &featureErr) {
				t.Fatalf("Build error = %T (%v), want UnsupportedFeatureError", err, err)
			}
			if featureErr.SQL != tc.query {
				t.Fatalf("UnsupportedFeatureError.SQL = %q, want %q", featureErr.SQL, tc.query)
			}
		})
	}
}

func TestV1QueryPlanSupportedReadMatrixShapes(t *testing.T) {
	opts := Options{
		AllowLimit:   true,
		AllowOrderBy: true,
		AllowOffset:  true,
	}
	cases := []struct {
		name                  string
		query                 string
		wantProjectionColumns int
		wantAggregate         bool
		wantJoins             int
		wantOrderBy           int
		wantLimit             bool
		wantOffset            bool
		wantCallerIdentity    bool
	}{
		{
			name:  "select_all",
			query: "SELECT * FROM tasks",
		},
		{
			name:  "qualified_star_alias",
			query: "SELECT task.* FROM tasks AS task",
		},
		{
			name:                  "explicit_projection_alias",
			query:                 "SELECT id AS task_id, owner FROM tasks",
			wantProjectionColumns: 2,
		},
		{
			name:               "sender_nullable_boolean_predicates",
			query:              "SELECT * FROM tasks WHERE owner = :sender AND deleted_at IS NULL AND active = TRUE",
			wantCallerIdentity: true,
		},
		{
			name:                  "multi_column_order_limit_offset",
			query:                 "SELECT id, owner FROM tasks WHERE active = TRUE ORDER BY owner ASC, id DESC LIMIT 10 OFFSET 5",
			wantProjectionColumns: 2,
			wantOrderBy:           2,
			wantLimit:             true,
			wantOffset:            true,
		},
		{
			name:      "inner_join_table_shape",
			query:     "SELECT o.* FROM orders o JOIN users u ON o.user_id = u.id WHERE u.active = TRUE",
			wantJoins: 1,
		},
		{
			name:      "multi_way_inner_join",
			query:     "SELECT o.* FROM orders o JOIN users u ON o.user_id = u.id JOIN teams team ON u.team_id = team.id WHERE team.active = TRUE",
			wantJoins: 2,
		},
		{
			name:      "cross_join_with_filter",
			query:     "SELECT t.* FROM t CROSS JOIN s WHERE t.id = s.id",
			wantJoins: 1,
		},
		{
			name:          "count_star",
			query:         "SELECT COUNT(*) AS n FROM tasks",
			wantAggregate: true,
		},
		{
			name:          "count_distinct",
			query:         "SELECT COUNT(DISTINCT owner) AS owners FROM tasks",
			wantAggregate: true,
		},
		{
			name:          "sum_numeric",
			query:         "SELECT SUM(points) AS total FROM tasks",
			wantAggregate: true,
		},
		{
			name:          "join_count",
			query:         "SELECT COUNT(*) AS n FROM orders o JOIN users u ON o.user_id = u.id",
			wantAggregate: true,
			wantJoins:     1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := Build(tc.query, opts)
			if err != nil {
				t.Fatalf("Build(%q) error = %v", tc.query, err)
			}
			if plan.Statement.Table == "" {
				t.Fatalf("Statement.Table = empty for %q", tc.query)
			}
			if got := len(plan.Statement.ProjectionColumns); got != tc.wantProjectionColumns {
				t.Fatalf("ProjectionColumns len = %d, want %d", got, tc.wantProjectionColumns)
			}
			if got := plan.Statement.Aggregate != nil; got != tc.wantAggregate {
				t.Fatalf("Aggregate present = %v, want %v", got, tc.wantAggregate)
			}
			if got := len(plan.Statement.Joins); got != tc.wantJoins {
				t.Fatalf("Joins len = %d, want %d", got, tc.wantJoins)
			}
			if got := len(plan.OrderBy); got != tc.wantOrderBy {
				t.Fatalf("OrderBy len = %d, want %d", got, tc.wantOrderBy)
			}
			if got := plan.Statement.HasLimit; got != tc.wantLimit {
				t.Fatalf("HasLimit = %v, want %v", got, tc.wantLimit)
			}
			if got := plan.Statement.HasOffset; got != tc.wantOffset {
				t.Fatalf("HasOffset = %v, want %v", got, tc.wantOffset)
			}
			if plan.UsesCallerIdentity != tc.wantCallerIdentity {
				t.Fatalf("UsesCallerIdentity = %v, want %v", plan.UsesCallerIdentity, tc.wantCallerIdentity)
			}
		})
	}
}

func TestV1QueryPlanRejectedNonGoalMatrixShapes(t *testing.T) {
	opts := Options{
		AllowLimit:   true,
		AllowOrderBy: true,
		AllowOffset:  true,
	}
	cases := []struct {
		name  string
		query string
	}{
		{name: "insert", query: "INSERT INTO t (id) VALUES (1)"},
		{name: "update", query: "UPDATE t SET id = 2"},
		{name: "delete", query: "DELETE FROM t"},
		{name: "group_by", query: "SELECT id, COUNT(*) FROM t GROUP BY id"},
		{name: "having", query: "SELECT COUNT(id) AS n FROM t HAVING n > 0"},
		{name: "scalar_function", query: "SELECT LOWER(body) FROM t"},
		{name: "arithmetic_expression", query: "SELECT * FROM t WHERE id + 1 = 2"},
		{name: "subquery", query: "SELECT * FROM (SELECT * FROM t) AS nested"},
		{name: "union", query: "SELECT * FROM t UNION SELECT * FROM s"},
		{name: "intersect", query: "SELECT * FROM t INTERSECT SELECT * FROM s"},
		{name: "except", query: "SELECT * FROM t EXCEPT SELECT * FROM s"},
		{name: "outer_join", query: "SELECT t.* FROM t LEFT OUTER JOIN s ON t.id = s.id"},
		{name: "natural_join", query: "SELECT t.* FROM t NATURAL JOIN s"},
		{name: "recursive_query", query: "WITH RECURSIVE r AS (SELECT * FROM t) SELECT * FROM r"},
		{name: "json_path_operator", query: "SELECT * FROM t WHERE metadata->'kind' = 'task'"},
		{name: "full_text_search", query: "SELECT * FROM t WHERE body MATCH 'needle'"},
		{name: "transaction_control_begin", query: "BEGIN"},
		{name: "transaction_control_commit", query: "COMMIT"},
		{name: "transaction_control_rollback", query: "ROLLBACK"},
		{name: "set", query: "SET search_path = public"},
		{name: "show", query: "SHOW TABLES"},
		{name: "procedure_call", query: "CALL refresh_tasks()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build(tc.query, opts)
			if !errors.Is(err, sql.ErrUnsupportedSQL) {
				t.Fatalf("Build(%q) err = %v, want ErrUnsupportedSQL", tc.query, err)
			}
		})
	}
}

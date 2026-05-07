package sql

import (
	"errors"
	"testing"
)

func TestV1UnsupportedSQLNonGoalsRejected(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{name: "insert", sql: "INSERT INTO t (id) VALUES (1)"},
		{name: "update", sql: "UPDATE t SET id = 2"},
		{name: "delete", sql: "DELETE FROM t"},
		{name: "group_by", sql: "SELECT id, COUNT(*) FROM t GROUP BY id"},
		{name: "having", sql: "SELECT COUNT(id) AS n FROM t HAVING n > 0"},
		{name: "scalar_function", sql: "SELECT LOWER(body) FROM t"},
		{name: "arithmetic_expression", sql: "SELECT * FROM t WHERE id + 1 = 2"},
		{name: "subquery", sql: "SELECT * FROM (SELECT * FROM t) AS nested"},
		{name: "union", sql: "SELECT * FROM t UNION SELECT * FROM s"},
		{name: "intersect", sql: "SELECT * FROM t INTERSECT SELECT * FROM s"},
		{name: "except", sql: "SELECT * FROM t EXCEPT SELECT * FROM s"},
		{name: "outer_join", sql: "SELECT t.* FROM t LEFT OUTER JOIN s ON t.id = s.id"},
		{name: "natural_join", sql: "SELECT t.* FROM t NATURAL JOIN s"},
		{name: "recursive_query", sql: "WITH RECURSIVE r AS (SELECT * FROM t) SELECT * FROM r"},
		{name: "json_path_operator", sql: "SELECT * FROM t WHERE metadata->'kind' = 'task'"},
		{name: "full_text_search", sql: "SELECT * FROM t WHERE body MATCH 'needle'"},
		{name: "transaction_control_begin", sql: "BEGIN"},
		{name: "transaction_control_commit", sql: "COMMIT"},
		{name: "transaction_control_rollback", sql: "ROLLBACK"},
		{name: "set", sql: "SET search_path = public"},
		{name: "show", sql: "SHOW TABLES"},
		{name: "procedure_call", sql: "CALL refresh_tasks()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.sql)
			if !errors.Is(err, ErrUnsupportedSQL) {
				t.Fatalf("Parse(%q) err = %v, want ErrUnsupportedSQL", tc.sql, err)
			}
		})
	}
}

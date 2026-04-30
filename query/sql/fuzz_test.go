package sql

import (
	"errors"
	"reflect"
	"testing"
)

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"",
		"SELECT * FROM players",
		"select * from players where id = 1",
		`SELECT "users".* FROM "users" WHERE "users"."name" = 'ada'`,
		"SELECT COUNT(*) AS n FROM players WHERE active = TRUE LIMIT 10",
		"SELECT p.id, team.name FROM players AS p JOIN teams AS team ON p.team_id = team.id WHERE team.active = TRUE",
		"SELECT * FROM t WHERE bytes = 0xDEADBEEF",
		"SELECT * FROM t WHERE id = :sender",
		"SELECT * FROM t WHERE id = 12abc",
		"SELECT * FROM t WHERE name = 'unterminated",
		"SELECT * FROM t WHERE c = 1e999999999",
		"SELECT * FROM t INNER",
		"SELECT * FROM t LEFT JOIN s ON t.id = s.id",
	} {
		f.Add(seed)
	}

	const maxSQLFuzzBytes = 8 << 10
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > maxSQLFuzzBytes {
			t.Skip("SQL fuzz input above bounded local limit")
		}

		stmt, err := Parse(input)
		if err != nil {
			if !errors.Is(err, ErrUnsupportedSQL) {
				t.Fatalf("Parse(%q) error = %v, want ErrUnsupportedSQL category", input, err)
			}
			return
		}
		if stmt.Table == "" {
			t.Fatalf("Parse(%q) accepted empty table statement: %+v", input, stmt)
		}

		again, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q) accepted once then failed: %v", input, err)
		}
		if !reflect.DeepEqual(again, stmt) {
			t.Fatalf("Parse(%q) is not deterministic: first=%#v second=%#v", input, stmt, again)
		}
	})
}

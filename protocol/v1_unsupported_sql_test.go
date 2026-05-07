package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestV1UnsupportedSQLNonGoalsReturnProtocolErrors(t *testing.T) {
	for i, tc := range v1UnsupportedSQLNonGoalProtocolCases() {
		t.Run(tc.name+"/one_off", func(t *testing.T) {
			conn := testConnDirect(nil)
			msg := &OneOffQueryMsg{
				MessageID:   []byte{0xA0, byte(i)},
				QueryString: tc.sql,
			}
			stateAccess := &mockStateAccess{snap: &mockSnapshot{
				rows: map[schema.TableID][]types.ProductValue{},
			}}

			handleOneOffQuery(context.Background(), conn, msg, stateAccess, v1UnsupportedSQLSchemaLookup(t))

			result := drainOneOff(t, conn)
			if result.Error == nil || *result.Error == "" {
				t.Fatalf("OneOff error = %v, want non-empty validation error", result.Error)
			}
			if len(result.Tables) != 0 {
				t.Fatalf("OneOff tables = %d, want 0 on unsupported SQL", len(result.Tables))
			}
		})

		t.Run(tc.name+"/subscribe_single", func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			requestID := uint32(900 + i*2)
			queryID := requestID + 1
			msg := &SubscribeSingleMsg{
				RequestID:   requestID,
				QueryID:     queryID,
				QueryString: tc.sql,
			}

			handleSubscribeSingle(context.Background(), conn, msg, executor, v1UnsupportedSQLSchemaLookup(t))

			tag, decoded := drainServerMsgEventually(t, conn)
			if tag != TagSubscriptionError {
				t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
			}
			se := decoded.(SubscriptionError)
			requireOptionalUint32(t, se.RequestID, requestID, "SubscriptionError.RequestID")
			requireOptionalUint32(t, se.QueryID, queryID, "SubscriptionError.QueryID")
			if se.Error == "" {
				t.Fatal("SubscriptionError.Error = empty, want validation error")
			}
			if req := executor.getRegisterSetReq(); req != nil {
				t.Fatal("executor should not be called for unsupported SQL")
			}
		})
	}
}

func v1UnsupportedSQLNonGoalProtocolCases() []struct {
	name string
	sql  string
} {
	return []struct {
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
}

func v1UnsupportedSQLSchemaLookup(t *testing.T) SchemaLookup {
	t.Helper()
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "t",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
			{Name: "body", Type: schema.KindString},
			{Name: "metadata", Type: schema.KindJSON},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "s",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32},
			{Name: "body", Type: schema.KindString},
			{Name: "metadata", Type: schema.KindJSON},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema: %v", err)
	}
	return registrySchemaLookup{reg: eng.Registry()}
}

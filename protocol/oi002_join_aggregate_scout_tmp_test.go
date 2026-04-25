package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestOI002JoinAggregateScout_SubscribeJoinAggregateGuardYieldsToWhereResolution(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"t": {id: 1, schema: &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
		}}},
		"s": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
		}}},
	}}

	const sqlText = "SELECT COUNT(*) AS n FROM t JOIN s ON t.id = s.id WHERE s.where_missing = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   720,
		QueryID:     721,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "`where_missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
}

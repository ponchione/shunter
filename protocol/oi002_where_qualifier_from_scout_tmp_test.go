package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOI002WhereQualifierFromScout_FromResolutionPrecedesWhereQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT * FROM missing WHERE x.id = 1"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF4},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	const want = "no such table: `missing`. If the table exists, it may be marked private."
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestOI002WhereQualifierFromScout_SubscribeFromResolutionPrecedesWhereQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	const sqlText = "SELECT * FROM missing WHERE x.id = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   738,
		QueryID:     739,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `missing`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
}

func TestOI002WhereQualifierFromScout_JoinRightTableResolutionPrecedesWhereQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT t.* FROM t JOIN missing ON t.id = missing.id WHERE x.id = 1"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF5},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	const want = "no such table: `missing`. If the table exists, it may be marked private."
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestOI002WhereQualifierFromScout_SubscribeJoinRightTableResolutionPrecedesWhereQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	const sqlText = "SELECT t.* FROM t JOIN missing ON t.id = missing.id WHERE x.id = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   740,
		QueryID:     741,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `missing`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
}

func TestOI002WhereQualifierFromScout_JoinOnResolutionPrecedesWhereQualifier(t *testing.T) {
	conn := testConnDirect(nil)
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
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT t.* FROM t JOIN s ON t.missing = s.id WHERE x.id = 1"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF6},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	const want = "`missing` is not in scope"
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestOI002WhereQualifierFromScout_SubscribeJoinOnResolutionPrecedesWhereQualifier(t *testing.T) {
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

	const sqlText = "SELECT t.* FROM t JOIN s ON t.missing = s.id WHERE x.id = 1"
	msg := &SubscribeSingleMsg{
		RequestID:   742,
		QueryID:     743,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "`missing` is not in scope, executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
}

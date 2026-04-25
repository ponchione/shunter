package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOI002LimitScout_MissingTablePrecedesInvalidLimitLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT * FROM missing_table LIMIT 1.5"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE0},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	const want = "no such table: `missing_table`. If the table exists, it may be marked private."
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestOI002LimitScout_ProjectionErrorPrecedesInvalidLimitLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT missing FROM t LIMIT 1.5"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE1},
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

func TestOI002LimitScout_WhereErrorPrecedesInvalidLimitLiteral(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT * FROM t WHERE missing = 1 LIMIT 1.5"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE3},
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

func TestOI002LimitScout_JoinOnErrorPrecedesInvalidLimitLiteral(t *testing.T) {
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

	const sqlText = "SELECT t.* FROM t JOIN s ON t.missing = s.id LIMIT 1.5"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE4},
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

func TestOI002LimitScout_LeadingPlusLimitRejectedByReferenceParser(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {{types.NewUint32(7)}},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT * FROM t LIMIT +1"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE2},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	want := "Unsupported: " + sqlText
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

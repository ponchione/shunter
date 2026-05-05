package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOI002LimitOrdering_MissingTablePrecedesInvalidLimitLiteral(t *testing.T) {
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

	const want = "no such table: `missing_table`. If the table exists, it may be marked private."
	requireOneOffError(t, conn, want)
}

func TestOI002LimitOrdering_ProjectionErrorPrecedesInvalidLimitLiteral(t *testing.T) {
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

	const want = "`missing` is not in scope"
	requireOneOffError(t, conn, want)
}

func TestOI002LimitOrdering_WhereErrorPrecedesInvalidLimitLiteral(t *testing.T) {
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

	const want = "`missing` is not in scope"
	requireOneOffError(t, conn, want)
}

func TestOI002LimitOrdering_JoinOnErrorPrecedesInvalidLimitLiteral(t *testing.T) {
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

	const want = "`missing` is not in scope"
	requireOneOffError(t, conn, want)
}

func TestOI002LimitOrdering_LeadingPlusLimitRejectedByReferenceParser(t *testing.T) {
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

	want := "Unsupported: " + sqlText
	requireOneOffError(t, conn, want)
}

func TestOI002LimitOrdering_NegativeLimitRejectedByReferenceParser(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {{types.NewUint32(7)}},
	}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT * FROM t LIMIT -1"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE5},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	want := "Unsupported: " + sqlText
	requireOneOffError(t, conn, want)
}

func TestOI002LimitOrdering_SignedLimitRejectedBeforeMissingTable(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT * FROM missing LIMIT +1"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE6},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	want := "Unsupported: " + sqlText
	requireOneOffError(t, conn, want)
}

func TestOI002LimitOrdering_NonNumericLimitRejectedBeforeProjection(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT missing FROM t LIMIT '5'"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xE7},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	want := "Unsupported: " + sqlText
	requireOneOffError(t, conn, want)
}

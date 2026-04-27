package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func requireOneOffError(t *testing.T, conn *Conn, want string) {
	t.Helper()
	result := drainOneOff(t, conn)
	if result.Error == nil {
		t.Fatalf("Error = nil, want %q", want)
	}
	if *result.Error != want {
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func requireNoSubscriptionRegistration(t *testing.T, executor *mockSubExecutor) {
	t.Helper()
	if req := executor.getRegisterSetReq(); req != nil {
		t.Fatalf("RegisterSubscriptionSet called with %+v, want compile rejection before registration", *req)
	}
}

func exactIdentifierJoinSchema() *mockSchemaLookup {
	return &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"t": {id: 1, schema: &schema.TableSchema{ID: 1, Name: "t", Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "u32", Type: schema.KindUint32},
		}}},
		"s": {id: 2, schema: &schema.TableSchema{ID: 2, Name: "s", Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint32},
			{Index: 1, Name: "u32", Type: schema.KindUint32},
		}}},
	}}
}

func exactIdentifierIndexedJoinSchema() *mockSchemaLookup {
	sl := exactIdentifierJoinSchema()
	for _, name := range []string{"t", "s"} {
		entry := sl.tables[name]
		entry.schema.Indexes = []schema.IndexSchema{{ID: 1, Name: "idx_" + name + "_u32", Columns: []int{1}}}
		sl.tables[name] = entry
	}
	return sl
}

func exactIdentifierRegistry(t *testing.T, tableName string) (registrySchemaLookup, schema.TableID) {
	t.Helper()
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: tableName,
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint32, PrimaryKey: true},
			{Name: "u32", Type: schema.KindUint32},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	id, _, ok := eng.Registry().TableByName(tableName)
	if !ok {
		t.Fatalf("TableByName(%q) failed", tableName)
	}
	return registrySchemaLookup{reg: eng.Registry()}, id
}

func TestHandleOneOffQuery_SQLTableLookupIsByteExact(t *testing.T) {
	for _, sqlText := range []string{
		"SELECT * FROM PLAYERS",
		`SELECT * FROM "PLAYERS"`,
	} {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl, tableID := exactIdentifierRegistry(t, "players")
			stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
				tableID: {{types.NewUint32(1), types.NewUint32(7)}},
			}}}

			handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
				MessageID:   []byte{0xA0},
				QueryString: sqlText,
			}, stateAccess, sl)

			requireOneOffError(t, conn, "no such table: `PLAYERS`. If the table exists, it may be marked private.")
		})
	}
}

func TestHandleOneOffQuery_SQLColumnLookupIsByteExact(t *testing.T) {
	cases := []string{
		"SELECT * FROM t WHERE U32 = 7",
		`SELECT * FROM t WHERE "U32" = 7`,
		"SELECT U32 FROM t",
		`SELECT "U32" FROM t`,
		`SELECT t.* FROM t JOIN s ON t."U32" = s.u32`,
	}
	for _, sqlText := range cases {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl := exactIdentifierJoinSchema()
			stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

			handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
				MessageID:   []byte{0xA1},
				QueryString: sqlText,
			}, stateAccess, sl)

			requireOneOffError(t, conn, "`U32` is not in scope")
		})
	}
}

func TestHandleOneOffQuery_SQLAliasQualifierLookupIsByteExact(t *testing.T) {
	cases := []string{
		"SELECT R.* FROM t AS r",
		"SELECT * FROM t AS r WHERE R.id = 1",
		"SELECT r.* FROM t AS r JOIN s ON R.id = s.id",
	}
	for _, sqlText := range cases {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl := exactIdentifierJoinSchema()
			stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

			handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
				MessageID:   []byte{0xA2},
				QueryString: sqlText,
			}, stateAccess, sl)

			requireOneOffError(t, conn, "`R` is not in scope")
		})
	}
}

func TestHandleOneOffQuery_SQLJoinBaseTableQualifierAfterAliasRejected(t *testing.T) {
	cases := []string{
		"SELECT r.* FROM t AS r JOIN s AS q ON r.u32 = q.u32 WHERE t.id = 1",
		`SELECT r.* FROM t AS r JOIN s AS q ON r.u32 = q.u32 WHERE "t".id = 1`,
		"SELECT r.* FROM t AS r JOIN s AS q WHERE t.u32 = q.u32",
		"SELECT t.id FROM t AS r JOIN s AS q ON r.u32 = q.u32",
		"SELECT t.id FROM t AS r JOIN t AS q ON r.u32 = q.u32",
	}
	for _, sqlText := range cases {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl := exactIdentifierJoinSchema()
			stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

			handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
				MessageID:   []byte{0xA4},
				QueryString: sqlText,
			}, stateAccess, sl)

			requireOneOffError(t, conn, "`t` is not in scope")
		})
	}
}

func TestHandleOneOffQuery_SQLProjectionKeepsCaseDistinctColumns(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "u32", Type: schema.KindUint32},
		schema.ColumnSchema{Index: 1, Name: "U32", Type: schema.KindUint32},
	)
	stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {{types.NewUint32(1), types.NewUint32(2)}},
	}}}

	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0xA3},
		QueryString: "SELECT u32, U32 FROM t",
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("Error = %q, want success", *result.Error)
	}
	projected := &schema.TableSchema{ID: 1, Name: "projection", Columns: []schema.ColumnSchema{
		{Index: 0, Name: "u32", Type: schema.KindUint32},
		{Index: 1, Name: "U32", Type: schema.KindUint32},
	}}
	rows := decodeRows(t, firstTableRows(result), projected)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got := rows[0][0]; !got.Equal(types.NewUint32(1)) {
		t.Fatalf("projected u32 = %v, want Uint32(1)", got)
	}
	if got := rows[0][1]; !got.Equal(types.NewUint32(2)) {
		t.Fatalf("projected U32 = %v, want Uint32(2)", got)
	}
}

func TestHandleSubscribeSingle_SQLTableLookupIsByteExact(t *testing.T) {
	for _, sqlText := range []string{
		"SELECT * FROM PLAYERS",
		`SELECT * FROM "PLAYERS"`,
	} {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl, _ := exactIdentifierRegistry(t, "players")

			handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
				RequestID:   760,
				QueryID:     761,
				QueryString: sqlText,
			}, executor, sl)

			want := "no such table: `PLAYERS`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
			requireSubscriptionError(t, conn, 760, 761, want)
			requireNoSubscriptionRegistration(t, executor)
		})
	}
}

func TestHandleSubscribeSingle_SQLColumnLookupIsByteExact(t *testing.T) {
	cases := []string{
		"SELECT * FROM t WHERE U32 = 7",
		`SELECT * FROM t WHERE "U32" = 7`,
		`SELECT t.* FROM t JOIN s ON t."U32" = s.u32`,
	}
	for _, sqlText := range cases {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := exactIdentifierJoinSchema()

			handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
				RequestID:   762,
				QueryID:     763,
				QueryString: sqlText,
			}, executor, sl)

			want := "`U32` is not in scope, executing: `" + sqlText + "`"
			requireSubscriptionError(t, conn, 762, 763, want)
			requireNoSubscriptionRegistration(t, executor)
		})
	}
}

func TestHandleSubscribeSingle_SQLAliasQualifierLookupIsByteExact(t *testing.T) {
	cases := []string{
		"SELECT R.* FROM t AS r",
		"SELECT * FROM t AS r WHERE R.id = 1",
		"SELECT r.* FROM t AS r JOIN s ON R.id = s.id",
	}
	for _, sqlText := range cases {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := exactIdentifierJoinSchema()

			handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
				RequestID:   764,
				QueryID:     765,
				QueryString: sqlText,
			}, executor, sl)

			want := "`R` is not in scope, executing: `" + sqlText + "`"
			requireSubscriptionError(t, conn, 764, 765, want)
			requireNoSubscriptionRegistration(t, executor)
		})
	}
}

func TestHandleSubscribeSingle_SQLJoinBaseTableQualifierAfterAliasRejected(t *testing.T) {
	cases := []string{
		"SELECT r.* FROM t AS r JOIN s AS q ON r.u32 = q.u32 WHERE t.id = 1",
		`SELECT r.* FROM t AS r JOIN s AS q ON r.u32 = q.u32 WHERE "t".id = 1`,
		"SELECT t.id FROM t AS r JOIN s AS q ON r.u32 = q.u32",
		"SELECT t.id FROM t AS r JOIN t AS q ON r.u32 = q.u32",
	}
	for _, sqlText := range cases {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := exactIdentifierIndexedJoinSchema()

			handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
				RequestID:   768,
				QueryID:     769,
				QueryString: sqlText,
			}, executor, sl)

			requireNoSubscriptionRegistration(t, executor)
			want := "`t` is not in scope, executing: `" + sqlText + "`"
			requireSubscriptionError(t, conn, 768, 769, want)
		})
	}
}

func TestHandleSubscribeMulti_SQLIdentifiersAreByteExact(t *testing.T) {
	cases := []struct {
		name     string
		lookup   SchemaLookup
		validSQL string
		sqlText  string
		wantText string
	}{
		{
			name:     "table",
			validSQL: "SELECT * FROM players WHERE id = 1",
			sqlText:  "SELECT * FROM PLAYERS",
			wantText: "no such table: `PLAYERS`. If the table exists, it may be marked private.",
		},
		{
			name:     "column",
			lookup:   exactIdentifierJoinSchema(),
			validSQL: "SELECT * FROM t WHERE id = 1",
			sqlText:  "SELECT * FROM t WHERE U32 = 7",
			wantText: "`U32` is not in scope",
		},
		{
			name:     "join_where_base_after_alias",
			lookup:   exactIdentifierIndexedJoinSchema(),
			validSQL: "SELECT * FROM t WHERE id = 1",
			sqlText:  "SELECT r.* FROM t AS r JOIN s AS q ON r.u32 = q.u32 WHERE t.id = 1",
			wantText: "`t` is not in scope",
		},
		{
			name:     "join_projection_base_after_alias",
			lookup:   exactIdentifierIndexedJoinSchema(),
			validSQL: "SELECT * FROM t WHERE id = 1",
			sqlText:  "SELECT t.id FROM t AS r JOIN s AS q ON r.u32 = q.u32",
			wantText: "`t` is not in scope",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			executor := &mockSubExecutor{}
			sl := tc.lookup
			if sl == nil {
				var tableID schema.TableID
				sl, tableID = exactIdentifierRegistry(t, "players")
				_ = tableID
			}

			handleSubscribeMulti(context.Background(), conn, &SubscribeMultiMsg{
				RequestID:    766,
				QueryID:      767,
				QueryStrings: []string{tc.validSQL, tc.sqlText},
			}, executor, sl)

			requireNoSubscriptionRegistration(t, executor)
			want := tc.wantText + ", executing: `" + tc.sqlText + "`"
			requireSubscriptionError(t, conn, 766, 767, want)
		})
	}
}

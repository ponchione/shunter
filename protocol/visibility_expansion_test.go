package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func visibilityIdentity(seed byte) types.Identity {
	var id types.Identity
	for i := range id {
		id[i] = seed
	}
	return id
}

func visibilityMessagesSchema() (*mockSchemaLookup, *schema.TableSchema) {
	sl := newMockSchema("messages", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint64},
		schema.ColumnSchema{Index: 1, Name: "owner", Type: schema.KindString},
		schema.ColumnSchema{Index: 2, Name: "thread", Type: schema.KindUint64},
	)
	entry := sl.tables["messages"]
	entry.schema.ReadPolicy = schema.ReadPolicy{Access: schema.TableAccessPublic}
	sl.tables["messages"] = entry
	return sl, entry.schema
}

func ownerVisibilityFilters(sqls ...string) []VisibilityFilter {
	filters := make([]VisibilityFilter, 0, len(sqls))
	for _, sqlText := range sqls {
		filters = append(filters, VisibilityFilter{
			SQL:                sqlText,
			ReturnTableID:      1,
			UsesCallerIdentity: true,
		})
	}
	return filters
}

func visibilityMessageRows(alice, bob types.Identity) []types.ProductValue {
	return []types.ProductValue{
		{types.NewUint64(1), types.NewString(bob.Hex()), types.NewUint64(1)},
		{types.NewUint64(2), types.NewString(alice.Hex()), types.NewUint64(1)},
		{types.NewUint64(3), types.NewString(alice.Hex()), types.NewUint64(2)},
	}
}

func TestVisibilityExpansionRawOneOffReturnsOnlyCallerVisibleRows(t *testing.T) {
	alice := visibilityIdentity(0x0a)
	bob := visibilityIdentity(0x0b)
	conn := testConnDirect(nil)
	conn.AllowAllPermissions = false
	conn.Identity = alice
	sl, ts := visibilityMessagesSchema()

	handleOneOffQueryWithVisibility(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("visible-one-off"),
		QueryString: "SELECT * FROM messages",
	}, &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: visibilityMessageRows(alice, bob),
	}}}, sl, ownerVisibilityFilters("SELECT * FROM messages WHERE owner = :sender"))

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("one-off visibility error = %q", *result.Error)
	}
	rows := decodeRows(t, firstTableRows(result), ts)
	assertProductRowsEqual(t, rows, []types.ProductValue{
		{types.NewUint64(2), types.NewString(alice.Hex()), types.NewUint64(1)},
		{types.NewUint64(3), types.NewString(alice.Hex()), types.NewUint64(2)},
	})
}

func TestVisibilityExpansionRawSubscriptionInitialAndDeltasAreCallerVisible(t *testing.T) {
	alice := visibilityIdentity(0x0c)
	bob := visibilityIdentity(0x0d)
	sl, _ := visibilityMessagesSchema()
	compiled, err := CompileSQLQueryStringWithVisibility("SELECT * FROM messages", sl, &alice, SQLQueryValidationOptions{
		AllowLimit:      false,
		AllowProjection: false,
	}, ownerVisibilityFilters("SELECT * FROM messages WHERE owner = :sender"), false)
	if err != nil {
		t.Fatalf("CompileSQLQueryStringWithVisibility: %v", err)
	}

	inbox := make(chan subscription.FanOutMessage, 1)
	mgr := subscription.NewManager(sl, nil, subscription.WithFanOutInbox(inbox))
	initialView := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: visibilityMessageRows(alice, bob),
	}}
	result, err := mgr.RegisterSet(subscription.SubscriptionSetRegisterRequest{
		ConnID:                  types.ConnectionID{1},
		QueryID:                 10,
		Predicates:              []subscription.Predicate{compiled.Predicate()},
		PredicateHashIdentities: []*types.Identity{compiled.PredicateHashIdentity(alice)},
	}, initialView)
	if err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if len(result.Update) != 1 {
		t.Fatalf("initial updates = %#v, want one update", result.Update)
	}
	assertProductRowsEqual(t, result.Update[0].Inserts, []types.ProductValue{
		{types.NewUint64(2), types.NewString(alice.Hex()), types.NewUint64(1)},
		{types.NewUint64(3), types.NewString(alice.Hex()), types.NewUint64(2)},
	})

	cs := &store.Changeset{
		TxID: 2,
		Tables: map[schema.TableID]*store.TableChangeset{
			1: {
				TableID:   1,
				TableName: "messages",
				Inserts: []types.ProductValue{
					{types.NewUint64(4), types.NewString(bob.Hex()), types.NewUint64(3)},
					{types.NewUint64(5), types.NewString(alice.Hex()), types.NewUint64(3)},
				},
			},
		},
	}
	afterView := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: append(visibilityMessageRows(alice, bob),
			types.ProductValue{types.NewUint64(4), types.NewString(bob.Hex()), types.NewUint64(3)},
			types.ProductValue{types.NewUint64(5), types.NewString(alice.Hex()), types.NewUint64(3)},
		),
	}}
	mgr.EvalAndBroadcast(types.TxID(2), cs, afterView, subscription.PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("delta updates = %#v, want one update", updates)
	}
	assertProductRowsEqual(t, updates[0].Inserts, []types.ProductValue{
		{types.NewUint64(5), types.NewString(alice.Hex()), types.NewUint64(3)},
	})
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("delta deletes = %#v, want none", updates[0].Deletes)
	}
}

func TestVisibilityExpansionDifferentIdentitiesSeeDifferentRowsForSameSQL(t *testing.T) {
	alice := visibilityIdentity(0x0e)
	bob := visibilityIdentity(0x0f)
	sl, ts := visibilityMessagesSchema()
	rowsByTable := map[schema.TableID][]types.ProductValue{1: visibilityMessageRows(alice, bob)}
	filters := ownerVisibilityFilters("SELECT * FROM messages WHERE owner = :sender")

	aliceConn := testConnDirect(nil)
	aliceConn.AllowAllPermissions = false
	aliceConn.Identity = alice
	handleOneOffQueryWithVisibility(context.Background(), aliceConn, &OneOffQueryMsg{
		MessageID:   []byte("alice"),
		QueryString: "SELECT * FROM messages",
	}, &mockStateAccess{snap: &mockSnapshot{rows: rowsByTable}}, sl, filters)

	bobConn := testConnDirect(nil)
	bobConn.AllowAllPermissions = false
	bobConn.Identity = bob
	handleOneOffQueryWithVisibility(context.Background(), bobConn, &OneOffQueryMsg{
		MessageID:   []byte("bob"),
		QueryString: "SELECT * FROM messages",
	}, &mockStateAccess{snap: &mockSnapshot{rows: rowsByTable}}, sl, filters)

	aliceRows := decodeRows(t, firstTableRows(drainOneOff(t, aliceConn)), ts)
	bobRows := decodeRows(t, firstTableRows(drainOneOff(t, bobConn)), ts)
	assertProductRowsEqual(t, aliceRows, []types.ProductValue{
		{types.NewUint64(2), types.NewString(alice.Hex()), types.NewUint64(1)},
		{types.NewUint64(3), types.NewString(alice.Hex()), types.NewUint64(2)},
	})
	assertProductRowsEqual(t, bobRows, []types.ProductValue{
		{types.NewUint64(1), types.NewString(bob.Hex()), types.NewUint64(1)},
	})
}

func TestVisibilityExpansionMultipleFiltersOrTogether(t *testing.T) {
	alice := visibilityIdentity(0x10)
	bob := visibilityIdentity(0x11)
	conn := testConnDirect(nil)
	conn.AllowAllPermissions = false
	conn.Identity = alice
	sl, ts := visibilityMessagesSchema()

	handleOneOffQueryWithVisibility(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("or-filters"),
		QueryString: "SELECT * FROM messages",
	}, &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString(bob.Hex()), types.NewUint64(1)},
			{types.NewUint64(2), types.NewString(alice.Hex()), types.NewUint64(1)},
			{types.NewUint64(3), types.NewString("public"), types.NewUint64(2)},
		},
	}}}, sl, []VisibilityFilter{
		{SQL: "SELECT * FROM messages WHERE owner = :sender", ReturnTableID: 1, UsesCallerIdentity: true},
		{SQL: "SELECT * FROM messages WHERE owner = 'public'", ReturnTableID: 1},
	})

	rows := decodeRows(t, firstTableRows(drainOneOff(t, conn)), ts)
	assertProductRowsEqual(t, rows, []types.ProductValue{
		{types.NewUint64(2), types.NewString(alice.Hex()), types.NewUint64(1)},
		{types.NewUint64(3), types.NewString("public"), types.NewUint64(2)},
	})
}

func TestVisibilityExpansionSelfJoinFiltersEachAlias(t *testing.T) {
	alice := visibilityIdentity(0x12)
	bob := visibilityIdentity(0x13)
	conn := testConnDirect(nil)
	conn.AllowAllPermissions = false
	conn.Identity = alice
	sl, ts := visibilityMessagesSchema()

	handleOneOffQueryWithVisibility(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("self-join"),
		QueryString: "SELECT a.* FROM messages AS a JOIN messages AS b ON a.thread = b.thread",
	}, &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString(alice.Hex()), types.NewUint64(1)},
			{types.NewUint64(2), types.NewString(bob.Hex()), types.NewUint64(1)},
		},
	}}}, sl, ownerVisibilityFilters("SELECT * FROM messages WHERE owner = :sender"))

	rows := decodeRows(t, firstTableRows(drainOneOff(t, conn)), ts)
	assertProductRowsEqual(t, rows, []types.ProductValue{
		{types.NewUint64(1), types.NewString(alice.Hex()), types.NewUint64(1)},
	})
}

func TestVisibilityExpansionFilteredNonProjectedJoinTableDoesNotLeak(t *testing.T) {
	alice := visibilityIdentity(0x14)
	bob := visibilityIdentity(0x15)
	conn := testConnDirect(nil)
	conn.AllowAllPermissions = false
	conn.Identity = alice
	sl := &mockSchemaLookup{tables: map[string]struct {
		id     schema.TableID
		schema *schema.TableSchema
	}{
		"visible": {
			id: 1,
			schema: &schema.TableSchema{
				ID:         1,
				Name:       "visible",
				ReadPolicy: schema.ReadPolicy{Access: schema.TableAccessPublic},
				Columns: []schema.ColumnSchema{
					{Index: 0, Name: "id", Type: schema.KindUint64},
					{Index: 1, Name: "label", Type: schema.KindString},
				},
			},
		},
		"secret": {
			id: 2,
			schema: &schema.TableSchema{
				ID:         2,
				Name:       "secret",
				ReadPolicy: schema.ReadPolicy{Access: schema.TableAccessPublic},
				Columns: []schema.ColumnSchema{
					{Index: 0, Name: "id", Type: schema.KindUint64},
					{Index: 1, Name: "visible_id", Type: schema.KindUint64},
					{Index: 2, Name: "owner", Type: schema.KindString},
				},
			},
		},
	}}

	handleOneOffQueryWithVisibility(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("join-leak"),
		QueryString: "SELECT visible.* FROM visible JOIN secret ON visible.id = secret.visible_id",
	}, &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("only-bob-secret")},
			{types.NewUint64(2), types.NewString("alice-secret")},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(1), types.NewString(bob.Hex())},
			{types.NewUint64(11), types.NewUint64(2), types.NewString(alice.Hex())},
		},
	}}}, sl, []VisibilityFilter{
		{SQL: "SELECT * FROM secret WHERE owner = :sender", ReturnTableID: 2, UsesCallerIdentity: true},
	})

	visibleTS := sl.tables["visible"].schema
	rows := decodeRows(t, firstTableRows(drainOneOff(t, conn)), visibleTS)
	assertProductRowsEqual(t, rows, []types.ProductValue{
		{types.NewUint64(2), types.NewString("alice-secret")},
	})
}

func TestVisibilityExpansionAggregateAndLimitUseVisibleRows(t *testing.T) {
	alice := visibilityIdentity(0x16)
	bob := visibilityIdentity(0x17)
	sl, aggregateSchema := visibilityMessagesSchema()
	filters := ownerVisibilityFilters("SELECT * FROM messages WHERE owner = :sender")
	rowsByTable := map[schema.TableID][]types.ProductValue{1: visibilityMessageRows(alice, bob)}

	countConn := testConnDirect(nil)
	countConn.AllowAllPermissions = false
	countConn.Identity = alice
	handleOneOffQueryWithVisibility(context.Background(), countConn, &OneOffQueryMsg{
		MessageID:   []byte("count-visible"),
		QueryString: "SELECT COUNT(*) AS count FROM messages",
	}, &mockStateAccess{snap: &mockSnapshot{rows: rowsByTable}}, sl, filters)
	countRows := decodeRows(t, firstTableRows(drainOneOff(t, countConn)), &schema.TableSchema{
		Columns: []schema.ColumnSchema{{Index: 0, Name: "count", Type: schema.KindUint64}},
	})
	assertProductRowsEqual(t, countRows, []types.ProductValue{{types.NewUint64(2)}})

	limitConn := testConnDirect(nil)
	limitConn.AllowAllPermissions = false
	limitConn.Identity = alice
	handleOneOffQueryWithVisibility(context.Background(), limitConn, &OneOffQueryMsg{
		MessageID:   []byte("limit-visible"),
		QueryString: "SELECT * FROM messages LIMIT 1",
	}, &mockStateAccess{snap: &mockSnapshot{rows: rowsByTable}}, sl, filters)
	limitRows := decodeRows(t, firstTableRows(drainOneOff(t, limitConn)), aggregateSchema)
	assertProductRowsEqual(t, limitRows, []types.ProductValue{
		{types.NewUint64(2), types.NewString(alice.Hex()), types.NewUint64(1)},
	})
}

func TestVisibilityExpansionAllowAllBypassesFilters(t *testing.T) {
	alice := visibilityIdentity(0x18)
	bob := visibilityIdentity(0x19)
	conn := testConnDirect(nil)
	conn.AllowAllPermissions = true
	conn.Identity = alice
	sl, ts := visibilityMessagesSchema()

	handleOneOffQueryWithVisibility(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte("allow-all"),
		QueryString: "SELECT * FROM messages",
	}, &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: visibilityMessageRows(alice, bob),
	}}}, sl, ownerVisibilityFilters("SELECT * FROM messages WHERE owner = :sender"))

	rows := decodeRows(t, firstTableRows(drainOneOff(t, conn)), ts)
	assertProductRowsEqual(t, rows, visibilityMessageRows(alice, bob))
}

package shunter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const declaredReadProtocolSigningKey = "declared-read-protocol-secret"

func TestProtocolDeclaredQuerySucceedsWithDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "reader", "messages:read"))
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-query"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffRows(t, client, "messages", 1)
}

func TestProtocolDeclaredViewSucceedsWithDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 31,
		QueryID:   41,
		Name:      "live_messages",
	})
	requireDeclaredReadAppliedRows(t, client, 31, 41, "messages", 1)
}

func TestProtocolDeclaredViewUnindexedJoinRejected(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, NewModule("protocol_unindexed_join_reads").
		SchemaVersion(1).
		TableDef(joinReadTableDef("t")).
		TableDef(joinReadTableDef("s")).
		View(ViewDeclaration{
			Name:        "live_unindexed_t_rows",
			SQL:         "SELECT t.* FROM t JOIN s ON t.u32 = s.u32",
			Permissions: PermissionMetadata{Required: []string{"joins:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "subscriber", "joins:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 32,
		QueryID:   42,
		Name:      "live_unindexed_t_rows",
	})
	requireDeclaredReadSubscriptionError(t, client, 32, 42, "join column has no index")
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("ActiveSubscriptionSets = %d, want 0 after rejected protocol declared view", active)
	}
}

func TestProtocolDeclaredReadsApplyVisibilityToInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		VisibilityFilter(VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE body = :sender",
		}).
		Query(QueryDeclaration{
			Name:        "visible_messages",
			SQL:         "SELECT * FROM messages ORDER BY id",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_visible_messages",
			SQL:         "SELECT * FROM messages ORDER BY id",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()

	client, identityToken := dialDeclaredReadProtocolWithIdentity(t, rt, mintDeclaredReadProtocolToken(t, "visible-reader", "messages:read", "messages:subscribe"))
	visibleOwner := types.Identity(identityToken.Identity).Hex()
	hiddenOwner := visibilityRuntimeIdentity(0x55).Hex()
	insertMessageWithBody(t, rt, 1, visibleOwner)
	insertMessageWithBody(t, rt, 2, hiddenOwner)
	insertMessageWithBody(t, rt, 3, visibleOwner)

	columns := []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: types.KindUint64},
		{Index: 1, Name: "body", Type: types.KindString},
	}
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("visible-declared-query"),
		Name:      "visible_messages",
	})
	queryRows := requireDeclaredReadOneOffValues(t, client, "messages", columns)
	assertDeclaredReadVisibleMessageRows(t, queryRows, []uint64{1, 3}, visibleOwner, "declared query visibility")

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 34,
		QueryID:   44,
		Name:      "live_visible_messages",
	})
	initialRows := requireDeclaredReadAppliedValues(t, client, 34, 44, "messages", columns)
	assertDeclaredReadVisibleMessageRows(t, initialRows, []uint64{1, 3}, visibleOwner, "declared view initial visibility")

	insertMessageWithBody(t, rt, 4, hiddenOwner)
	insertMessageWithBody(t, rt, 5, visibleOwner)
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 44, "messages", columns)
	assertDeclaredReadVisibleMessageRows(t, inserts, []uint64{5}, visibleOwner, "declared view delta visibility")
	if len(deletes) != 0 {
		t.Fatalf("declared view delta visibility deletes = %#v, want none", deletes)
	}
}

func TestProtocolDeclaredViewMultiWayJoinSendsDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_message_chain",
			SQL:         "SELECT a.* FROM messages AS a JOIN messages AS b ON a.id = b.id JOIN messages AS c ON b.id = c.id",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "multi-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 32,
		QueryID:   42,
		Name:      "live_message_chain",
	})
	requireDeclaredReadAppliedRows(t, client, 32, 42, "messages", 1)

	insertMessage(t, rt, "world")
	requireDeclaredReadDeltaRows(t, client, 42, "messages", 1, 0)
}

func TestProtocolDeclaredViewColumnProjectionSendsProjectedInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_message_bodies",
			SQL:         "SELECT body AS text FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "projected-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 33,
		QueryID:   43,
		Name:      "live_message_bodies",
	})
	projectedColumns := []schema.ColumnSchema{{Name: "text", Type: types.KindString}}
	initial := requireDeclaredReadAppliedValues(t, client, 33, 43, "messages", projectedColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsString() != "hello" {
		t.Fatalf("projected initial rows = %#v, want one body row hello", initial)
	}

	insertMessage(t, rt, "world")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 43, "messages", projectedColumns)
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsString() != "world" || len(deletes) != 0 {
		t.Fatalf("projected delta inserts/deletes = %#v/%#v, want one body insert world", inserts, deletes)
	}
}

func TestProtocolDeclaredViewColumnProjectionSuppressesNoOpReplacementDelta(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Reducer("replace_message_projected_body", replaceMessageProjectedBodyReducer).
		View(ViewDeclaration{
			Name:        "live_message_bodies",
			SQL:         "SELECT body AS text FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, "same")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "projected-replacement-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 44,
		QueryID:   54,
		Name:      "live_message_bodies",
	})
	projectedColumns := []schema.ColumnSchema{{Name: "text", Type: types.KindString}}
	initial := requireDeclaredReadAppliedValues(t, client, 44, 54, "messages", projectedColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsString() != "same" {
		t.Fatalf("projected initial rows = %#v, want one body row same", initial)
	}

	replaceMessageProjectedBody(t, rt, 1, 2, "same")
	requireNoDeclaredReadProtocolMessage(t, client)
}

func TestProtocolDeclaredViewOrderBySendsOrderedInitialRowsAndRowDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Reducer("delete_message_by_id", deleteMessageByIDReducer).
		View(ViewDeclaration{
			Name:        "live_ordered_message_ranks",
			SQL:         "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 3, "bravo")
	insertMessageWithBody(t, rt, 1, "charlie")
	insertMessageWithBody(t, rt, 2, "charlie")
	insertMessageWithBody(t, rt, 4, "alpha")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "ordered-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 35,
		QueryID:   45,
		Name:      "live_ordered_message_ranks",
	})
	projectedColumns := []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: types.KindUint64},
		{Index: 1, Name: "text", Type: types.KindString},
	}
	initial := requireDeclaredReadAppliedValues(t, client, 35, 45, "messages", projectedColumns)
	if got, want := rowUint64IDs(initial), []uint64{1, 2, 3, 4}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ordered protocol initial ids = %v, want %v; rows=%#v", got, want, initial)
	}

	insertMessageWithBody(t, rt, 5, "delta")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 45, "messages", projectedColumns)
	if len(inserts) != 1 || inserts[0][0].AsUint64() != 5 || inserts[0][1].AsString() != "delta" || len(deletes) != 0 {
		t.Fatalf("ordered protocol insert delta inserts/deletes = %#v/%#v, want row 5/delta insert", inserts, deletes)
	}

	deleteMessageByID(t, rt, 2)
	inserts, deletes = requireDeclaredReadDeltaValues(t, client, 45, "messages", projectedColumns)
	if len(inserts) != 0 || len(deletes) != 1 || deletes[0][0].AsUint64() != 2 || deletes[0][1].AsString() != "charlie" {
		t.Fatalf("ordered protocol delete delta inserts/deletes = %#v/%#v, want row 2/charlie delete", inserts, deletes)
	}
}

func TestProtocolDeclaredViewLimitSendsLimitedInitialRowsAndRowDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Reducer("delete_message_by_id", deleteMessageByIDReducer).
		View(ViewDeclaration{
			Name:        "live_limited_message_ranks",
			SQL:         "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 3, "bravo")
	insertMessageWithBody(t, rt, 1, "charlie")
	insertMessageWithBody(t, rt, 2, "charlie")
	insertMessageWithBody(t, rt, 4, "alpha")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "limited-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 36,
		QueryID:   46,
		Name:      "live_limited_message_ranks",
	})
	projectedColumns := []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: types.KindUint64},
		{Index: 1, Name: "text", Type: types.KindString},
	}
	initial := requireDeclaredReadAppliedValues(t, client, 36, 46, "messages", projectedColumns)
	if got, want := rowUint64IDs(initial), []uint64{1, 2}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("limited protocol initial ids = %v, want %v; rows=%#v", got, want, initial)
	}

	insertMessageWithBody(t, rt, 5, "alpha")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 46, "messages", projectedColumns)
	if len(inserts) != 1 || inserts[0][0].AsUint64() != 5 || inserts[0][1].AsString() != "alpha" || len(deletes) != 0 {
		t.Fatalf("limited protocol insert delta inserts/deletes = %#v/%#v, want row 5/alpha insert", inserts, deletes)
	}

	deleteMessageByID(t, rt, 1)
	inserts, deletes = requireDeclaredReadDeltaValues(t, client, 46, "messages", projectedColumns)
	if len(inserts) != 0 || len(deletes) != 1 || deletes[0][0].AsUint64() != 1 || deletes[0][1].AsString() != "charlie" {
		t.Fatalf("limited protocol delete delta inserts/deletes = %#v/%#v, want row 1/charlie delete", inserts, deletes)
	}
}

func TestProtocolDeclaredViewOffsetSendsOffsetInitialRowsAndRowDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Reducer("delete_message_by_id", deleteMessageByIDReducer).
		View(ViewDeclaration{
			Name:        "live_offset_message_ranks",
			SQL:         "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 2 OFFSET 1",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 3, "bravo")
	insertMessageWithBody(t, rt, 1, "charlie")
	insertMessageWithBody(t, rt, 2, "charlie")
	insertMessageWithBody(t, rt, 4, "alpha")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "offset-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 37,
		QueryID:   47,
		Name:      "live_offset_message_ranks",
	})
	projectedColumns := []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: types.KindUint64},
		{Index: 1, Name: "text", Type: types.KindString},
	}
	initial := requireDeclaredReadAppliedValues(t, client, 37, 47, "messages", projectedColumns)
	if got, want := rowUint64IDs(initial), []uint64{2, 3}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("offset protocol initial ids = %v, want %v; rows=%#v", got, want, initial)
	}

	insertMessageWithBody(t, rt, 5, "alpha")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 47, "messages", projectedColumns)
	if len(inserts) != 1 || inserts[0][0].AsUint64() != 5 || inserts[0][1].AsString() != "alpha" || len(deletes) != 0 {
		t.Fatalf("offset protocol insert delta inserts/deletes = %#v/%#v, want row 5/alpha insert", inserts, deletes)
	}

	deleteMessageByID(t, rt, 2)
	inserts, deletes = requireDeclaredReadDeltaValues(t, client, 47, "messages", projectedColumns)
	if len(inserts) != 0 || len(deletes) != 1 || deletes[0][0].AsUint64() != 2 || deletes[0][1].AsString() != "charlie" {
		t.Fatalf("offset protocol delete delta inserts/deletes = %#v/%#v, want row 2/charlie delete", inserts, deletes)
	}
}

func TestProtocolDeclaredViewAggregateSendsInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_message_count",
			SQL:         "SELECT COUNT(body) AS n FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "aggregate-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 34,
		QueryID:   44,
		Name:      "live_message_count",
	})
	aggregateColumns := []schema.ColumnSchema{{Name: "n", Type: types.KindUint64}}
	initial := requireDeclaredReadAppliedValues(t, client, 34, 44, "messages", aggregateColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsUint64() != 1 {
		t.Fatalf("aggregate initial rows = %#v, want count 1", initial)
	}

	insertMessage(t, rt, "world")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 44, "messages", aggregateColumns)
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0].AsUint64() != 1 {
		t.Fatalf("aggregate delta deletes = %#v, want old count 1", deletes)
	}
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsUint64() != 2 {
		t.Fatalf("aggregate delta inserts = %#v, want new count 2", inserts)
	}
}

func TestProtocolDeclaredViewJoinAggregateSendsInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{
			Name:        "live_self_join_count",
			SQL:         "SELECT COUNT(*) AS n FROM messages AS a JOIN messages AS b ON a.id = b.id",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "join-aggregate-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 40,
		QueryID:   50,
		Name:      "live_self_join_count",
	})
	aggregateColumns := []schema.ColumnSchema{{Name: "n", Type: types.KindUint64}}
	initial := requireDeclaredReadAppliedValues(t, client, 40, 50, "messages", aggregateColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsUint64() != 1 {
		t.Fatalf("join aggregate initial rows = %#v, want count 1", initial)
	}

	insertMessage(t, rt, "world")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 50, "messages", aggregateColumns)
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0].AsUint64() != 1 {
		t.Fatalf("join aggregate delta deletes = %#v, want old count 1", deletes)
	}
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsUint64() != 2 {
		t.Fatalf("join aggregate delta inserts = %#v, want new count 2", inserts)
	}
}

func TestProtocolDeclaredViewJoinSumAggregateSendsInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		View(ViewDeclaration{
			Name:        "live_self_join_total",
			SQL:         "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b ON a.id = b.id",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "join-sum-aggregate-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 41,
		QueryID:   51,
		Name:      "live_self_join_total",
	})
	aggregateColumns := []schema.ColumnSchema{{Name: "total", Type: types.KindUint64}}
	initial := requireDeclaredReadAppliedValues(t, client, 41, 51, "messages", aggregateColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsUint64() != 1 {
		t.Fatalf("join SUM aggregate initial rows = %#v, want total 1", initial)
	}

	insertMessageWithBody(t, rt, 2, "world")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 51, "messages", aggregateColumns)
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0].AsUint64() != 1 {
		t.Fatalf("join SUM aggregate delta deletes = %#v, want old total 1", deletes)
	}
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsUint64() != 3 {
		t.Fatalf("join SUM aggregate delta inserts = %#v, want new total 3", inserts)
	}
}

func TestProtocolDeclaredViewCrossJoinSumAggregateSendsInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		View(ViewDeclaration{
			Name:        "live_self_cross_join_total",
			SQL:         "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "cross-join-sum-aggregate-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 42,
		QueryID:   52,
		Name:      "live_self_cross_join_total",
	})
	aggregateColumns := []schema.ColumnSchema{{Name: "total", Type: types.KindUint64}}
	initial := requireDeclaredReadAppliedValues(t, client, 42, 52, "messages", aggregateColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsUint64() != 1 {
		t.Fatalf("cross-join SUM aggregate initial rows = %#v, want total 1", initial)
	}

	insertMessageWithBody(t, rt, 2, "world")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 52, "messages", aggregateColumns)
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0].AsUint64() != 1 {
		t.Fatalf("cross-join SUM aggregate delta deletes = %#v, want old total 1", deletes)
	}
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsUint64() != 6 {
		t.Fatalf("cross-join SUM aggregate delta inserts = %#v, want new total 6", inserts)
	}
}

func TestProtocolDeclaredViewMultiWayJoinSumAggregateSendsInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		View(ViewDeclaration{
			Name:        "live_self_multi_join_total",
			SQL:         "SELECT SUM(a.id) AS total FROM messages AS a JOIN messages AS b ON a.id = b.id JOIN messages AS c ON b.id = c.id",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, "hello")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "multi-way-join-sum-aggregate-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 43,
		QueryID:   53,
		Name:      "live_self_multi_join_total",
	})
	aggregateColumns := []schema.ColumnSchema{{Name: "total", Type: types.KindUint64}}
	initial := requireDeclaredReadAppliedValues(t, client, 43, 53, "messages", aggregateColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsUint64() != 1 {
		t.Fatalf("multi-way join SUM aggregate initial rows = %#v, want total 1", initial)
	}

	insertMessageWithBody(t, rt, 2, "world")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 53, "messages", aggregateColumns)
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0].AsUint64() != 1 {
		t.Fatalf("multi-way join SUM aggregate delta deletes = %#v, want old total 1", deletes)
	}
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsUint64() != 3 {
		t.Fatalf("multi-way join SUM aggregate delta inserts = %#v, want new total 3", inserts)
	}
}

func TestProtocolDeclaredViewCountDistinctAggregateSendsInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Reducer("delete_message_by_id", deleteMessageByIDReducer).
		View(ViewDeclaration{
			Name:        "live_distinct_message_bodies",
			SQL:         "SELECT COUNT(DISTINCT body) AS n FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, "alpha")
	insertMessageWithBody(t, rt, 2, "alpha")
	insertMessageWithBody(t, rt, 3, "bravo")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "distinct-aggregate-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 39,
		QueryID:   49,
		Name:      "live_distinct_message_bodies",
	})
	aggregateColumns := []schema.ColumnSchema{{Name: "n", Type: types.KindUint64}}
	initial := requireDeclaredReadAppliedValues(t, client, 39, 49, "messages", aggregateColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsUint64() != 2 {
		t.Fatalf("COUNT(DISTINCT) aggregate initial rows = %#v, want distinct count 2", initial)
	}

	insertMessageWithBody(t, rt, 4, "charlie")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 49, "messages", aggregateColumns)
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0].AsUint64() != 2 {
		t.Fatalf("COUNT(DISTINCT) aggregate delta deletes = %#v, want old distinct count 2", deletes)
	}
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsUint64() != 3 {
		t.Fatalf("COUNT(DISTINCT) aggregate delta inserts = %#v, want new distinct count 3", inserts)
	}

}

func TestProtocolDeclaredViewSumAggregateSendsInitialRowsAndDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Reducer("delete_message_by_id", deleteMessageByIDReducer).
		View(ViewDeclaration{
			Name:        "live_message_total",
			SQL:         "SELECT SUM(id) AS total FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessageWithBody(t, rt, 1, "alpha")
	insertMessageWithBody(t, rt, 2, "bravo")
	insertMessageWithBody(t, rt, 3, "charlie")

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "sum-aggregate-subscriber", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 38,
		QueryID:   48,
		Name:      "live_message_total",
	})
	aggregateColumns := []schema.ColumnSchema{{Name: "total", Type: types.KindUint64}}
	initial := requireDeclaredReadAppliedValues(t, client, 38, 48, "messages", aggregateColumns)
	if len(initial) != 1 || len(initial[0]) != 1 || initial[0][0].AsUint64() != 6 {
		t.Fatalf("SUM aggregate initial rows = %#v, want total 6", initial)
	}

	insertMessageWithBody(t, rt, 4, "delta")
	inserts, deletes := requireDeclaredReadDeltaValues(t, client, 48, "messages", aggregateColumns)
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0].AsUint64() != 6 {
		t.Fatalf("SUM aggregate delta deletes = %#v, want old total 6", deletes)
	}
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsUint64() != 10 {
		t.Fatalf("SUM aggregate delta inserts = %#v, want new total 10", inserts)
	}

	deleteMessageByID(t, rt, 2)
	inserts, deletes = requireDeclaredReadDeltaValues(t, client, 48, "messages", aggregateColumns)
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0].AsUint64() != 10 {
		t.Fatalf("SUM aggregate delete delta deletes = %#v, want old total 10", deletes)
	}
	if len(inserts) != 1 || len(inserts[0]) != 1 || inserts[0][0].AsUint64() != 8 {
		t.Fatalf("SUM aggregate delete delta inserts = %#v, want new total 8", inserts)
	}
}

func TestProtocolDeclaredReadsSurviveCleanRestart(t *testing.T) {
	dataDir := t.TempDir()
	cfg := declaredReadProtocolConfig(t)
	cfg.DataDir = dataDir
	module := func() *Module {
		return validChatModule().
			Reducer("insert_message", insertMessageReducer).
			Query(QueryDeclaration{
				Name:        "recent_messages",
				SQL:         "SELECT * FROM messages",
				Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			}).
			View(ViewDeclaration{
				Name:        "live_messages",
				SQL:         "SELECT * FROM messages",
				Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			})
	}

	rt := buildStartedDeclaredReadRuntimeWithConfig(t, module(), cfg)
	insertMessage(t, rt, "hello")
	if err := rt.Close(); err != nil {
		t.Fatalf("Close before declared-read restart: %v", err)
	}

	rt = buildStartedDeclaredReadRuntimeWithConfig(t, module(), cfg)
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "restart-client", "messages:read", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-query-after-restart"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffRows(t, client, "messages", 1)

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 51,
		QueryID:   61,
		Name:      "live_messages",
	})
	requireDeclaredReadAppliedRows(t, client, 51, 61, "messages", 1)

	insertMessage(t, rt, "world")
	requireDeclaredReadDeltaRows(t, client, 61, "messages", 1, 0)
}

func TestProtocolDeclaredReadRejectionsDoNotRecoverAfterCleanRestart(t *testing.T) {
	dataDir := t.TempDir()
	cfg := declaredReadProtocolConfig(t)
	cfg.DataDir = dataDir
	module := func() *Module {
		return validChatModule().
			Reducer("insert_message", insertMessageReducer).
			Query(QueryDeclaration{
				Name:        "recent_messages",
				SQL:         "SELECT * FROM messages",
				Permissions: PermissionMetadata{Required: []string{"messages:read"}},
			}).
			View(ViewDeclaration{
				Name:        "live_messages",
				SQL:         "SELECT * FROM messages",
				Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
			})
	}

	rt := buildStartedDeclaredReadRuntimeWithConfig(t, module(), cfg)
	deniedClient := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "denied-before-restart"))
	writeDeclaredReadProtocolMessage(t, deniedClient, protocol.DeclaredQueryMsg{
		MessageID: []byte("denied-declared-query-before-restart"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffError(t, deniedClient, "permission denied")

	writeDeclaredReadProtocolMessage(t, deniedClient, protocol.SubscribeDeclaredViewMsg{
		RequestID: 71,
		QueryID:   81,
		Name:      "live_messages",
	})
	requireDeclaredReadSubscriptionError(t, deniedClient, 71, 81, "permission denied")

	writeDeclaredReadProtocolMessage(t, deniedClient, protocol.DeclaredQueryMsg{
		MessageID: []byte("unknown-declared-query-before-restart"),
		Name:      "missing_declared_query",
	})
	requireDeclaredReadOneOffError(t, deniedClient, "unknown declared read")

	writeDeclaredReadProtocolMessage(t, deniedClient, protocol.SubscribeDeclaredViewMsg{
		RequestID: 72,
		QueryID:   82,
		Name:      "missing_declared_view",
	})
	requireDeclaredReadSubscriptionError(t, deniedClient, 72, 82, "unknown declared read")

	insertMessage(t, rt, "before-restart")
	if err := rt.Close(); err != nil {
		t.Fatalf("Close after rejected declared reads before restart: %v", err)
	}

	rt = buildStartedDeclaredReadRuntimeWithConfig(t, module(), cfg)
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "authorized-after-restart", "messages:read", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-query-after-rejected-restart"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffRows(t, client, "messages", 1)

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 71,
		QueryID:   81,
		Name:      "live_messages",
	})
	requireDeclaredReadAppliedRows(t, client, 71, 81, "messages", 1)

	insertMessage(t, rt, "after-restart")
	requireDeclaredReadDeltaRows(t, client, 81, "messages", 1, 0)
}

func TestProtocolDeclaredReadsReportPermissionDeniedAndUnknownName(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "missing"))
	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("missing-permission-query"),
		Name:      "recent_messages",
	})
	requireDeclaredReadOneOffError(t, client, "permission denied")

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 32,
		QueryID:   42,
		Name:      "live_messages",
	})
	requireDeclaredReadSubscriptionError(t, client, 32, 42, "permission denied")

	writeDeclaredReadProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: []byte("unknown-query"),
		Name:      "SELECT * FROM messages",
	})
	requireDeclaredReadOneOffError(t, client, "unknown declared read")

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: 33,
		QueryID:   43,
		Name:      "SELECT * FROM messages",
	})
	requireDeclaredReadSubscriptionError(t, client, 33, 43, "unknown declared read")
}

func TestProtocolRawSQLEquivalentDoesNotUseDeclarationPermission(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "raw-sql", "messages:read", "messages:subscribe"))
	writeDeclaredReadProtocolMessage(t, client, protocol.OneOffQueryMsg{
		MessageID:   []byte("raw-query"),
		QueryString: "SELECT * FROM messages",
	})
	requireDeclaredReadOneOffError(t, client, "no such table: `messages`. If the table exists, it may be marked private.")

	writeDeclaredReadProtocolMessage(t, client, protocol.SubscribeSingleMsg{
		RequestID:   34,
		QueryID:     44,
		QueryString: "SELECT * FROM messages",
	})
	requireDeclaredReadSubscriptionError(t, client, 34, 44, "no such table: `messages`. If the table exists, it may be marked private.")
}

func TestProtocolDeclaredQueryUsesRuntimeClientSender(t *testing.T) {
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, validChatModule().
		Reducer("insert_message", insertMessageReducer).
		Query(QueryDeclaration{
			Name:        "recent_messages",
			SQL:         "SELECT * FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}), declaredReadProtocolConfig(t))
	defer rt.Close()
	insertMessage(t, rt, "hello")

	sender := &declaredReadCapturingSender{}
	rt.mu.Lock()
	rt.protocolSender = sender
	rt.mu.Unlock()

	conn := newDeclaredReadProtocolTestConn(t, "messages:read")
	rt.HandleDeclaredQuery(context.Background(), conn, &protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-query-through-sender"),
		Name:      "recent_messages",
	})

	sendCalls, gotConnID, gotMsg, heavyCalls, lightCalls := sender.snapshot()
	if sendCalls != 1 {
		t.Fatalf("sender Send calls = %d, want 1", sendCalls)
	}
	if heavyCalls != 0 || lightCalls != 0 {
		t.Fatalf("transaction sender calls = heavy:%d light:%d, want 0/0", heavyCalls, lightCalls)
	}
	if gotConnID != conn.ID {
		t.Fatalf("sender conn ID = %x, want %x", gotConnID[:], conn.ID[:])
	}
	if got := len(conn.OutboundCh); got != 0 {
		t.Fatalf("conn outbound queue length = %d, want 0; declared reads must use ClientSender", got)
	}

	resp, ok := gotMsg.(protocol.OneOffQueryResponse)
	if !ok {
		t.Fatalf("sender msg = %T, want OneOffQueryResponse", gotMsg)
	}
	if resp.Error != nil {
		t.Fatalf("declared query error = %q, want nil", *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != "messages" {
		t.Fatalf("declared query tables = %+v, want messages table", resp.Tables)
	}
	rows, err := protocol.DecodeRowList(resp.Tables[0].Rows)
	if err != nil {
		t.Fatalf("DecodeRowList sender response: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sender response rows = %d, want 1", len(rows))
	}
}

func declaredReadProtocolConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		DataDir:        t.TempDir(),
		EnableProtocol: true,
		AuthMode:       AuthModeStrict,
		AuthSigningKey: []byte(declaredReadProtocolSigningKey),
	}
}

func mintDeclaredReadProtocolToken(t *testing.T, subject string, permissions ...string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":         "declared-read-test",
		"sub":         subject,
		"permissions": permissions,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(declaredReadProtocolSigningKey))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

type declaredReadCapturingSender struct {
	mu sync.Mutex

	sendCalls int
	sendConn  types.ConnectionID
	sendMsg   any
	sendErr   error

	heavyCalls int
	lightCalls int
}

func (s *declaredReadCapturingSender) Send(connID types.ConnectionID, msg any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCalls++
	s.sendConn = connID
	s.sendMsg = msg
	return s.sendErr
}

func (s *declaredReadCapturingSender) SendTransactionUpdate(types.ConnectionID, *protocol.TransactionUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heavyCalls++
	return nil
}

func (s *declaredReadCapturingSender) SendTransactionUpdateLight(types.ConnectionID, *protocol.TransactionUpdateLight) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lightCalls++
	return nil
}

func (s *declaredReadCapturingSender) snapshot() (sendCalls int, sendConn types.ConnectionID, sendMsg any, heavyCalls, lightCalls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendCalls, s.sendConn, s.sendMsg, s.heavyCalls, s.lightCalls
}

func newDeclaredReadProtocolTestConn(t *testing.T, permissions ...string) *protocol.Conn {
	t.Helper()
	opts := protocol.DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn := protocol.NewConn(protocol.GenerateConnectionID(), types.Identity{1}, "", false, nil, &opts)
	conn.Permissions = append([]string(nil), permissions...)
	return conn
}

func dialDeclaredReadProtocol(t *testing.T, rt *Runtime, token string) *websocket.Conn {
	client, _ := dialDeclaredReadProtocolWithIdentity(t, rt, token)
	return client
}

func dialDeclaredReadProtocolWithIdentity(t *testing.T, rt *Runtime, token string) (*websocket.Conn, protocol.IdentityToken) {
	t.Helper()
	srv := httptest.NewServer(rt.HTTPHandler())
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/subscribe"
	header := http.Header{"Authorization": []string{"Bearer " + token}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader:   header,
		Subprotocols: []string{protocol.SubprotocolV1},
	})
	if err != nil {
		t.Fatalf("dial runtime protocol: %v", err)
	}
	t.Cleanup(func() { client.CloseNow() })

	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagIdentityToken {
		t.Fatalf("first protocol tag = %d, msg = %T, want IdentityToken", tag, msg)
	}
	identityToken, ok := msg.(protocol.IdentityToken)
	if !ok {
		t.Fatalf("first protocol msg = %T, want IdentityToken", msg)
	}
	return client, identityToken
}

func writeDeclaredReadProtocolMessage(t *testing.T, client *websocket.Conn, msg any) {
	t.Helper()
	frame, err := protocol.EncodeClientMessage(msg)
	if err != nil {
		t.Fatalf("EncodeClientMessage(%T): %v", msg, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write protocol message %T: %v", msg, err)
	}
}

func readDeclaredReadProtocolMessage(t *testing.T, client *websocket.Conn) (uint8, any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, frame, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read protocol message: %v", err)
	}
	tag, msg, err := protocol.DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("DecodeServerMessage: %v", err)
	}
	return tag, msg
}

func requireNoDeclaredReadProtocolMessage(t *testing.T, client *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, frame, err := client.Read(ctx)
	if err == nil {
		tag, msg, decErr := protocol.DecodeServerMessage(frame)
		if decErr != nil {
			t.Fatalf("unexpected undecodable protocol message: %v", decErr)
		}
		t.Fatalf("unexpected protocol message tag=%d msg=%+v, want none", tag, msg)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read protocol message error = %v, want timeout with no message", err)
	}
}

func replaceMessageProjectedBody(t *testing.T, rt *Runtime, oldID, newID byte, body string) {
	t.Helper()
	args := append([]byte{oldID, newID}, []byte(body)...)
	res, err := rt.CallReducer(context.Background(), "replace_message_projected_body", args)
	if err != nil {
		t.Fatalf("replace projected message reducer admission: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("replace projected message reducer status = %v, err = %v, want committed", res.Status, res.Error)
	}
}

func replaceMessageProjectedBodyReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("missing ids")
	}
	oldID := uint64(args[0])
	newID := uint64(args[1])
	body := string(args[2:])
	for rowID, row := range ctx.DB.ScanTable(0) {
		if len(row) > 0 && row[0].AsUint64() == oldID {
			if err := ctx.DB.Delete(0, rowID); err != nil {
				return nil, err
			}
			_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(newID), types.NewString(body)})
			return nil, err
		}
	}
	return nil, fmt.Errorf("message %d not found", oldID)
}

func requireDeclaredReadOneOffRows(t *testing.T, client *websocket.Conn, wantTable string, wantRows int) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagOneOffQueryResponse {
		t.Fatalf("tag = %d, want OneOffQueryResponse", tag)
	}
	resp := msg.(protocol.OneOffQueryResponse)
	if resp.Error != nil {
		t.Fatalf("declared query error = %q, want nil", *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != wantTable {
		t.Fatalf("declared query tables = %+v, want table %q", resp.Tables, wantTable)
	}
	rows, err := protocol.DecodeRowList(resp.Tables[0].Rows)
	if err != nil {
		t.Fatalf("DecodeRowList declared query rows for table %q: %v", wantTable, err)
	}
	if len(rows) != wantRows {
		t.Fatalf("declared query row count = %d, want %d for table %q", len(rows), wantRows, wantTable)
	}
}

func requireDeclaredReadOneOffValues(t *testing.T, client *websocket.Conn, wantTable string, columns []schema.ColumnSchema) []types.ProductValue {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagOneOffQueryResponse {
		t.Fatalf("tag = %d, want OneOffQueryResponse", tag)
	}
	resp := msg.(protocol.OneOffQueryResponse)
	if resp.Error != nil {
		t.Fatalf("declared query error = %q, want nil", *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != wantTable {
		t.Fatalf("declared query tables = %+v, want table %q", resp.Tables, wantTable)
	}
	return decodeDeclaredReadProtocolRows(t, resp.Tables[0].Rows, columns)
}

func requireDeclaredReadAppliedRows(t *testing.T, client *websocket.Conn, requestID, queryID uint32, wantTable string, wantRows int) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagSubscribeSingleApplied {
		t.Fatalf("tag = %d, want SubscribeSingleApplied for request=%d query=%d", tag, requestID, queryID)
	}
	applied := msg.(protocol.SubscribeSingleApplied)
	if applied.RequestID != requestID || applied.QueryID != queryID || applied.TableName != wantTable {
		t.Fatalf("declared view applied = %+v, want request=%d query=%d table=%q", applied, requestID, queryID, wantTable)
	}
	rows, err := protocol.DecodeRowList(applied.Rows)
	if err != nil {
		t.Fatalf("DecodeRowList declared view rows for request=%d query=%d table=%q: %v", requestID, queryID, wantTable, err)
	}
	if len(rows) != wantRows {
		t.Fatalf("declared view initial row count = %d, want %d for request=%d query=%d table=%q", len(rows), wantRows, requestID, queryID, wantTable)
	}
}

func requireDeclaredReadAppliedValues(t *testing.T, client *websocket.Conn, requestID, queryID uint32, wantTable string, columns []schema.ColumnSchema) []types.ProductValue {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagSubscribeSingleApplied {
		t.Fatalf("tag = %d, want SubscribeSingleApplied for request=%d query=%d", tag, requestID, queryID)
	}
	applied := msg.(protocol.SubscribeSingleApplied)
	if applied.RequestID != requestID || applied.QueryID != queryID || applied.TableName != wantTable {
		t.Fatalf("declared view applied = %+v, want request=%d query=%d table=%q", applied, requestID, queryID, wantTable)
	}
	return decodeDeclaredReadProtocolRows(t, applied.Rows, columns)
}

func requireDeclaredReadDeltaRows(t *testing.T, client *websocket.Conn, queryID uint32, wantTable string, wantInserts, wantDeletes int) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("tag = %d, want TransactionUpdateLight for query=%d", tag, queryID)
	}
	update := msg.(protocol.TransactionUpdateLight)
	if len(update.Update) != 1 {
		t.Fatalf("declared view update entries = %+v, want one entry for query=%d", update.Update, queryID)
	}
	entry := update.Update[0]
	if entry.QueryID != queryID || entry.TableName != wantTable {
		t.Fatalf("declared view update entry = %+v, want query=%d table=%q", entry, queryID, wantTable)
	}
	insertRows, err := protocol.DecodeRowList(entry.Inserts)
	if err != nil {
		t.Fatalf("DecodeRowList declared view delta inserts for query=%d table=%q: %v", queryID, wantTable, err)
	}
	deleteRows, err := protocol.DecodeRowList(entry.Deletes)
	if err != nil {
		t.Fatalf("DecodeRowList declared view delta deletes for query=%d table=%q: %v", queryID, wantTable, err)
	}
	if len(insertRows) != wantInserts || len(deleteRows) != wantDeletes {
		t.Fatalf("declared view delta inserts/deletes = %d/%d, want %d/%d for query=%d table=%q", len(insertRows), len(deleteRows), wantInserts, wantDeletes, queryID, wantTable)
	}
}

func requireDeclaredReadDeltaValues(t *testing.T, client *websocket.Conn, queryID uint32, wantTable string, columns []schema.ColumnSchema) ([]types.ProductValue, []types.ProductValue) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("tag = %d, want TransactionUpdateLight for query=%d", tag, queryID)
	}
	update := msg.(protocol.TransactionUpdateLight)
	if len(update.Update) != 1 {
		t.Fatalf("declared view update entries = %+v, want one entry for query=%d", update.Update, queryID)
	}
	entry := update.Update[0]
	if entry.QueryID != queryID || entry.TableName != wantTable {
		t.Fatalf("declared view update entry = %+v, want query=%d table=%q", entry, queryID, wantTable)
	}
	return decodeDeclaredReadProtocolRows(t, entry.Inserts, columns), decodeDeclaredReadProtocolRows(t, entry.Deletes, columns)
}

func decodeDeclaredReadProtocolRows(t *testing.T, rowList []byte, columns []schema.ColumnSchema) []types.ProductValue {
	t.Helper()
	rawRows, err := protocol.DecodeRowList(rowList)
	if err != nil {
		t.Fatalf("DecodeRowList projected declared rows: %v", err)
	}
	ts := &schema.TableSchema{Columns: columns}
	rows := make([]types.ProductValue, 0, len(rawRows))
	for _, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, ts)
		if err != nil {
			t.Fatalf("DecodeProductValue projected declared row: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

func requireDeclaredReadOneOffError(t *testing.T, client *websocket.Conn, wantSubstring string) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagOneOffQueryResponse {
		t.Fatalf("tag = %d, want OneOffQueryResponse", tag)
	}
	resp := msg.(protocol.OneOffQueryResponse)
	if resp.Error == nil {
		t.Fatal("one-off response error = nil, want error")
	}
	if !strings.Contains(*resp.Error, wantSubstring) {
		t.Fatalf("one-off response error = %q, want substring %q", *resp.Error, wantSubstring)
	}
}

func requireDeclaredReadSubscriptionError(t *testing.T, client *websocket.Conn, requestID, queryID uint32, wantSubstring string) {
	t.Helper()
	tag, msg := readDeclaredReadProtocolMessage(t, client)
	if tag != protocol.TagSubscriptionError {
		t.Fatalf("tag = %d, want SubscriptionError", tag)
	}
	resp := msg.(protocol.SubscriptionError)
	if resp.RequestID == nil || *resp.RequestID != requestID {
		t.Fatalf("subscription error request id = %v, want %d", resp.RequestID, requestID)
	}
	if resp.QueryID == nil || *resp.QueryID != queryID {
		t.Fatalf("subscription error query id = %v, want %d", resp.QueryID, queryID)
	}
	if !strings.Contains(resp.Error, wantSubstring) {
		t.Fatalf("subscription error = %q, want substring %q", resp.Error, wantSubstring)
	}
}

func assertDeclaredReadVisibleMessageRows(t *testing.T, rows []types.ProductValue, wantIDs []uint64, wantBody string, label string) {
	t.Helper()
	if len(rows) != len(wantIDs) {
		t.Fatalf("%s rows = %#v, want ids %v", label, rows, wantIDs)
	}
	for i, wantID := range wantIDs {
		if len(rows[i]) != 2 {
			t.Fatalf("%s row %d = %#v, want id/body", label, i, rows[i])
		}
		if rows[i][0].AsUint64() != wantID || rows[i][1].AsString() != wantBody {
			t.Fatalf("%s row %d = %#v, want id=%d body=%q", label, i, rows[i], wantID, wantBody)
		}
	}
}

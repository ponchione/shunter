package shunter

import (
	"context"
	"maps"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/protocolclient"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type callerDeliveryGate struct {
	subscription.FanOutSender
	connID           types.ConnectionID
	started, release chan struct{}
	once             sync.Once
}

func (g *callerDeliveryGate) wait(connID types.ConnectionID) {
	if connID == g.connID {
		g.once.Do(func() {
			close(g.started)
			<-g.release
		})
	}
}

func (g *callerDeliveryGate) SendTransactionUpdateLight(connID types.ConnectionID, requestID uint32, updates []subscription.SubscriptionUpdate, memo *subscription.EncodingMemo) error {
	g.wait(connID)
	return g.FanOutSender.SendTransactionUpdateLight(connID, requestID, updates, memo)
}

func (g *callerDeliveryGate) SendTransactionUpdateHeavy(connID types.ConnectionID, outcome subscription.CallerOutcome, updates []subscription.SubscriptionUpdate, memo *subscription.EncodingMemo) error {
	g.wait(connID)
	return g.FanOutSender.SendTransactionUpdateHeavy(connID, outcome, updates, memo)
}

func TestProtocolCallerDeltaCommitOrder(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		externalInsert, disconnect bool
		durable                    bool
	}{
		{name: "local_insert_external_delete"},
		{name: "external_insert_local_delete", externalInsert: true},
		{name: "disconnect_with_queued_reply", disconnect: true},
		{name: "local_insert_durable_delete", durable: true},
		{name: "durable_insert_local_delete", externalInsert: true, durable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deleted := make(chan struct{})
			rt, err := Build(validChatModule().
				Reducer("insert_message", insertMessageReducer).
				Reducer("delete_message", func(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
					defer close(deleted)
					return deleteMessageByIDReducer(ctx, args)
				}).
				Reducer("barrier", func(*schema.ReducerContext, []byte) ([]byte, error) { return nil, nil }),
				Config{DataDir: t.TempDir(), EnableProtocol: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = rt.Close() })
			server := httptest.NewServer(rt.HTTPHandler())
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)
			dial := func() *protocolclient.Client {
				client, _, err := protocolclient.Dial(ctx, protocolclient.Options{
					URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/subscribe", AllowAnonymous: true,
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = client.Close(ctx) })
				return client
			}
			read := func(client *protocolclient.Client) any {
				_, msg, err := client.Read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				return msg
			}
			subscribe := func(client *protocolclient.Client, wantRows int) map[string]bool {
				if err := client.Send(ctx, protocol.SubscribeSingleMsg{RequestID: 1, QueryID: 1, QueryString: "SELECT * FROM messages"}); err != nil {
					t.Fatal(err)
				}
				msg := read(client)
				applied, ok := msg.(protocol.SubscribeSingleApplied)
				if !ok {
					t.Fatalf("initial response = %T, want SubscribeSingleApplied", msg)
				}
				rows, err := protocol.DecodeRowList(applied.Rows)
				if err != nil || len(rows) != wantRows {
					t.Fatalf("initial rows = %d, error %v; want %d", len(rows), err, wantRows)
				}
				cache := make(map[string]bool)
				for _, row := range rows {
					cache[string(row)] = true
				}
				return cache
			}
			local := func(name string, args []byte) {
				result, err := rt.CallReducer(ctx, name, args)
				if err != nil || result.Status != StatusCommitted {
					t.Fatalf("%s = %+v, %v", name, result, err)
				}
			}
			external := func(client *protocolclient.Client, name string, args []byte, requestID uint32) {
				flags := protocol.CallReducerFlagsFullUpdate
				if tc.durable {
					flags = protocol.CallReducerFlagsDurableSuccess
				}
				if err := client.Send(ctx, protocol.CallReducerMsg{ReducerName: name, Args: args, RequestID: requestID, Flags: flags}); err != nil {
					t.Fatal(err)
				}
			}
			client := dial()
			cache := subscribe(client, 0)
			swappable := rt.fanOutSender.(*swappableFanOutSender)
			gate := &callerDeliveryGate{FanOutSender: swappable.Target(), connID: client.IdentityToken().ConnectionID, started: make(chan struct{}), release: make(chan struct{})}
			swappable.SetTarget(gate)
			release := sync.OnceFunc(func() { close(gate.release) })
			t.Cleanup(release)
			if tc.externalInsert {
				external(client, "insert_message", []byte{1}, 2)
			} else {
				local("insert_message", []byte{1})
			}
			select {
			case <-gate.started:
			case <-ctx.Done():
				t.Fatal("insert delivery did not reach gate")
			}
			// Admission during queued delivery must include the inserted row once,
			// followed only by commits evaluated after admission.
			late := dial()
			lateCache := subscribe(late, 1)
			if tc.externalInsert {
				local("delete_message", []byte{1})
			} else {
				external(client, "delete_message", []byte{1}, 3)
			}
			select {
			case <-deleted:
			case <-ctx.Done():
				t.Fatal("delete reducer did not run")
			}
			local("barrier", nil) // The delete has committed while the insert remains paused.
			type received struct {
				msg any
				err error
			}
			first := make(chan received, 1)
			go func() { _, msg, err := client.Read(ctx); first <- received{msg, err} }()
			select {
			case got := <-first:
				t.Fatalf("later update overtook paused insert: %T, %v", got.msg, got.err)
			case <-time.After(50 * time.Millisecond):
			}
			if tc.disconnect {
				if err := client.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			release()
			apply := func(cache map[string]bool, msg any, wantHeavy bool, requestID uint32, inserts, deletes int) {
				var updates []protocol.SubscriptionUpdate
				switch update := msg.(type) {
				case protocol.TransactionUpdate:
					if !wantHeavy || update.ReducerCall.RequestID != requestID {
						t.Fatalf("unexpected heavy: %+v", update)
					}
					committed, ok := update.Status.(protocol.StatusCommitted)
					if !ok {
						t.Fatalf("status = %T", update.Status)
					}
					updates = committed.Update
				case protocol.TransactionUpdateLight:
					if wantHeavy {
						t.Fatal("caller received a light update")
					}
					updates = update.Update
				default:
					t.Fatalf("delta = %T", msg)
				}
				if len(updates) != 1 || updates[0].QueryID != 1 {
					t.Fatalf("updates = %+v", updates)
				}
				ins, err := protocol.DecodeRowList(updates[0].Inserts)
				if err != nil || len(ins) != inserts {
					t.Fatalf("inserts = %d, %v; want %d", len(ins), err, inserts)
				}
				del, err := protocol.DecodeRowList(updates[0].Deletes)
				if err != nil || len(del) != deletes {
					t.Fatalf("deletes = %d, %v; want %d", len(del), err, deletes)
				}
				for _, row := range del {
					if !cache[string(row)] {
						t.Fatal("deleted row absent from cache")
					}
					delete(cache, string(row))
				}
				for _, row := range ins {
					if cache[string(row)] {
						t.Fatal("duplicate insert")
					}
					cache[string(row)] = true
				}
			}
			got := <-first
			if tc.disconnect {
				if got.err == nil {
					t.Fatalf("disconnected caller received %+v", got.msg)
				}
			} else {
				if got.err != nil {
					t.Fatal(got.err)
				}
				apply(cache, got.msg, tc.externalInsert, 2, 1, 0)
				apply(cache, read(client), !tc.externalInsert, 3, 0, 1)
			}
			apply(lateCache, read(late), false, 0, 0, 1)
			if len(cache) != 0 || len(lateCache) != 0 {
				t.Fatalf("caches after delete = %v, %v", cache, lateCache)
			}
			// An ordered empty response flushes earlier deliveries and exposes any
			// duplicate caller envelope or pre-admission delta still on the wire.
			for _, c := range []*protocolclient.Client{client, late} {
				if tc.disconnect && c == client {
					continue
				}
				external(c, "barrier", nil, 4)
				msg := read(c)
				heavy, ok := msg.(protocol.TransactionUpdate)
				if !ok || heavy.ReducerCall.RequestID != 4 {
					t.Fatalf("trailing response = %+v", msg)
				}
				committed, ok := heavy.Status.(protocol.StatusCommitted)
				if !ok || len(committed.Update) != 0 {
					t.Fatalf("barrier status = %+v", heavy.Status)
				}
			}
		})
	}
}

func TestProtocolJoinedSubscriptionConvergesAcrossReconnect(t *testing.T) {
	def := messagesTableDef()
	def.ReadPolicy = schema.ReadPolicy{Access: schema.TableAccessPublic}
	def.Indexes = []schema.IndexDefinition{{Name: "messages_body", Columns: []string{"body"}}}
	rt := buildStartedDeclaredReadRuntimeWithConfig(t, NewModule("joined_reconnect").
		SchemaVersion(1).
		TableDef(def).
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Reducer("replace_message_projected_body", replaceMessageProjectedBodyReducer).
		Reducer("delete_message", deleteMessageByIDReducer).
		Reducer("barrier", func(*schema.ReducerContext, []byte) ([]byte, error) { return nil, nil }),
		Config{DataDir: t.TempDir(), EnableProtocol: true})
	t.Cleanup(func() { _ = rt.Close() })
	server := httptest.NewServer(rt.HTTPHandler())
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	dial := func(token string) *protocolclient.Client {
		t.Helper()
		client, _, err := protocolclient.Dial(ctx, protocolclient.Options{
			URL:   "ws" + strings.TrimPrefix(server.URL, "http") + "/subscribe",
			Token: token, AllowAnonymous: token == "",
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close(ctx) })
		return client
	}
	read := func(client *protocolclient.Client) any {
		t.Helper()
		_, msg, err := client.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}
	decode := func(encoded []byte) [][]byte {
		t.Helper()
		rows, err := protocol.DecodeRowList(encoded)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	multiset := func(encoded []byte) map[string]int {
		rows := make(map[string]int)
		for _, row := range decode(encoded) {
			rows[string(row)]++
		}
		return rows
	}
	const query = "SELECT a.* FROM messages AS a JOIN messages AS b ON a.body = b.body WHERE a.body = :sender"
	subscribe := func(client *protocolclient.Client, wantDistinct, wantCopies int) map[string]int {
		t.Helper()
		if err := client.Send(ctx, protocol.SubscribeSingleMsg{RequestID: 1, QueryID: 1, QueryString: query}); err != nil {
			t.Fatal(err)
		}
		msg := read(client)
		applied, ok := msg.(protocol.SubscribeSingleApplied)
		if !ok || applied.RequestID != 1 || applied.QueryID != 1 || applied.TableName != "messages" {
			t.Fatalf("initial response = %+v, want messages subscription 1", msg)
		}
		cache := multiset(applied.Rows)
		if len(cache) != wantDistinct {
			t.Fatalf("initial distinct rows = %d, want %d", len(cache), wantDistinct)
		}
		for row, copies := range cache {
			if copies != wantCopies {
				t.Fatalf("initial row %x has %d copies, want %d", row, copies, wantCopies)
			}
		}
		return cache
	}
	checkQuery := func(client *protocolclient.Client, cache map[string]int) {
		t.Helper()
		result, err := client.SQLQuery(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Tables) != 1 || result.Tables[0].TableName != "messages" {
			t.Fatalf("query tables = %+v, want messages", result.Tables)
		}
		if want := multiset(result.Tables[0].Rows); !maps.Equal(cache, want) {
			t.Fatalf("live row multiset = %v, fresh query = %v", cache, want)
		}
	}
	concurrentWriters := func(updateArgs []byte, deleteID byte) {
		t.Helper()
		type outcome struct {
			result ReducerResult
			err    error
		}
		start := make(chan struct{})
		results := make(chan outcome, 2)
		for _, call := range []struct {
			name string
			args []byte
		}{
			{"replace_message_projected_body", updateArgs},
			{"delete_message", []byte{deleteID}},
		} {
			go func() {
				<-start
				result, err := rt.CallReducer(ctx, call.name, call.args)
				results <- outcome{result, err}
			}()
		}
		close(start)
		for range 2 {
			select {
			case got := <-results:
				if got.err != nil || got.result.Status != StatusCommitted {
					t.Fatalf("concurrent writer = %+v, %v", got.result, got.err)
				}
			case <-ctx.Done():
				t.Fatal("concurrent writers did not commit while delivery was paused")
			}
		}
	}

	old := dial("")
	identity := old.IdentityToken()
	owner := types.Identity(identity.Identity).Hex()
	hidden := visibilityRuntimeIdentity(0x55).Hex()
	for _, id := range []byte{1, 2, 3} {
		insertMessageWithBody(t, rt, id, owner)
	}
	insertMessageWithBody(t, rt, 9, hidden)
	checkQuery(old, subscribe(old, 3, 3))

	swappable := rt.fanOutSender.(*swappableFanOutSender)
	gate := &callerDeliveryGate{FanOutSender: swappable.Target(), connID: identity.ConnectionID, started: make(chan struct{}), release: make(chan struct{})}
	swappable.SetTarget(gate)
	release := sync.OnceFunc(func() { close(gate.release) })
	t.Cleanup(release)
	insertMessageWithBody(t, rt, 4, owner)
	select {
	case <-gate.started:
	case <-ctx.Done():
		t.Fatal("joined delta did not reach delivery gate")
	}
	// Both writers commit while the old connection's first delta is held.
	concurrentWriters(append([]byte{1, 1}, hidden...), 3)
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}
	client := dial(identity.Token)
	if got := client.IdentityToken(); got.Identity != identity.Identity || got.ConnectionID == identity.ConnectionID {
		t.Fatalf("reconnect identity/connection = %+v, previous = %+v", got, identity)
	}
	// Reuse the query ID. Its fresh snapshot must include all prior commits
	// exactly once, even though their old-connection deltas remain queued.
	cache := subscribe(client, 2, 2)
	checkQuery(client, cache)
	concurrentWriters(append([]byte{1, 1}, owner...), 2)
	if err := client.Send(ctx, protocol.CallReducerMsg{ReducerName: "barrier", RequestID: 100}); err != nil {
		t.Fatal(err)
	}
	release()
	// Exactly two post-admission commits affect this subscription. Counting
	// frames and row multiplicity catches repeated or missing delta application.
	for range 2 {
		msg := read(client)
		delta, ok := msg.(protocol.TransactionUpdateLight)
		if !ok || len(delta.Update) != 1 || delta.Update[0].QueryID != 1 || delta.Update[0].TableName != "messages" {
			t.Fatalf("joined delta = %+v, want subscription 1 light update", msg)
		}
		for _, row := range decode(delta.Update[0].Deletes) {
			key := string(row)
			if cache[key] == 0 {
				t.Fatalf("deleted row %x absent from live multiset", row)
			}
			cache[key]--
			if cache[key] == 0 {
				delete(cache, key)
			}
		}
		for _, row := range decode(delta.Update[0].Inserts) {
			cache[string(row)]++
		}
	}
	// The ordered caller reply establishes that all preceding fan-out has
	// drained, exposing any extra pre-admission or duplicate delta on the wire.
	msg := read(client)
	barrier, ok := msg.(protocol.TransactionUpdate)
	if !ok || barrier.ReducerCall.RequestID != 100 {
		t.Fatalf("trailing response = %+v, want barrier", msg)
	}
	committed, ok := barrier.Status.(protocol.StatusCommitted)
	if !ok || len(committed.Update) != 0 {
		t.Fatalf("barrier status = %+v, want committed without updates", barrier.Status)
	}
	checkQuery(client, cache)
	if _, msg, err := old.Read(ctx); err == nil {
		t.Fatalf("closed connection received stale delivery: %+v", msg)
	}
}

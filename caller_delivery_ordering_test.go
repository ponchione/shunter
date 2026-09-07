package shunter

import (
	"context"
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

package shunter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func BenchmarkDeclaredReadRuntimeSurfaces(b *testing.B) {
	rt := buildDeclaredReadBenchmarkRuntime(b)
	defer rt.Close()

	b.Run("call_query_projection_order_limit", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result, err := rt.CallQuery(context.Background(), "ranked_messages", WithDeclaredReadPermissions("messages:read"))
			if err != nil {
				b.Fatalf("CallQuery: %v", err)
			}
			if len(result.Rows) != 32 {
				b.Fatalf("CallQuery rows = %d, want 32", len(result.Rows))
			}
		}
	})

	connID := types.ConnectionID{0x42}
	b.Run("subscribe_view_projection_order_limit_initial", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			queryID := uint32(i + 1)
			sub, err := rt.SubscribeView(
				context.Background(),
				"live_ranked_messages",
				queryID,
				WithDeclaredReadConnectionID(connID),
				WithDeclaredReadPermissions("messages:subscribe"),
			)
			if err != nil {
				b.Fatalf("SubscribeView: %v", err)
			}
			if len(sub.InitialRows) != 32 {
				b.Fatalf("SubscribeView initial rows = %d, want 32", len(sub.InitialRows))
			}
			b.StopTimer()
			benchmarkUnregisterDeclaredView(b, rt, connID, queryID)
			b.StartTimer()
		}
	})

	b.Run("subscribe_view_count_initial", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			queryID := uint32(i + 1)
			sub, err := rt.SubscribeView(
				context.Background(),
				"live_message_count",
				queryID,
				WithDeclaredReadConnectionID(connID),
				WithDeclaredReadPermissions("messages:subscribe"),
			)
			if err != nil {
				b.Fatalf("SubscribeView aggregate: %v", err)
			}
			if len(sub.InitialRows) != 1 || len(sub.InitialRows[0]) != 1 || sub.InitialRows[0][0].AsUint64() != 128 {
				b.Fatalf("SubscribeView aggregate initial rows = %#v, want count 128", sub.InitialRows)
			}
			b.StopTimer()
			benchmarkUnregisterDeclaredView(b, rt, connID, queryID)
			b.StartTimer()
		}
	})
}

func buildDeclaredReadBenchmarkRuntime(b *testing.B) *Runtime {
	b.Helper()
	rt, err := Build(validChatModule().
		Reducer("insert_message_with_body", insertMessageWithBodyReducer).
		Query(QueryDeclaration{
			Name:        "ranked_messages",
			SQL:         "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 32",
			Permissions: PermissionMetadata{Required: []string{"messages:read"}},
		}).
		View(ViewDeclaration{
			Name:        "live_ranked_messages",
			SQL:         "SELECT id, body AS text FROM messages ORDER BY text DESC, id ASC LIMIT 32",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}).
		View(ViewDeclaration{
			Name:        "live_message_count",
			SQL:         "SELECT COUNT(*) AS n FROM messages",
			Permissions: PermissionMetadata{Required: []string{"messages:subscribe"}},
		}), Config{DataDir: b.TempDir()})
	if err != nil {
		b.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		b.Fatalf("Start: %v", err)
	}
	for i := 0; i < 128; i++ {
		benchmarkInsertMessageWithBody(b, rt, byte(i+1), fmt.Sprintf("body-%03d", 127-i))
	}
	return rt
}

func benchmarkInsertMessageWithBody(b *testing.B, rt *Runtime, id byte, body string) {
	b.Helper()
	args := append([]byte{id}, []byte(body)...)
	res, err := rt.CallReducer(context.Background(), "insert_message_with_body", args)
	if err != nil {
		b.Fatalf("insert benchmark row: %v", err)
	}
	if res.Status != StatusCommitted {
		b.Fatalf("insert benchmark row status = %v, err = %v, want committed", res.Status, res.Error)
	}
}

func benchmarkUnregisterDeclaredView(b *testing.B, rt *Runtime, connID types.ConnectionID, queryID uint32) {
	b.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply := make(chan error, 1)
	cmd := executor.UnregisterSubscriptionSetCmd{
		ConnID:  connID,
		QueryID: queryID,
		Context: ctx,
		Reply: func(_ subscription.SubscriptionSetUnregisterResult, err error) {
			reply <- err
		},
	}
	if err := rt.executor.SubmitWithContext(ctx, cmd); err != nil {
		b.Fatalf("SubmitWithContext unsubscribe: %v", err)
	}
	select {
	case err := <-reply:
		if err != nil {
			b.Fatalf("unsubscribe declared view: %v", err)
		}
	case <-ctx.Done():
		b.Fatalf("unsubscribe declared view timed out: %v", ctx.Err())
	}
}

package executor

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type benchmarkDurability struct{}

func (benchmarkDurability) EnqueueCommitted(types.TxID, *store.Changeset) {}

func (benchmarkDurability) WaitUntilDurable(txID types.TxID) <-chan types.TxID {
	ch := make(chan types.TxID, 1)
	ch <- txID
	close(ch)
	return ch
}

func (benchmarkDurability) FatalError() error { return nil }

type benchmarkSubscriptions struct{}

func (benchmarkSubscriptions) RegisterSet(subscription.SubscriptionSetRegisterRequest, store.CommittedReadView) (subscription.SubscriptionSetRegisterResult, error) {
	return subscription.SubscriptionSetRegisterResult{}, nil
}

func (benchmarkSubscriptions) UnregisterSet(types.ConnectionID, uint32, store.CommittedReadView) (subscription.SubscriptionSetUnregisterResult, error) {
	return subscription.SubscriptionSetUnregisterResult{}, nil
}

func (benchmarkSubscriptions) EvalAndBroadcast(types.TxID, *store.Changeset, store.CommittedReadView, subscription.PostCommitMeta) {
}

func (benchmarkSubscriptions) DrainDroppedClients() []types.ConnectionID { return nil }

func (benchmarkSubscriptions) DisconnectClient(types.ConnectionID) error { return nil }

func BenchmarkExecutorReducerCommitRoundTrip(b *testing.B) {
	builder := schema.NewBuilder()
	builder.SchemaVersion(1)
	builder.TableDef(schema.TableDefinition{
		Name: "events",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "body", Type: types.KindString},
		},
	})
	engine, err := builder.Build(schema.EngineOptions{})
	if err != nil {
		b.Fatalf("Build schema: %v", err)
	}
	reg := engine.Registry()

	committed := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, ok := reg.Table(tid)
		if !ok {
			b.Fatalf("registry missing table %d", tid)
		}
		committed.RegisterTable(tid, store.NewTable(ts))
	}

	var nextID uint64
	reducers := NewReducerRegistry()
	if err := reducers.Register(RegisteredReducer{
		Name: "InsertEvent",
		Handler: types.ReducerHandler(func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
			nextID++
			_, err := ctx.DB.Insert(0, types.ProductValue{
				types.NewUint64(nextID),
				types.NewString("created"),
			})
			return nil, err
		}),
	}); err != nil {
		b.Fatalf("Register reducer: %v", err)
	}
	reducers.Freeze()

	exec := NewExecutor(ExecutorConfig{
		InboxCapacity: 1024,
		Durability:    benchmarkDurability{},
		Subscriptions: benchmarkSubscriptions{},
	}, reducers, committed, reg, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go exec.Run(ctx)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		respCh := make(chan ReducerResponse, 1)
		if err := exec.Submit(CallReducerCmd{
			Request:    ReducerRequest{ReducerName: "InsertEvent", Source: CallSourceExternal},
			ResponseCh: respCh,
		}); err != nil {
			b.Fatalf("Submit: %v", err)
		}
		resp := <-respCh
		if resp.Status != StatusCommitted {
			b.Fatalf("status=%d err=%v, want committed", resp.Status, resp.Error)
		}
	}
}

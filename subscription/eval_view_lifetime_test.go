package subscription

import (
	"iter"
	"sync/atomic"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// Tests in this file pin the read-view subscription-seam read-view lifetime
//: EvalAndBroadcast receives a borrowed store.CommittedReadView
// and MUST NOT let any reference to that view escape past its synchronous
// return. The executor (executor/executor.go:540-541) calls Close on the
// view immediately after EvalAndBroadcast returns, so any post-return use
// would read against a released snapshot RLock.
//
// trackingView wraps a real CommittedReadView and records every method
// invocation that arrives after Close has been called. The test invokes
// EvalAndBroadcast, closes the tracking view on the same goroutine the
// instant the call returns, drains the fan-out inbox, and asserts that no
// tracked method fired against the closed wrapper. Evaluation paths
// covered: join Tier-2 candidate probing (IndexSeek + GetRow on the view)
// and join delta evaluation (IndexSeek via delta_join.go).

type trackingView struct {
	inner      store.CommittedReadView
	closed     atomic.Bool
	calls      atomic.Int64
	callsAfter atomic.Int64
}

func (v *trackingView) tick() {
	v.calls.Add(1)
	if v.closed.Load() {
		v.callsAfter.Add(1)
	}
}

func (v *trackingView) TableScan(id TableID) iter.Seq2[types.RowID, types.ProductValue] {
	v.tick()
	return v.inner.TableScan(id)
}

func (v *trackingView) IndexScan(tid TableID, idx IndexID, val types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	v.tick()
	return v.inner.IndexScan(tid, idx, val)
}

func (v *trackingView) IndexRange(tid TableID, idx IndexID, lo, hi store.Bound) iter.Seq2[types.RowID, types.ProductValue] {
	v.tick()
	return v.inner.IndexRange(tid, idx, lo, hi)
}

func (v *trackingView) IndexSeek(tid TableID, idx IndexID, key store.IndexKey) []types.RowID {
	v.tick()
	return v.inner.IndexSeek(tid, idx, key)
}

func (v *trackingView) GetRow(tid TableID, rid types.RowID) (types.ProductValue, bool) {
	v.tick()
	return v.inner.GetRow(tid, rid)
}

func (v *trackingView) RowCount(tid TableID) int {
	v.tick()
	return v.inner.RowCount(tid)
}

func (v *trackingView) Close() {
	v.closed.Store(true)
}

func TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewString("lhs")}},
		2: {{types.NewUint64(1), types.NewString("rhs")}},
	})
	tv := &trackingView{inner: inner}

	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, tv); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}

	cs := simpleChangeset(1, []types.ProductValue{
		{types.NewUint64(1), types.NewString("lhs2")},
	}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, tv, PostCommitMeta{})
	// Executor calls view.Close() immediately after EvalAndBroadcast returns.
	tv.Close()

	during := tv.calls.Load()
	if during == 0 {
		t.Fatalf("tracking view recorded 0 calls during evaluation; instrument missed the path")
	}

	// Drain inbox — fan-out worker delivery path must never call back into
	// the view. Any delivery hand-off works off the materialized row slices
	// inside FanOutMessage.Fanout.
	msg := <-inbox
	_ = msg

	if n := tv.callsAfter.Load(); n != 0 {
		t.Fatalf("view methods called %d times after Close; EvalAndBroadcast must not leak view past synchronous return", n)
	}
	if total := tv.calls.Load(); total != during {
		t.Fatalf("call count advanced after Close: during=%d total=%d (expected equal)", during, total)
	}
}

func TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(42), types.NewString("seed")}},
	})
	tv := &trackingView{inner: inner}

	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, tv); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}

	cs := simpleChangeset(1, []types.ProductValue{
		{types.NewUint64(42), types.NewString("new")},
	}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, tv, PostCommitMeta{})
	tv.Close()

	<-inbox

	if n := tv.callsAfter.Load(); n != 0 {
		t.Fatalf("view methods called %d times after Close on single-table path", n)
	}
}

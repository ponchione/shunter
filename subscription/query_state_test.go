package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestQueryRegistryCreateAndSubscribe(t *testing.T) {
	r := newQueryRegistry()
	h := hashN(1)
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	r.createQueryState(h, pred, nil)
	r.addSubscriber(h, types.ConnectionID{1}, types.SubscriptionID(10), 0, 0)
	qs := r.getQuery(h)
	if qs == nil {
		t.Fatal("getQuery nil")
	}
	if qs.refCount != 1 {
		t.Fatalf("refCount = %d, want 1", qs.refCount)
	}
}

func TestQueryRegistrySecondSubscriberSharesState(t *testing.T) {
	r := newQueryRegistry()
	h := hashN(1)
	r.createQueryState(h, AllRows{Table: 1}, nil)
	r.addSubscriber(h, types.ConnectionID{1}, types.SubscriptionID(10), 0, 0)
	r.addSubscriber(h, types.ConnectionID{2}, types.SubscriptionID(11), 0, 0)
	if qs := r.getQuery(h); qs.refCount != 2 {
		t.Fatalf("refCount = %d, want 2", qs.refCount)
	}
}

func TestQueryRegistrySameConnectionCanTrackMultipleSubscriptionIDs(t *testing.T) {
	r := newQueryRegistry()
	h := hashN(1)
	c := types.ConnectionID{1}
	r.createQueryState(h, AllRows{Table: 1}, nil)
	r.addSubscriber(h, c, types.SubscriptionID(10), 0, 0)
	r.addSubscriber(h, c, types.SubscriptionID(11), 0, 0)
	qs := r.getQuery(h)
	if qs.refCount != 2 {
		t.Fatalf("refCount = %d, want 2", qs.refCount)
	}
	if got := len(qs.subscribers[c]); got != 2 {
		t.Fatalf("same-connection subscriber set = %d, want 2", got)
	}
	if subs := r.subscriptionsForConn(c); len(subs) != 2 {
		t.Fatalf("subscriptionsForConn = %v, want 2", subs)
	}
}

func TestQueryRegistryRemoveNotLast(t *testing.T) {
	r := newQueryRegistry()
	h := hashN(1)
	r.createQueryState(h, AllRows{Table: 1}, nil)
	r.addSubscriber(h, types.ConnectionID{1}, types.SubscriptionID(10), 0, 0)
	r.addSubscriber(h, types.ConnectionID{2}, types.SubscriptionID(11), 0, 0)
	_, last, ok := r.removeSubscriber(types.ConnectionID{1}, types.SubscriptionID(10))
	if !ok {
		t.Fatal("removeSubscriber should have succeeded")
	}
	if last {
		t.Fatal("last should be false")
	}
}

func TestQueryRegistryRemoveLast(t *testing.T) {
	r := newQueryRegistry()
	h := hashN(1)
	r.createQueryState(h, AllRows{Table: 1}, nil)
	r.addSubscriber(h, types.ConnectionID{1}, types.SubscriptionID(10), 0, 0)
	_, last, ok := r.removeSubscriber(types.ConnectionID{1}, types.SubscriptionID(10))
	if !ok || !last {
		t.Fatalf("removeLast got last=%v ok=%v", last, ok)
	}
}

func TestQueryRegistryReverseLookupCleared(t *testing.T) {
	r := newQueryRegistry()
	h := hashN(1)
	r.createQueryState(h, AllRows{Table: 1}, nil)
	r.addSubscriber(h, types.ConnectionID{1}, types.SubscriptionID(10), 0, 0)
	r.removeSubscriber(types.ConnectionID{1}, types.SubscriptionID(10))
	if _, ok := r.bySub[subscriptionRef{connID: types.ConnectionID{1}, subID: types.SubscriptionID(10)}]; ok {
		t.Fatal("bySub not cleared")
	}
	if subs := r.subscriptionsForConn(types.ConnectionID{1}); len(subs) != 0 {
		t.Fatalf("byConn not cleared: %v", subs)
	}
}

func TestQueryRegistrySubscriptionsForConn(t *testing.T) {
	r := newQueryRegistry()
	hA := hashN(1)
	hB := hashN(2)
	r.createQueryState(hA, AllRows{Table: 1}, nil)
	r.createQueryState(hB, AllRows{Table: 2}, nil)
	c := types.ConnectionID{1}
	r.addSubscriber(hA, c, types.SubscriptionID(10), 0, 0)
	r.addSubscriber(hB, c, types.SubscriptionID(11), 0, 0)
	subs := r.subscriptionsForConn(c)
	if len(subs) != 2 {
		t.Fatalf("subscriptionsForConn = %v, want 2", subs)
	}
}

func TestQueryRegistryGetQueryMissing(t *testing.T) {
	r := newQueryRegistry()
	if qs := r.getQuery(hashN(5)); qs != nil {
		t.Fatal("missing query should return nil")
	}
}

func TestQueryRegistryRemoveUnknown(t *testing.T) {
	r := newQueryRegistry()
	_, _, ok := r.removeSubscriber(types.ConnectionID{1}, types.SubscriptionID(99))
	if ok {
		t.Fatal("removing unknown subID should fail")
	}
}

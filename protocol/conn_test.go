package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestSubscriptionTrackerReserve(t *testing.T) {
	tr := NewSubscriptionTracker()
	if err := tr.Reserve(1); err != nil {
		t.Fatal(err)
	}
	if !tr.IsActiveOrPending(1) {
		t.Error("reserved id should be active-or-pending")
	}
}

func TestSubscriptionTrackerDuplicateReserve(t *testing.T) {
	tr := NewSubscriptionTracker()
	if err := tr.Reserve(1); err != nil {
		t.Fatal(err)
	}
	err := tr.Reserve(1)
	if !errors.Is(err, ErrDuplicateSubscriptionID) {
		t.Errorf("got %v, want ErrDuplicateSubscriptionID", err)
	}
	// Even active duplicates are rejected.
	tr.Activate(1)
	err = tr.Reserve(1)
	if !errors.Is(err, ErrDuplicateSubscriptionID) {
		t.Errorf("active duplicate: got %v, want ErrDuplicateSubscriptionID", err)
	}
}

func TestSubscriptionTrackerActivate(t *testing.T) {
	tr := NewSubscriptionTracker()
	_ = tr.Reserve(5)
	tr.Activate(5)
	st, ok := tr.state(5)
	if !ok || st != SubActive {
		t.Errorf("state = %v ok=%v, want SubActive,true", st, ok)
	}
}

func TestSubscriptionTrackerRemove(t *testing.T) {
	tr := NewSubscriptionTracker()
	_ = tr.Reserve(7)
	tr.Activate(7)
	if err := tr.Remove(7); err != nil {
		t.Fatal(err)
	}
	if tr.IsActiveOrPending(7) {
		t.Error("removed id should not be active-or-pending")
	}
}

func TestSubscriptionTrackerRemoveMissing(t *testing.T) {
	tr := NewSubscriptionTracker()
	err := tr.Remove(42)
	if !errors.Is(err, ErrSubscriptionNotFound) {
		t.Errorf("got %v, want ErrSubscriptionNotFound", err)
	}
}

func TestSubscriptionTrackerRemoveAll(t *testing.T) {
	tr := NewSubscriptionTracker()
	_ = tr.Reserve(1)
	_ = tr.Reserve(2)
	_ = tr.Reserve(3)
	got := tr.RemoveAll()
	if len(got) != 3 {
		t.Fatalf("RemoveAll returned %d ids, want 3", len(got))
	}
	// After RemoveAll, no ids remain.
	for _, id := range []uint32{1, 2, 3} {
		if tr.IsActiveOrPending(id) {
			t.Errorf("id %d still tracked after RemoveAll", id)
		}
	}
}

func TestConnOutboundChCapacity(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 8
	c := NewConn(types.ConnectionID{}, types.Identity{}, "", false, nil, &opts)
	if cap(c.OutboundCh) != 8 {
		t.Errorf("OutboundCh cap = %d, want 8", cap(c.OutboundCh))
	}
}

func TestConnManagerAddGetRemove(t *testing.T) {
	m := NewConnManager()
	var id types.ConnectionID
	id[0] = 1
	opts := DefaultProtocolOptions()
	c := NewConn(id, types.Identity{}, "", false, nil, &opts)

	m.Add(c)
	got := m.Get(id)
	if got != c {
		t.Errorf("Get returned %p, want %p", got, c)
	}

	m.Remove(id)
	if m.Get(id) != nil {
		t.Error("Remove did not clear the entry")
	}
}

func TestConnManagerGetMissingReturnsNil(t *testing.T) {
	m := NewConnManager()
	var id types.ConnectionID
	if m.Get(id) != nil {
		t.Error("Get on empty manager should return nil")
	}
}

package protocol

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

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

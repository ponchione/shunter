package protocol

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

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

func TestNewConnNilOptionsUsesDefaults(t *testing.T) {
	c := NewConn(types.ConnectionID{}, types.Identity{}, "", false, nil, nil)
	defaults := DefaultProtocolOptions()
	if cap(c.OutboundCh) != defaults.OutgoingBufferMessages {
		t.Fatalf("OutboundCh cap = %d, want %d", cap(c.OutboundCh), defaults.OutgoingBufferMessages)
	}
	if cap(c.inflightSem) != defaults.IncomingQueueMessages {
		t.Fatalf("inflightSem cap = %d, want %d", cap(c.inflightSem), defaults.IncomingQueueMessages)
	}
	if c.opts == nil {
		t.Fatal("Conn opts is nil")
	}
	if c.ProtocolVersion != CurrentProtocolVersion {
		t.Fatalf("ProtocolVersion = %s, want %s", c.ProtocolVersion, CurrentProtocolVersion)
	}
}

func TestNewConnZeroOptionsNormalizeToDefaults(t *testing.T) {
	opts := ProtocolOptions{}
	c := NewConn(types.ConnectionID{}, types.Identity{}, "", false, nil, &opts)
	defaults := DefaultProtocolOptions()
	if cap(c.OutboundCh) != defaults.OutgoingBufferMessages {
		t.Fatalf("OutboundCh cap = %d, want %d", cap(c.OutboundCh), defaults.OutgoingBufferMessages)
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

func TestConnManagerRejectsZeroConnectionID(t *testing.T) {
	m := NewConnManager()
	opts := DefaultProtocolOptions()
	conn := NewConn(types.ConnectionID{}, types.Identity{}, "", false, nil, &opts)

	if err := m.Add(conn); err != ErrZeroConnectionID {
		t.Fatalf("Add zero connection err = %v, want ErrZeroConnectionID", err)
	}
	if err := m.reserve(conn); err != ErrZeroConnectionID {
		t.Fatalf("reserve zero connection err = %v, want ErrZeroConnectionID", err)
	}
	if got := m.ActiveCount(); got != 0 {
		t.Fatalf("ActiveCount = %d, want 0", got)
	}
	if got := m.AcceptedCount(); got != 0 {
		t.Fatalf("AcceptedCount = %d, want 0", got)
	}
}

func TestConnManagerGetMissingReturnsNil(t *testing.T) {
	m := NewConnManager()
	var id types.ConnectionID
	if m.Get(id) != nil {
		t.Error("Get on empty manager should return nil")
	}
}

func TestConnManagerConcurrentAddGetRemoveShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xc011ec72)
		workers    = 6
		iterations = 96
	)
	m := NewConnManager()
	opts := DefaultProtocolOptions()
	start := make(chan struct{})
	failures := make(chan string, workers*iterations)

	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				id := connManagerSoakID(worker, op)
				conn := NewConn(id, types.Identity{}, "", false, nil, &opts)
				m.Add(conn)
				if got := m.Get(id); got != conn {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=Add+Get observed=%p expected=%p",
						seed, worker, op, workers, iterations, got, conn)
					return
				}
				probeID := connManagerSoakID((worker+1)%workers, (op*17+int(seed))%iterations)
				if got := m.Get(probeID); got != nil && got.ID != probeID {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=probe-Get observed_id=%x expected_id=%x",
						seed, worker, op, workers, iterations, got.ID[:], probeID[:])
					return
				}
				m.Remove(id)
				if got := m.Get(id); got != nil {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=Remove+Get observed=%p expected=nil",
						seed, worker, op, workers, iterations, got)
					return
				}
				if (int(seed)+worker+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}

	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("seed=%#x op=wait runtime_config=workers=%d/iterations=%d operation=wait observed=timeout expected=all-workers-finished",
			seed, workers, iterations)
	}
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func connManagerSoakID(worker, op int) types.ConnectionID {
	var id types.ConnectionID
	id[0] = byte(worker + 1)
	id[1] = byte(op + 1)
	id[2] = byte((worker+1)*17 + op*3)
	id[3] = byte((op + 1) * 11)
	return id
}

package subscription

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

type subscriptionMetricObserver struct {
	mu             sync.Mutex
	active         []int
	evalResults    []string
	evalErrors     int
	fanoutReasons  []string
	droppedReasons []string
}

func (o *subscriptionMetricObserver) LogSubscriptionEvalError(types.TxID, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.evalErrors++
}

func (o *subscriptionMetricObserver) LogSubscriptionFanoutError(reason string, _ *types.ConnectionID, _ error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.fanoutReasons = append(o.fanoutReasons, reason)
}

func (o *subscriptionMetricObserver) LogSubscriptionClientDropped(reason string, _ *types.ConnectionID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.droppedReasons = append(o.droppedReasons, reason)
}

func (o *subscriptionMetricObserver) LogProtocolBackpressure(string, string) {}

func (o *subscriptionMetricObserver) RecordSubscriptionActive(active int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.active = append(o.active, active)
}

func (o *subscriptionMetricObserver) RecordSubscriptionEvalDuration(result string, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.evalResults = append(o.evalResults, result)
}

func TestSubscriptionMetricsActiveGaugeRegisterUnregisterDisconnect(t *testing.T) {
	observer := &subscriptionMetricObserver{}
	s := testSchema()
	mgr := NewManager(s, s, WithObserver(observer))
	conn1 := cid(1)
	conn2 := cid(2)

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: conn1, QueryID: 10, Predicates: []Predicate{AllRows{Table: 1}}}, nil); err != nil {
		t.Fatalf("RegisterSet conn1: %v", err)
	}
	observer.requireActive(t, 1)
	if _, err := mgr.UnregisterSet(conn1, 10, nil); err != nil {
		t.Fatalf("UnregisterSet conn1: %v", err)
	}
	observer.requireActive(t, 0)

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: conn2, QueryID: 11, Predicates: []Predicate{AllRows{Table: 1}}}, nil); err != nil {
		t.Fatalf("RegisterSet conn2 query 11: %v", err)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: conn2, QueryID: 12, Predicates: []Predicate{AllRows{Table: 2}}}, nil); err != nil {
		t.Fatalf("RegisterSet conn2 query 12: %v", err)
	}
	if err := mgr.DisconnectClient(conn2); err != nil {
		t.Fatalf("DisconnectClient: %v", err)
	}
	observer.requireActive(t, 0)
}

func TestSubscriptionMetricsEvalDurationRecordsErrorResult(t *testing.T) {
	observer := &subscriptionMetricObserver{}
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox), WithObserver(observer))
	conn := cid(1)
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: conn, QueryID: 10, RequestID: 7, Predicates: []Predicate{AllRows{Table: 1}}}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	mgr.schema = nil
	mgr.EvalAndBroadcast(1, simpleChangeset(1, []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}, nil), nil, PostCommitMeta{})
	observer.requireEvalResult(t, "error")
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if observer.evalErrors != 1 {
		t.Fatalf("eval error logs = %d, want 1", observer.evalErrors)
	}
}

func TestSubscriptionMetricsFanoutAndDroppedCounters(t *testing.T) {
	observer := &subscriptionMetricObserver{}
	worker := NewFanOutWorkerWithObserver(nil, &mockFanOutSender{sendErr: ErrSendBufferFull}, func(types.ConnectionID) {}, observer)
	worker.deliver(context.Background(), FanOutMessage{
		TxID: 1,
		Fanout: CommitFanout{
			cid(1): {{SubscriptionID: 1, TableName: "t1"}},
		},
	})
	observer.requireFanoutReason(t, "buffer_full")
	observer.requireDroppedReason(t, "buffer_full")
}

func (o *subscriptionMetricObserver) requireActive(t *testing.T, want int) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.active {
		if got == want {
			return
		}
	}
	t.Fatalf("missing active gauge %d in %v", want, o.active)
}

func (o *subscriptionMetricObserver) requireEvalResult(t *testing.T, want string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.evalResults {
		if got == want {
			return
		}
	}
	t.Fatalf("missing eval result %q in %v", want, o.evalResults)
}

func (o *subscriptionMetricObserver) requireFanoutReason(t *testing.T, want string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.fanoutReasons {
		if got == want {
			return
		}
	}
	t.Fatalf("missing fanout reason %q in %v", want, o.fanoutReasons)
}

func (o *subscriptionMetricObserver) requireDroppedReason(t *testing.T, want string) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.droppedReasons {
		if got == want {
			return
		}
	}
	t.Fatalf("missing dropped reason %q in %v", want, o.droppedReasons)
}

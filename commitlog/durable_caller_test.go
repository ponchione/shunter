package commitlog

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// Gate the segment buffer flush so fsync cannot run until released.
type callerSyncGate struct {
	file             *os.File
	started, release chan struct{}
	once             sync.Once
	failSync         bool
}

func (g *callerSyncGate) Write(p []byte) (int, error) {
	g.once.Do(func() { close(g.started); <-g.release })
	n, err := g.file.Write(p)
	if err == nil && g.failSync {
		// The flush succeeds, but the following real file.Sync must fail.
		err = g.file.Close()
	}
	return n, err
}

type durableCallerSender struct {
	subscription.FanOutSender
	outcomes chan subscription.CallerOutcome
}

func (s durableCallerSender) SendTransactionUpdateHeavy(_ types.ConnectionID, outcome subscription.CallerOutcome, _ []subscription.SubscriptionUpdate, _ *subscription.EncodingMemo) error {
	s.outcomes <- outcome
	return nil
}

func TestDurableCallerFsyncBoundary(t *testing.T) {
	for _, mode := range []string{"durable", "fsync_failure", "fast"} {
		t.Run(mode, func(t *testing.T) {
			opts := DefaultCommitLogOptions()
			opts.OffsetIndexIntervalBytes, opts.OffsetIndexCap = 0, 0
			dw, err := NewDurabilityWorker(t.TempDir(), 1, opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = dw.Close() })
			gate := &callerSyncGate{file: dw.seg.file, started: make(chan struct{}), release: make(chan struct{}), failSync: mode == "fsync_failure"}
			// The worker is idle until EnqueueCommitted below.
			dw.seg.bw.Reset(gate)
			release := sync.OnceFunc(func() { close(gate.release) })
			t.Cleanup(release)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			sender := durableCallerSender{outcomes: make(chan subscription.CallerOutcome, 2)}
			inbox := make(chan subscription.FanOutMessage, 1)
			worker := subscription.NewFanOutWorker(inbox, sender, nil)
			caller := types.ConnectionID{1}
			worker.SetConfirmedReads(caller, false) // Cannot weaken an explicit durable call.
			done := make(chan struct{})
			go func() { worker.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			flags := subscription.CallerOutcomeFlagDurableSuccess
			if mode == "fast" {
				flags = subscription.CallerOutcomeFlagFullUpdate
			}
			inbox <- subscription.FanOutMessage{
				TxID: 1, TxDurable: dw.WaitUntilDurable(1), CallerConnID: &caller,
				CallerOutcome: &subscription.CallerOutcome{Kind: subscription.CallerOutcomeCommitted, RequestID: 7, Flags: flags, FastReply: true},
			}
			dw.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
			select {
			case <-gate.started:
			case <-ctx.Done():
				t.Fatal("segment flush did not reach gate")
			}
			if dw.DurableTxID() != 0 {
				t.Fatal("durability advanced before fsync")
			}
			if mode != "fast" {
				select {
				case outcome := <-sender.outcomes:
					t.Fatalf("caller outcome before fsync: %+v", outcome)
				case <-time.After(50 * time.Millisecond):
				}
				release()
			}
			select {
			case outcome := <-sender.outcomes:
				want := subscription.CallerOutcomeCommitted
				if gate.failSync {
					want = subscription.CallerOutcomeDurabilityUnknown
				}
				if outcome.Kind != want || outcome.RequestID != 7 {
					t.Fatalf("outcome = %+v, want kind %d for request 7", outcome, want)
				}
				if mode == "durable" && dw.DurableTxID() != 1 {
					t.Fatal("success preceded durable watermark")
				}
				if gate.failSync && dw.FatalError() == nil {
					t.Fatal("expected real fsync failure")
				}
			case <-ctx.Done():
				t.Fatal("caller outcome missing")
			}
			release()
		})
	}
}

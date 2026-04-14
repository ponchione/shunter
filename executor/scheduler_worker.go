package executor

import (
	"context"
	"log"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// Scheduler is the background worker that reads sys_scheduled and
// enqueues due scheduled-reducer calls into the executor inbox
// (SPEC-003 §9.4, Story 6.3).
//
// sys_scheduled is the durable source of truth; the scheduler only
// caches the next wakeup time in memory. On restart, Story 6.5
// replays the table to repopulate the wakeup state.
type Scheduler struct {
	// inbox is the executor's command channel. Due rows are enqueued
	// here as CallReducerCmd with Source = CallSourceScheduled.
	inbox chan<- ExecutorCommand
	// cs is the committed state the scheduler reads via Snapshot for
	// scanning sys_scheduled. Writes to the table go through normal
	// reducer paths (Story 6.2 handle + Story 6.4 firing semantics).
	cs      *store.CommittedState
	tableID schema.TableID
	// wakeup signals the Run loop to rescan immediately. Buffered to
	// cap 1 so Notify() is non-blocking — a pending wakeup already
	// scheduled covers any number of intervening Notify() calls.
	wakeup chan struct{}
	// now is the clock; tests override to simulate time.
	now func() time.Time
	// nextWakeup is the earliest known future next_run_at_ns, set by
	// scan() and consumed by Run() to arm its timer. Zero means "no
	// future schedules currently known."
	nextWakeup time.Time
	// respCh is the dump channel for CallReducerCmd responses that
	// the scheduler owns (no client is waiting for a scheduled
	// reducer's reply). Drained by a goroutine started in Run so the
	// executor never blocks on response delivery.
	respCh chan ReducerResponse
}

// NewScheduler constructs a Scheduler that reads sys_scheduled from
// cs and enqueues CallReducerCmd into inbox. Run(ctx) blocks until
// ctx is cancelled; Notify() triggers an immediate rescan.
func NewScheduler(inbox chan<- ExecutorCommand, cs *store.CommittedState, tableID schema.TableID) *Scheduler {
	return &Scheduler{
		inbox:   inbox,
		cs:      cs,
		tableID: tableID,
		wakeup:  make(chan struct{}, 1),
		now:     time.Now,
		respCh:  make(chan ReducerResponse, 64),
	}
}

// Run drives the scheduler loop. Returns when ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	// Drain scheduled-reducer responses in the background so the
	// executor's post-commit path never blocks on a full respCh.
	go s.drainResponses(ctx)

	for {
		s.scan()

		var wait <-chan time.Time
		if !s.nextWakeup.IsZero() {
			d := time.Until(s.nextWakeup)
			if d < 0 {
				d = 0
			}
			timer := time.NewTimer(d)
			wait = timer.C
			defer timer.Stop()
		}

		select {
		case <-ctx.Done():
			return
		case <-s.wakeup:
			// rescan
		case <-wait:
			// timer fired, rescan
		}
	}
}

// Notify triggers an immediate rescan. Safe to call from any goroutine
// and from any executor state; the send is non-blocking because the
// wakeup channel has cap 1 — a pending wakeup covers subsequent calls.
func (s *Scheduler) Notify() {
	select {
	case s.wakeup <- struct{}{}:
	default:
	}
}

// scan reads sys_scheduled via a read-locked snapshot, enqueues every
// row with next_run_at_ns <= now, and records the earliest future
// next_run_at_ns into s.nextWakeup.
func (s *Scheduler) scan() {
	now := s.now()
	nowNs := now.UnixNano()
	view := s.cs.Snapshot()
	defer view.Close()

	s.nextWakeup = time.Time{}
	for _, row := range view.TableScan(s.tableID) {
		nextNs := row[SysScheduledColNextRunAtNs].AsInt64()
		if nextNs <= nowNs {
			s.enqueue(row)
			continue
		}
		t := time.Unix(0, nextNs)
		if s.nextWakeup.IsZero() || t.Before(s.nextWakeup) {
			s.nextWakeup = t
		}
	}
}

// enqueue sends a CallReducerCmd for a due schedule row. A blocked
// inbox backpressures the scheduler — acceptable in v1 because the
// executor drains at a much higher rate than schedules can be due.
func (s *Scheduler) enqueue(row types.ProductValue) {
	cmd := CallReducerCmd{
		Request: ReducerRequest{
			ReducerName: row[SysScheduledColReducerName].AsString(),
			Args:        append([]byte(nil), row[SysScheduledColArgs].AsBytes()...),
			Source:      CallSourceScheduled,
			// Caller left zero — scheduled calls have no connection.
		},
		ResponseCh: s.respCh,
	}
	s.inbox <- cmd
}

// drainResponses consumes scheduled-reducer outcomes so the executor's
// post-commit write to respCh never blocks. Failures are logged; no
// caller is listening.
func (s *Scheduler) drainResponses(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case resp := <-s.respCh:
			if resp.Status != StatusCommitted {
				log.Printf("scheduler: reducer outcome status=%d err=%v", resp.Status, resp.Error)
			}
		}
	}
}

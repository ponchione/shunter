package executor

import (
	"context"
	"sync"
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
	// inFlight tracks due schedule firings already enqueued into the
	// executor so Scheduler.Run scans do not enqueue the same timer a
	// second time before the current attempt completes. replayQueued is
	// the subset enqueued during Startup replay; failed replay attempts
	// get one wakeup after completion so retry is not hidden by startup
	// duplicate suppression.
	inFlightMu   sync.Mutex
	inFlight     map[scheduledFireKey]struct{}
	replayQueued map[scheduledFireKey]struct{}
}

type scheduledFireKey struct {
	id             ScheduleID
	intendedFireAt int64
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
	}
}

// Run drives the scheduler loop. Returns when ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		if !s.scanWithContext(ctx) {
			return
		}

		var (
			wait  <-chan time.Time
			timer *time.Timer
		)
		if !s.nextWakeup.IsZero() {
			d := s.nextWakeup.Sub(s.now())
			if d < 0 {
				d = 0
			}
			timer = time.NewTimer(d)
			wait = timer.C
		}

		select {
		case <-ctx.Done():
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			return
		case <-s.wakeup:
			// rescan
		case <-wait:
			// timer fired, rescan
		}
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
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
// next_run_at_ns into s.nextWakeup. Per-row behavior is shared with
// ReplayFromCommitted; callers interested in the observed max
// schedule_id should use ReplayFromCommitted directly.
func (s *Scheduler) scan() {
	_, _ = s.scanAndTrackMaxWithContext(context.Background())
}

func (s *Scheduler) scanWithContext(ctx context.Context) bool {
	_, ok := s.scanAndTrackMaxWithContext(ctx)
	return ok
}

// ReplayFromCommitted is the startup entry point for SPEC-003 §9.2
// persistence: rebuilds the in-memory wakeup cache from sys_scheduled
// and enqueues any rows that are already past due so they fire
// promptly after recovery (Story 6.5). Startup runs before Executor.Run, so
// replay enqueue is bounded by the current inbox capacity; any due rows that do
// not fit remain in sys_scheduled and are picked up by Scheduler.Run's first
// post-startup scan.
//
// Returned: the largest observed schedule_id. Callers reset their
// ScheduleID sequence to maxID+1 so post-replay Schedule() calls don't
// collide with replayed rows.
func (s *Scheduler) ReplayFromCommitted() ScheduleID {
	nowNs := s.now().UnixNano()
	rows := s.snapshotScheduleRows()

	var (
		maxID          ScheduleID
		nextWakeup     time.Time
		inboxSaturated bool
	)
	for _, row := range rows {
		if id := ScheduleID(row[SysScheduledColScheduleID].AsUint64()); id > maxID {
			maxID = id
		}
		nextNs := row[SysScheduledColNextRunAtNs].AsInt64()
		if nextNs <= nowNs {
			if !inboxSaturated {
				if s.tryEnqueue(row) {
					s.markInFlight(row, true)
				} else {
					inboxSaturated = true
				}
			}
			continue
		}
		t := time.Unix(0, nextNs)
		if nextWakeup.IsZero() || t.Before(nextWakeup) {
			nextWakeup = t
		}
	}
	s.nextWakeup = nextWakeup
	return maxID
}

func (s *Scheduler) scanAndTrackMaxWithContext(ctx context.Context) (ScheduleID, bool) {
	nowNs := s.now().UnixNano()
	rows := s.snapshotScheduleRows()

	maxID, nextWakeup, ok := s.scanRows(rows, nowNs, func(row types.ProductValue) bool {
		if s.isInFlight(row) {
			return true
		}
		if !s.enqueueWithContext(ctx, row) {
			return false
		}
		s.markInFlight(row, false)
		return true
	})
	s.nextWakeup = nextWakeup
	return maxID, ok
}

func (s *Scheduler) snapshotScheduleRows() []types.ProductValue {
	view := s.cs.Snapshot()
	defer view.Close()

	var rows []types.ProductValue
	for _, row := range view.TableScan(s.tableID) {
		rows = append(rows, row)
	}
	return rows
}

func (s *Scheduler) scanRows(rows []types.ProductValue, nowNs int64, enqueue func(types.ProductValue) bool) (ScheduleID, time.Time, bool) {
	var (
		maxID      ScheduleID
		nextWakeup time.Time
	)
	for _, row := range rows {
		if id := ScheduleID(row[SysScheduledColScheduleID].AsUint64()); id > maxID {
			maxID = id
		}
		nextNs := row[SysScheduledColNextRunAtNs].AsInt64()
		if nextNs <= nowNs {
			if !enqueue(row) {
				return maxID, nextWakeup, false
			}
			continue
		}
		t := time.Unix(0, nextNs)
		if nextWakeup.IsZero() || t.Before(nextWakeup) {
			nextWakeup = t
		}
	}
	return maxID, nextWakeup, true
}

func (s *Scheduler) tryEnqueue(row types.ProductValue) bool {
	cmd := scheduledCallReducerCommand(row)
	select {
	case s.inbox <- cmd:
		return true
	default:
		return false
	}
}

func (s *Scheduler) enqueueWithContext(ctx context.Context, row types.ProductValue) bool {
	cmd := scheduledCallReducerCommand(row)
	select {
	case s.inbox <- cmd:
		return true
	case <-ctx.Done():
		return false
	}
}

func scheduledCallReducerCommand(row types.ProductValue) CallReducerCmd {
	return CallReducerCmd{
		Request: ReducerRequest{
			ReducerName:    row[SysScheduledColReducerName].AsString(),
			Args:           append([]byte(nil), row[SysScheduledColArgs].AsBytes()...),
			Source:         CallSourceScheduled,
			ScheduleID:     ScheduleID(row[SysScheduledColScheduleID].AsUint64()),
			IntendedFireAt: row[SysScheduledColNextRunAtNs].AsInt64(),
			// Caller left zero — scheduled calls have no connection.
		},
	}
}

func (s *Scheduler) markInFlight(row types.ProductValue, replay bool) {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	if s.inFlight == nil {
		s.inFlight = make(map[scheduledFireKey]struct{})
	}
	key := scheduledFireKeyForRow(row)
	s.inFlight[key] = struct{}{}
	if replay {
		if s.replayQueued == nil {
			s.replayQueued = make(map[scheduledFireKey]struct{})
		}
		s.replayQueued[key] = struct{}{}
	}
}

func (s *Scheduler) isInFlight(row types.ProductValue) bool {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	if len(s.inFlight) == 0 {
		return false
	}
	key := scheduledFireKeyForRow(row)
	_, ok := s.inFlight[key]
	return ok
}

func (s *Scheduler) completeInFlight(id ScheduleID, intendedFireAt int64) (bool, bool) {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	key := scheduledFireKey{id: id, intendedFireAt: intendedFireAt}
	_, wasInFlight := s.inFlight[key]
	_, wasReplayQueued := s.replayQueued[key]
	delete(s.inFlight, key)
	delete(s.replayQueued, key)
	return wasInFlight, wasReplayQueued
}

func scheduledFireKeyForRow(row types.ProductValue) scheduledFireKey {
	return scheduledFireKey{
		id:             ScheduleID(row[SysScheduledColScheduleID].AsUint64()),
		intendedFireAt: row[SysScheduledColNextRunAtNs].AsInt64(),
	}
}

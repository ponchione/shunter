package executor

import (
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// advanceOrDeleteSchedule mutates sys_scheduled atomically with a
// scheduled reducer's writes (Story 6.4, SPEC-003 §9.4):
//   - one-shot (repeat_ns == 0): delete the row
//   - repeating (repeat_ns > 0): advance next_run_at_ns to
//     intended+repeat for fixed-rate semantics (§9.5), independent of
//     how late the firing actually ran
//
// A missing row returns nil: a concurrent Cancel between enqueue and
// firing is acceptable; the reducer still commits (at-least-once).
func (e *Executor) advanceOrDeleteSchedule(tx *store.Transaction, id ScheduleID, intendedNs int64) error {
	target := uint64(id)
	for rowID, row := range tx.ScanTable(e.schedTableID) {
		if row[SysScheduledColScheduleID].AsUint64() != target {
			continue
		}
		repeatNs := row[SysScheduledColRepeatNs].AsInt64()
		if repeatNs == 0 {
			return tx.Delete(e.schedTableID, rowID)
		}
		newRow := row.Copy()
		newRow[SysScheduledColNextRunAtNs] = types.NewInt64(intendedNs + repeatNs)
		_, err := tx.Update(e.schedTableID, rowID, newRow)
		return err
	}
	return nil
}

// newSchedulerHandle builds a fresh SchedulerHandle bound to a reducer's
// transaction. Each reducer call gets its own handle so that mutations
// on sys_scheduled roll back with the reducer.
//
// Story 6.3 will replace the nil timerNotify with a closure that wakes
// the scheduler worker after a successful commit that touched
// sys_scheduled.
func (e *Executor) newSchedulerHandle(tx *store.Transaction) *schedulerHandle {
	return &schedulerHandle{
		tx:      tx,
		tableID: e.schedTableID,
		seq:     e.schedSeq,
	}
}

// schedulerHandle is the per-reducer SchedulerHandle implementation
// (SPEC-003 §9.3). It mutates sys_scheduled through the active
// transaction so that Schedule/ScheduleRepeat/Cancel roll back with
// the surrounding reducer (Story 6.2).
//
// The timer-notify hook is called by the post-commit pipeline once a
// commit that touched sys_scheduled has been observed — Story 6.3
// wires it to the scheduler worker. nil means "no timer yet."
type schedulerHandle struct {
	tx          *store.Transaction
	tableID     schema.TableID
	seq         *store.Sequence
	timerNotify func()
}

// Schedule inserts a one-shot scheduled-reducer row into sys_scheduled.
func (h *schedulerHandle) Schedule(reducerName string, args []byte, at time.Time) (ScheduleID, error) {
	return h.insertSchedule(reducerName, args, at.UnixNano(), 0)
}

// ScheduleRepeat inserts a repeating scheduled-reducer row. The first
// fire is one interval from now; subsequent fires advance by
// interval.Nanoseconds() using fixed-rate semantics (Story 6.4, §9.5).
func (h *schedulerHandle) ScheduleRepeat(reducerName string, args []byte, interval time.Duration) (ScheduleID, error) {
	first := time.Now().Add(interval).UnixNano()
	return h.insertSchedule(reducerName, args, first, interval.Nanoseconds())
}

func (h *schedulerHandle) insertSchedule(reducerName string, args []byte, nextRunAtNs, repeatNs int64) (ScheduleID, error) {
	id := ScheduleID(h.seq.Next())
	row := types.ProductValue{
		SysScheduledColScheduleID:  types.NewUint64(uint64(id)),
		SysScheduledColReducerName: types.NewString(reducerName),
		SysScheduledColArgs:        types.NewBytes(args),
		SysScheduledColNextRunAtNs: types.NewInt64(nextRunAtNs),
		SysScheduledColRepeatNs:    types.NewInt64(repeatNs),
	}
	if _, err := h.tx.Insert(h.tableID, row); err != nil {
		return 0, err
	}
	return id, nil
}

// Cancel removes the schedule row for id. Returns true if a row was
// found and marked for deletion. v1 scans the table; sys_scheduled is
// expected to be small.
func (h *schedulerHandle) Cancel(id ScheduleID) bool {
	target := uint64(id)
	for rowID, row := range h.tx.ScanTable(h.tableID) {
		if row[SysScheduledColScheduleID].AsUint64() != target {
			continue
		}
		if err := h.tx.Delete(h.tableID, rowID); err == nil {
			return true
		}
		return false
	}
	return false
}

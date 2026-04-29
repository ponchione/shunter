package commitlog

import (
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

type RecoveryResumePlan struct {
	SegmentStartTx types.TxID
	NextTxID       types.TxID
	AppendMode     AppendMode
}

// RecoveryTxIDRange describes the inclusive transaction IDs replayed from log
// records during recovery. The zero value means no records were replayed.
type RecoveryTxIDRange struct {
	Start types.TxID
	End   types.TxID
}

// SnapshotSkipReason is the diagnostic reason a snapshot candidate was not
// selected during recovery.
type SnapshotSkipReason string

const (
	// SnapshotSkipPastDurableHorizon means the snapshot was newer than the
	// contiguous durable log horizon.
	SnapshotSkipPastDurableHorizon SnapshotSkipReason = "past_durable_horizon"
	// SnapshotSkipReadFailed means the snapshot candidate could not be read or
	// failed payload validation.
	SnapshotSkipReadFailed SnapshotSkipReason = "read_failed"
)

// SkippedSnapshotReport records one snapshot candidate that recovery skipped.
type SkippedSnapshotReport struct {
	TxID   types.TxID
	Reason SnapshotSkipReason
	Detail string
}

// RecoveryReport captures operator-facing facts observed while recovering
// committed state.
type RecoveryReport struct {
	HasSelectedSnapshot  bool
	SelectedSnapshotTxID types.TxID
	HasDurableLog        bool
	DurableLogHorizon    types.TxID
	ReplayedTxRange      RecoveryTxIDRange
	RecoveredTxID        types.TxID
	ResumePlan           RecoveryResumePlan
	SkippedSnapshots     []SkippedSnapshotReport
	DamagedTailSegments  []SegmentInfo
	SegmentCoverage      []SegmentRange
}

// OpenAndRecover reconstructs committed state from the latest valid snapshot
// plus any durable segment records after that snapshot.
func OpenAndRecover(dir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, error) {
	committed, maxAppliedTxID, _, err := OpenAndRecoverDetailed(dir, reg)
	if err != nil {
		return nil, 0, err
	}
	return committed, maxAppliedTxID, nil
}

// OpenAndRecoverDetailed reconstructs committed state and also returns the
// append-resume plan needed to decide whether durability should reopen the
// active segment or start a fresh next segment.
func OpenAndRecoverDetailed(dir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, RecoveryResumePlan, error) {
	committed, maxAppliedTxID, plan, _, err := OpenAndRecoverWithReport(dir, reg)
	return committed, maxAppliedTxID, plan, err
}

// OpenAndRecoverWithReport reconstructs committed state and returns a
// structured recovery report for diagnostics.
func OpenAndRecoverWithReport(dir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, RecoveryResumePlan, RecoveryReport, error) {
	segments, durableHorizon, err := ScanSegments(dir)
	if err != nil {
		return nil, 0, RecoveryResumePlan{}, RecoveryReport{}, err
	}
	report := RecoveryReport{
		HasDurableLog:       len(segments) > 0,
		DamagedTailSegments: damagedTailSegments(segments),
		SegmentCoverage:     SegmentCoverage(segments),
	}
	if report.HasDurableLog {
		report.DurableLogHorizon = durableHorizon
	}
	if len(segments) == 0 {
		durableHorizon = types.TxID(^uint64(0))
	}

	snapshot, skippedSnapshots, err := selectSnapshotWithReport(dir, durableHorizon, reg)
	report.SkippedSnapshots = skippedSnapshots
	if err != nil {
		return nil, 0, RecoveryResumePlan{}, report, err
	}
	if snapshot == nil && len(segments) > 0 && isEmptyDamagedTail(segments[0]) {
		return nil, 0, RecoveryResumePlan{}, report, ErrTruncatedRecord
	}

	committed := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		tableSchema, ok := reg.Table(tableID)
		if !ok {
			return nil, 0, RecoveryResumePlan{}, report, fmt.Errorf("commitlog: registry missing table %d", tableID)
		}
		committed.RegisterTable(tableID, store.NewTable(tableSchema))
	}

	var replayFrom types.TxID
	if snapshot != nil {
		report.HasSelectedSnapshot = true
		report.SelectedSnapshotTxID = snapshot.TxID
		if err := restoreSnapshot(committed, snapshot); err != nil {
			return nil, 0, RecoveryResumePlan{}, report, err
		}
		replayFrom = snapshot.TxID
	} else if len(segments) == 0 {
		return nil, 0, RecoveryResumePlan{}, report, ErrNoData
	} else if segments[0].StartTx > 1 {
		return nil, 0, RecoveryResumePlan{}, report, ErrMissingBaseSnapshot
	}

	if snapshot != nil {
		if err := validateSnapshotLogBoundary(segments, snapshot.TxID); err != nil {
			return nil, 0, RecoveryResumePlan{}, report, err
		}
	}

	maxAppliedTxID, err := ReplayLog(committed, segments, replayFrom, reg)
	if err != nil {
		return nil, 0, RecoveryResumePlan{}, report, err
	}
	if maxAppliedTxID > replayFrom {
		report.ReplayedTxRange = RecoveryTxIDRange{Start: replayFrom + 1, End: maxAppliedTxID}
	}
	if err := advanceRecoveredSequences(committed); err != nil {
		return nil, 0, RecoveryResumePlan{}, report, err
	}
	if snapshot != nil && maxAppliedTxID < snapshot.TxID {
		maxAppliedTxID = snapshot.TxID
	}
	committed.SetCommittedTxID(maxAppliedTxID)
	report.RecoveredTxID = maxAppliedTxID

	plan, err := planRecoveryResume(segments, maxAppliedTxID)
	if err != nil {
		return nil, 0, RecoveryResumePlan{}, report, err
	}
	report.ResumePlan = plan

	return committed, maxAppliedTxID, plan, report, nil
}

func isEmptyDamagedTail(segment SegmentInfo) bool {
	return segment.AppendMode == AppendByFreshNextSegment && segment.LastTx < segment.StartTx
}

func damagedTailSegments(segments []SegmentInfo) []SegmentInfo {
	var damaged []SegmentInfo
	for _, segment := range segments {
		if segment.AppendMode == AppendByFreshNextSegment {
			damaged = append(damaged, segment)
		}
	}
	return damaged
}

func restoreSnapshot(committed *store.CommittedState, snapshot *SnapshotData) error {
	for _, tableData := range snapshot.Tables {
		table, ok := committed.Table(tableData.TableID)
		if !ok {
			return fmt.Errorf("commitlog: snapshot references unknown table %d", tableData.TableID)
		}
		for _, row := range tableData.Rows {
			if err := table.InsertRow(table.AllocRowID(), row); err != nil {
				return fmt.Errorf("commitlog: restore snapshot table %d: %w", tableData.TableID, err)
			}
		}
	}

	for tableID, next := range snapshot.Sequences {
		table, ok := committed.Table(tableID)
		if !ok {
			return fmt.Errorf("commitlog: snapshot sequence references unknown table %d", tableID)
		}
		if minNext, ok := nextSequenceValueForTable(table); ok && next < minNext {
			return fmt.Errorf("%w: snapshot sequence %d for table %d is below restored next sequence value %d", ErrSnapshot, next, tableID, minNext)
		}
		table.SetSequenceValue(next)
	}
	for tableID, next := range snapshot.NextIDs {
		table, ok := committed.Table(tableID)
		if !ok {
			return fmt.Errorf("commitlog: snapshot next_id references unknown table %d", tableID)
		}
		if next < uint64(table.NextID()) {
			return fmt.Errorf("%w: snapshot next_id %d for table %d is below restored next row ID %d", ErrSnapshot, next, tableID, table.NextID())
		}
		table.SetNextID(types.RowID(next))
	}
	return nil
}

func advanceRecoveredSequences(committed *store.CommittedState) error {
	for _, tableID := range committed.TableIDs() {
		table, ok := committed.Table(tableID)
		if !ok {
			continue
		}
		current, hasSequence := table.SequenceValue()
		if !hasSequence {
			continue
		}
		minNext, ok := nextSequenceValueForTable(table)
		if !ok || minNext <= current {
			continue
		}
		table.SetSequenceValue(minNext)
	}
	return nil
}

func nextSequenceValueForTable(table *store.Table) (uint64, bool) {
	ts := table.Schema()
	sequenceCol := -1
	for i := range ts.Columns {
		if ts.Columns[i].AutoIncrement {
			sequenceCol = i
			break
		}
	}
	if sequenceCol < 0 {
		return 0, false
	}

	maxSeen := uint64(0)
	for _, row := range table.Scan() {
		value, ok := autoIncrementValueAsUint64(row[sequenceCol], ts.Columns[sequenceCol].Type)
		if !ok {
			return 0, false
		}
		if value > maxSeen {
			maxSeen = value
		}
	}
	if maxSeen == ^uint64(0) {
		return maxSeen, true
	}
	return maxSeen + 1, true
}

func autoIncrementValueAsUint64(v types.Value, kind schema.ValueKind) (uint64, bool) {
	switch kind {
	case schema.KindInt8, schema.KindInt16, schema.KindInt32, schema.KindInt64:
		n := v.AsInt64()
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case schema.KindUint8, schema.KindUint16, schema.KindUint32, schema.KindUint64:
		return v.AsUint64(), true
	default:
		return 0, false
	}
}

func validateSnapshotLogBoundary(segments []SegmentInfo, snapshotTxID types.TxID) error {
	for _, segment := range segments {
		if segment.LastTx <= snapshotTxID {
			continue
		}
		expected := snapshotTxID + 1
		if segment.StartTx > expected {
			return &HistoryGapError{
				Expected: uint64(expected),
				Got:      uint64(segment.StartTx),
				Segment:  segment.Path,
			}
		}
		return nil
	}
	return nil
}

func planRecoveryResume(segments []SegmentInfo, maxAppliedTxID types.TxID) (RecoveryResumePlan, error) {
	plan := RecoveryResumePlan{
		SegmentStartTx: maxAppliedTxID + 1,
		NextTxID:       maxAppliedTxID + 1,
		AppendMode:     AppendByFreshNextSegment,
	}
	if len(segments) == 0 {
		return plan, nil
	}

	last := segments[len(segments)-1]
	plan.AppendMode = last.AppendMode
	switch last.AppendMode {
	case AppendInPlace:
		plan.SegmentStartTx = last.StartTx
		plan.NextTxID = maxAppliedTxID + 1
		return plan, nil
	case AppendByFreshNextSegment:
		plan.SegmentStartTx = maxAppliedTxID + 1
		plan.NextTxID = maxAppliedTxID + 1
		return plan, nil
	case AppendForbidden:
		return RecoveryResumePlan{}, fmt.Errorf("commitlog: append forbidden for recovery tail segment %s", last.Path)
	default:
		return RecoveryResumePlan{}, fmt.Errorf("commitlog: unknown append mode %d", last.AppendMode)
	}
}

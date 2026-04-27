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
	segments, durableHorizon, err := ScanSegments(dir)
	if err != nil {
		return nil, 0, RecoveryResumePlan{}, err
	}
	if len(segments) == 0 {
		durableHorizon = types.TxID(^uint64(0))
	}

	snapshot, err := SelectSnapshot(dir, durableHorizon, reg)
	if err != nil {
		return nil, 0, RecoveryResumePlan{}, err
	}

	committed := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		tableSchema, ok := reg.Table(tableID)
		if !ok {
			return nil, 0, RecoveryResumePlan{}, fmt.Errorf("commitlog: registry missing table %d", tableID)
		}
		committed.RegisterTable(tableID, store.NewTable(tableSchema))
	}

	var replayFrom types.TxID
	if snapshot != nil {
		if err := restoreSnapshot(committed, snapshot); err != nil {
			return nil, 0, RecoveryResumePlan{}, err
		}
		replayFrom = snapshot.TxID
	} else if len(segments) == 0 {
		return nil, 0, RecoveryResumePlan{}, ErrNoData
	} else if segments[0].StartTx > 1 {
		return nil, 0, RecoveryResumePlan{}, ErrMissingBaseSnapshot
	}

	maxAppliedTxID, err := ReplayLog(committed, segments, replayFrom, reg)
	if err != nil {
		return nil, 0, RecoveryResumePlan{}, err
	}
	if err := advanceRecoveredSequences(committed); err != nil {
		return nil, 0, RecoveryResumePlan{}, err
	}
	if snapshot != nil && maxAppliedTxID < snapshot.TxID {
		maxAppliedTxID = snapshot.TxID
	}
	committed.SetCommittedTxID(maxAppliedTxID)

	plan, err := planRecoveryResume(segments, maxAppliedTxID)
	if err != nil {
		return nil, 0, RecoveryResumePlan{}, err
	}

	return committed, maxAppliedTxID, plan, nil
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
		table.SetSequenceValue(next)
	}
	for tableID, next := range snapshot.NextIDs {
		table, ok := committed.Table(tableID)
		if !ok {
			return fmt.Errorf("commitlog: snapshot next_id references unknown table %d", tableID)
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

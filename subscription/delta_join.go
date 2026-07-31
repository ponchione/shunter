package subscription

import (
	"context"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// JoinFragments is the fixed 8-fragment output of EvalJoinDeltaFragments.
// Insert fragments: I1..I4 in order. Delete fragments: D1..D4 in order.
// Each fragment is a slice of concatenated (LHS, RHS) joined rows, one per
// matching pair. See SPEC-004 §6.2.
type JoinFragments struct {
	Inserts [4][]types.ProductValue
	Deletes [4][]types.ProductValue
}

// EvalJoinDeltaFragments computes the IVM 4+4 fragments for a two-table join
// subscription. The resolver maps (table, column) → indexID for committed
// lookups. Delta-side lookups use the delta indexes built by NewDeltaView —
// callers must include the join columns in deltaIndexColumns when constructing
// the DeltaView.
func EvalJoinDeltaFragments(dv *DeltaView, join *Join, resolver IndexResolver) JoinFragments {
	f, _ := evalJoinDeltaFragments(context.Background(), dv, join, resolver)
	return f
}

func evalJoinDeltaFragments(ctx context.Context, dv *DeltaView, join *Join, resolver IndexResolver) (JoinFragments, error) {
	var f JoinFragments

	dInsT1 := dv.InsertedRows(join.Left)
	dDelT1 := dv.DeletedRows(join.Left)
	dInsT2 := dv.InsertedRows(join.Right)
	dDelT2 := dv.DeletedRows(join.Right)

	// Insert fragments.
	// I1: dT1(+) join T2'   (drive=dT1(+), probe=committed T2)
	var err error
	f.Inserts[0], err = joinDriveCommitted(ctx, dv, dInsT1, true, join.Left, join.LeftCol,
		join.Right, join.RightCol, join, resolver)
	if err != nil {
		return JoinFragments{}, err
	}
	// I2: T1' join dT2(+)   (drive=dT2(+), probe=committed T1, swap to keep LHS,RHS order)
	f.Inserts[1], err = joinDriveCommittedReversed(ctx, dv, dInsT2, true, join.Right, join.RightCol,
		join.Left, join.LeftCol, join, resolver)
	if err != nil {
		return JoinFragments{}, err
	}
	// I3: dT1(+) join dT2(-)
	f.Inserts[2], err = joinDriveDelta(ctx, dv, dInsT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, false /* probe deletes */, join)
	if err != nil {
		return JoinFragments{}, err
	}
	// I4: dT1(-) join dT2(+)
	f.Inserts[3], err = joinDriveDelta(ctx, dv, dDelT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, true /* probe inserts */, join)
	if err != nil {
		return JoinFragments{}, err
	}

	// Delete fragments.
	// D1: dT1(-) join T2'
	f.Deletes[0], err = joinDriveCommitted(ctx, dv, dDelT1, false, join.Left, join.LeftCol,
		join.Right, join.RightCol, join, resolver)
	if err != nil {
		return JoinFragments{}, err
	}
	// D2: T1' join dT2(-)
	f.Deletes[1], err = joinDriveCommittedReversed(ctx, dv, dDelT2, false, join.Right, join.RightCol,
		join.Left, join.LeftCol, join, resolver)
	if err != nil {
		return JoinFragments{}, err
	}
	// D3: dT1(+) join dT2(+)
	f.Deletes[2], err = joinDriveDelta(ctx, dv, dInsT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, true /* probe inserts */, join)
	if err != nil {
		return JoinFragments{}, err
	}
	// D4: dT1(-) join dT2(-)
	f.Deletes[3], err = joinDriveDelta(ctx, dv, dDelT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, false /* probe deletes */, join)
	if err != nil {
		return JoinFragments{}, err
	}

	return f, nil
}

// joinDriveCommitted iterates the LHS driving slice and probes the committed
// RHS table by join column. Outputs (LHS, RHS)-concatenated rows that pass
// the optional Join.Filter.
func joinDriveCommitted(
	ctx context.Context,
	dv *DeltaView,
	driving []types.ProductValue,
	driveInserted bool,
	lhsTable TableID, lhsCol ColID,
	rhsTable TableID, rhsCol ColID,
	join *Join,
	resolver IndexResolver,
) ([]types.ProductValue, error) {
	return joinDriveCommittedRows(ctx, dv, driving, driveInserted, lhsTable, lhsCol, rhsTable, rhsCol, true, join, resolver)
}

// joinDriveCommittedReversed probes the committed LHS side while driving
// from the RHS delta. Output rows are still emitted in (Left, Right) order.
func joinDriveCommittedReversed(
	ctx context.Context,
	dv *DeltaView,
	driving []types.ProductValue,
	driveInserted bool,
	rhsTable TableID, rhsCol ColID,
	lhsTable TableID, lhsCol ColID,
	join *Join,
	resolver IndexResolver,
) ([]types.ProductValue, error) {
	return joinDriveCommittedRows(ctx, dv, driving, driveInserted, rhsTable, rhsCol, lhsTable, lhsCol, false, join, resolver)
}

func joinDriveCommittedRows(
	ctx context.Context,
	dv *DeltaView,
	driving []types.ProductValue,
	driveInserted bool,
	driveTable TableID, driveCol ColID,
	probeTable TableID, probeCol ColID,
	driveIsLeft bool,
	join *Join,
	resolver IndexResolver,
) ([]types.ProductValue, error) {
	if len(driving) == 0 || resolver == nil || dv.committed == nil {
		return nil, nil
	}
	if probeIdx, ok := resolver.IndexIDForColumn(probeTable, probeCol); ok {
		var out []types.ProductValue
		for _, driveRow := range driving {
			if err := chargeSubscriptionWork(ctx); err != nil {
				return nil, err
			}
			driveValue, ok := rowValue(driveRow, driveCol)
			if !ok {
				continue
			}
			key := store.NewIndexKey(driveValue)
			rowIDs := dv.committed.IndexSeek(probeTable, probeIdx, key)
			for _, rid := range rowIDs {
				if err := chargeSubscriptionWork(ctx); err != nil {
					return nil, err
				}
				probeRow, ok := dv.committed.GetRow(probeTable, rid)
				if !ok {
					continue
				}
				if joined := tryJoinFilterFromDrive(driveRow, driveTable, probeRow, probeTable, driveIsLeft, join); joined != nil {
					if err := chargeDeltaRow(ctx, joined); err != nil {
						return nil, err
					}
					out = append(out, joined)
				}
			}
			if dv.IsEventTable(probeTable) {
				for _, probeRow := range dv.InsertedRows(probeTable) {
					if err := chargeSubscriptionWork(ctx); err != nil {
						return nil, err
					}
					probeValue, ok := rowValue(probeRow, probeCol)
					if !ok || !driveValue.Equal(probeValue) {
						continue
					}
					if joined := tryJoinFilterFromDrive(driveRow, driveTable, probeRow, probeTable, driveIsLeft, join); joined != nil {
						if err := chargeDeltaRow(ctx, joined); err != nil {
							return nil, err
						}
						out = append(out, joined)
					}
				}
			}
		}
		return out, nil
	}
	if dv.hasDeltaIndex(driveTable, driveCol, driveInserted) {
		return joinDriveCommittedByDeltaIndex(ctx, dv, driving, driveInserted, driveTable, driveCol, probeTable, probeCol, driveIsLeft, join)
	}
	return joinDriveCommittedByNestedScan(ctx, dv, driving, driveTable, driveCol, probeTable, probeCol, driveIsLeft, join)
}

// joinDriveCommittedByDeltaIndex handles joins where only the changed side's
// join column is indexed: scan the committed probe table once, then use the
// per-transaction delta index to preserve drive-row output order.
func joinDriveCommittedByDeltaIndex(
	ctx context.Context,
	dv *DeltaView,
	driving []types.ProductValue,
	driveInserted bool,
	driveTable TableID, driveCol ColID,
	probeTable TableID, probeCol ColID,
	driveIsLeft bool,
	join *Join,
) ([]types.ProductValue, error) {
	matchesByDrive := make([][]types.ProductValue, len(driving))
	pending := 0
	err := visitRowsAfter(dv, probeTable, func(probeRow types.ProductValue) error {
		probeValue, ok := rowValue(probeRow, probeCol)
		if !ok {
			return nil
		}
		positions := dv.deltaIndexPositions(driveTable, driveCol, probeValue, driveInserted)
		for _, pos := range positions {
			if err := chargeSubscriptionWork(ctx); err != nil {
				return err
			}
			if pos >= len(driving) {
				continue
			}
			driveRow := driving[pos]
			if _, ok := rowValue(driveRow, driveCol); !ok {
				continue
			}
			if joined := tryJoinFilterFromDrive(driveRow, driveTable, probeRow, probeTable, driveIsLeft, join); joined != nil {
				if err := chargeDeltaRow(ctx, joined); err != nil {
					return err
				}
				matchesByDrive[pos] = append(matchesByDrive[pos], joined)
				pending++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if pending == 0 {
		return nil, nil
	}
	out := make([]types.ProductValue, 0, pending)
	for _, rows := range matchesByDrive {
		out = append(out, rows...)
	}
	return out, nil
}

func joinDriveCommittedByNestedScan(
	ctx context.Context,
	dv *DeltaView,
	driving []types.ProductValue,
	driveTable TableID, driveCol ColID,
	probeTable TableID, probeCol ColID,
	driveIsLeft bool,
	join *Join,
) ([]types.ProductValue, error) {
	var out []types.ProductValue
	for _, driveRow := range driving {
		if err := chargeSubscriptionWork(ctx); err != nil {
			return nil, err
		}
		driveValue, ok := rowValue(driveRow, driveCol)
		if !ok {
			continue
		}
		err := visitRowsAfter(dv, probeTable, func(probeRow types.ProductValue) error {
			if err := chargeSubscriptionWork(ctx); err != nil {
				return err
			}
			probeValue, ok := rowValue(probeRow, probeCol)
			if !ok || !driveValue.Equal(probeValue) {
				return nil
			}
			if joined := tryJoinFilterFromDrive(driveRow, driveTable, probeRow, probeTable, driveIsLeft, join); joined != nil {
				if err := chargeDeltaRow(ctx, joined); err != nil {
					return err
				}
				out = append(out, joined)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func visitRowsAfter(dv *DeltaView, table TableID, visit func(types.ProductValue) error) error {
	if dv == nil || visit == nil {
		return nil
	}
	if dv.committed != nil {
		for _, row := range dv.committed.TableScan(table) {
			if err := visit(row); err != nil {
				return err
			}
		}
	}
	if dv.IsEventTable(table) {
		for _, row := range dv.InsertedRows(table) {
			if err := visit(row); err != nil {
				return err
			}
		}
	}
	return nil
}

func tryJoinFilterFromDrive(
	driveRow types.ProductValue,
	driveTable TableID,
	probeRow types.ProductValue,
	probeTable TableID,
	driveIsLeft bool,
	join *Join,
) types.ProductValue {
	if driveIsLeft {
		return tryJoinFilter(driveRow, driveTable, probeRow, probeTable, join)
	}
	return tryJoinFilter(probeRow, probeTable, driveRow, driveTable, join)
}

// joinDriveDelta iterates the LHS delta driving slice and probes the RHS
// delta (inserts or deletes) using the delta index on the RHS join column.
// probeInserts selects the RHS side: true → insert delta, false → delete delta.
func joinDriveDelta(
	ctx context.Context,
	dv *DeltaView,
	driving []types.ProductValue,
	lhsTable TableID, lhsCol ColID,
	rhsTable TableID, rhsCol ColID,
	probeInserts bool,
	join *Join,
) ([]types.ProductValue, error) {
	if len(driving) == 0 {
		return nil, nil
	}
	var out []types.ProductValue
	for _, lrow := range driving {
		if err := chargeSubscriptionWork(ctx); err != nil {
			return nil, err
		}
		lhsValue, ok := rowValue(lrow, lhsCol)
		if !ok {
			continue
		}
		rhsRows := dv.DeltaIndexScan(rhsTable, rhsCol, lhsValue, probeInserts)
		for _, rrow := range rhsRows {
			if err := chargeSubscriptionWork(ctx); err != nil {
				return nil, err
			}
			if joined := tryJoinFilter(lrow, lhsTable, rrow, rhsTable, join); joined != nil {
				if err := chargeDeltaRow(ctx, joined); err != nil {
					return nil, err
				}
				out = append(out, joined)
			}
		}
	}
	return out, nil
}

// tryJoinFilter applies Join.Filter (if any) to the pair of rows and returns
// a concatenated joined row when the filter passes, or nil.
//
// The filter is evaluated against the whole joined pair so boolean structure is
// preserved when an OR spans the left and right relation. Relation-instance
// aliases still disambiguate self-join leaves.
func tryJoinFilter(lrow types.ProductValue, ltable TableID, rrow types.ProductValue, rtable TableID, join *Join) types.ProductValue {
	if !joinPairMatches(lrow, ltable, rrow, rtable, join) {
		return nil
	}
	joined := make(types.ProductValue, 0, len(lrow)+len(rrow))
	joined = append(joined, lrow...)
	joined = append(joined, rrow...)
	return joined
}

func joinPairMatches(lrow types.ProductValue, ltable TableID, rrow types.ProductValue, rtable TableID, join *Join) bool {
	return join.Filter == nil || MatchJoinPair(join.Filter, ltable, join.LeftAlias, lrow, rtable, join.RightAlias, rrow)
}

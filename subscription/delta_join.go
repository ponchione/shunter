package subscription

import (
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
// callers must include the join columns in activeColumns when constructing
// the DeltaView.
func EvalJoinDeltaFragments(dv *DeltaView, join *Join, resolver IndexResolver) JoinFragments {
	var f JoinFragments

	dInsT1 := dv.InsertedRows(join.Left)
	dDelT1 := dv.DeletedRows(join.Left)
	dInsT2 := dv.InsertedRows(join.Right)
	dDelT2 := dv.DeletedRows(join.Right)

	// Insert fragments.
	// I1: dT1(+) join T2'   (drive=dT1(+), probe=committed T2)
	f.Inserts[0] = joinDriveCommitted(dv, dInsT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, join, resolver)
	// I2: T1' join dT2(+)   (drive=dT2(+), probe=committed T1, swap to keep LHS,RHS order)
	f.Inserts[1] = joinDriveCommittedReversed(dv, dInsT2, join.Right, join.RightCol,
		join.Left, join.LeftCol, join, resolver)
	// I3: dT1(+) join dT2(-)
	f.Inserts[2] = joinDriveDelta(dv, dInsT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, false /* probe deletes */, join)
	// I4: dT1(-) join dT2(+)
	f.Inserts[3] = joinDriveDelta(dv, dDelT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, true /* probe inserts */, join)

	// Delete fragments.
	// D1: dT1(-) join T2'
	f.Deletes[0] = joinDriveCommitted(dv, dDelT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, join, resolver)
	// D2: T1' join dT2(-)
	f.Deletes[1] = joinDriveCommittedReversed(dv, dDelT2, join.Right, join.RightCol,
		join.Left, join.LeftCol, join, resolver)
	// D3: dT1(+) join dT2(+)
	f.Deletes[2] = joinDriveDelta(dv, dInsT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, true /* probe inserts */, join)
	// D4: dT1(-) join dT2(-)
	f.Deletes[3] = joinDriveDelta(dv, dDelT1, join.Left, join.LeftCol,
		join.Right, join.RightCol, false /* probe deletes */, join)

	return f
}

// joinDriveCommitted iterates the LHS driving slice and probes the committed
// RHS table by join column. Outputs (LHS, RHS)-concatenated rows that pass
// the optional Join.Filter.
func joinDriveCommitted(
	dv *DeltaView,
	driving []types.ProductValue,
	lhsTable TableID, lhsCol ColID,
	rhsTable TableID, rhsCol ColID,
	join *Join,
	resolver IndexResolver,
) []types.ProductValue {
	if len(driving) == 0 || resolver == nil || dv.committed == nil {
		return nil
	}
	if rhsIdx, ok := resolver.IndexIDForColumn(rhsTable, rhsCol); ok {
		var out []types.ProductValue
		for _, lrow := range driving {
			if int(lhsCol) >= len(lrow) {
				continue
			}
			key := store.NewIndexKey(lrow[lhsCol])
			rowIDs := dv.committed.IndexSeek(rhsTable, rhsIdx, key)
			for _, rid := range rowIDs {
				rrow, ok := dv.committed.GetRow(rhsTable, rid)
				if !ok {
					continue
				}
				if joined := tryJoinFilter(lrow, lhsTable, rrow, rhsTable, join); joined != nil {
					out = append(out, joined)
				}
			}
		}
		return out
	}
	var out []types.ProductValue
	for _, lrow := range driving {
		if int(lhsCol) >= len(lrow) {
			continue
		}
		for _, rrow := range dv.committed.TableScan(rhsTable) {
			if int(rhsCol) >= len(rrow) || !lrow[lhsCol].Equal(rrow[rhsCol]) {
				continue
			}
			if joined := tryJoinFilter(lrow, lhsTable, rrow, rhsTable, join); joined != nil {
				out = append(out, joined)
			}
		}
	}
	return out
}

// joinDriveCommittedReversed probes the committed LHS side while driving
// from the RHS delta. Output rows are still emitted in (Left, Right) order.
func joinDriveCommittedReversed(
	dv *DeltaView,
	driving []types.ProductValue,
	rhsTable TableID, rhsCol ColID,
	lhsTable TableID, lhsCol ColID,
	join *Join,
	resolver IndexResolver,
) []types.ProductValue {
	if len(driving) == 0 || resolver == nil || dv.committed == nil {
		return nil
	}
	if lhsIdx, ok := resolver.IndexIDForColumn(lhsTable, lhsCol); ok {
		var out []types.ProductValue
		for _, rrow := range driving {
			if int(rhsCol) >= len(rrow) {
				continue
			}
			key := store.NewIndexKey(rrow[rhsCol])
			rowIDs := dv.committed.IndexSeek(lhsTable, lhsIdx, key)
			for _, lid := range rowIDs {
				lrow, ok := dv.committed.GetRow(lhsTable, lid)
				if !ok {
					continue
				}
				if joined := tryJoinFilter(lrow, lhsTable, rrow, rhsTable, join); joined != nil {
					out = append(out, joined)
				}
			}
		}
		return out
	}
	var out []types.ProductValue
	for _, rrow := range driving {
		if int(rhsCol) >= len(rrow) {
			continue
		}
		for _, lrow := range dv.committed.TableScan(lhsTable) {
			if int(lhsCol) >= len(lrow) || !lrow[lhsCol].Equal(rrow[rhsCol]) {
				continue
			}
			if joined := tryJoinFilter(lrow, lhsTable, rrow, rhsTable, join); joined != nil {
				out = append(out, joined)
			}
		}
	}
	return out
}

// joinDriveDelta iterates the LHS delta driving slice and probes the RHS
// delta (inserts or deletes) using the delta index on the RHS join column.
// probeInserts selects the RHS side: true → insert delta, false → delete delta.
func joinDriveDelta(
	dv *DeltaView,
	driving []types.ProductValue,
	lhsTable TableID, lhsCol ColID,
	rhsTable TableID, rhsCol ColID,
	probeInserts bool,
	join *Join,
) []types.ProductValue {
	if len(driving) == 0 {
		return nil
	}
	var out []types.ProductValue
	for _, lrow := range driving {
		if int(lhsCol) >= len(lrow) {
			continue
		}
		rhsRows := dv.DeltaIndexScan(rhsTable, rhsCol, lrow[lhsCol], probeInserts)
		for _, rrow := range rhsRows {
			if joined := tryJoinFilter(lrow, lhsTable, rrow, rhsTable, join); joined != nil {
				out = append(out, joined)
			}
		}
	}
	return out
}

// tryJoinFilter applies Join.Filter (if any) to the pair of rows and returns
// a concatenated joined row when the filter passes, or nil.
//
// The filter is evaluated against the whole joined pair so boolean structure is
// preserved when an OR spans the left and right relation. Relation-instance
// aliases still disambiguate self-join leaves.
func tryJoinFilter(lrow types.ProductValue, ltable TableID, rrow types.ProductValue, rtable TableID, join *Join) types.ProductValue {
	if join.Filter != nil && !MatchJoinPair(join.Filter, ltable, join.LeftAlias, lrow, rtable, join.RightAlias, rrow) {
		return nil
	}
	joined := make(types.ProductValue, 0, len(lrow)+len(rrow))
	joined = append(joined, lrow...)
	joined = append(joined, rrow...)
	return joined
}

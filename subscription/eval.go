package subscription

import (
	"fmt"
	"sort"
	"time"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// EvalAndBroadcast evaluates subscriptions for a committed transaction and
// enqueues fan-out. The read view is borrowed only for this call, and
// caller-bound commits still produce their heavy response when there is no
// subscription work.
func (m *Manager) EvalAndBroadcast(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView, meta PostCommitMeta) {
	hasCaller := meta.CallerConnID != nil && (meta.CallerOutcome != nil || meta.CaptureCallerUpdates != nil)
	nothingToEvaluate := !m.registry.hasActive() || changeset == nil || changeset.IsEmpty()
	if nothingToEvaluate && !hasCaller {
		return
	}
	start := time.Now()
	var (
		fanout CommitFanout
		errs   map[types.ConnectionID][]SubscriptionError
	)
	if !nothingToEvaluate {
		fanout, errs = m.evaluate(txID, changeset, view)
	} else {
		fanout = CommitFanout{}
		errs = make(map[types.ConnectionID][]SubscriptionError)
	}
	evalResult := "ok"
	var evalErr error
	if len(errs) > 0 {
		evalResult = "error"
		evalErr = ErrSubscriptionEval
		durationMicros := uint64(time.Since(start).Microseconds())
		if durationMicros == 0 {
			durationMicros = 1
		}
		for connID, list := range errs {
			for i := range list {
				list[i].TotalHostExecutionDurationMicros = durationMicros
			}
			errs[connID] = list
		}
	}
	recordSubscriptionEvalDuration(m.observer, evalResult, time.Since(start))
	traceSubscriptionEval(m.observer, txID, evalResult, evalErr)
	if meta.CaptureCallerUpdates != nil {
		var callerUpdates []SubscriptionUpdate
		if meta.CallerConnID != nil {
			callerUpdates = fanout[*meta.CallerConnID]
		}
		if len(callerUpdates) > 0 {
			copied := append([]SubscriptionUpdate(nil), callerUpdates...)
			meta.CaptureCallerUpdates(copied)
		} else {
			meta.CaptureCallerUpdates(nil)
		}
	}
	if m.inbox != nil {
		m.sendFanOut(FanOutMessage{
			TxID:          txID,
			TxDurable:     meta.TxDurable,
			Fanout:        fanout,
			Errors:        errs,
			CallerConnID:  meta.CallerConnID,
			CallerOutcome: meta.CallerOutcome,
		})
	}
}

func (m *Manager) sendFanOut(msg FanOutMessage) {
	const retryDelay = time.Millisecond
	for {
		m.fanoutMu.Lock()
		if m.fanoutClosed {
			m.fanoutMu.Unlock()
			return
		}
		select {
		case m.inbox <- msg:
			m.fanoutMu.Unlock()
			return
		default:
		}
		closed := m.fanoutClosedCh
		m.fanoutMu.Unlock()

		timer := time.NewTimer(retryDelay)
		select {
		case <-closed:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

// evaluate is the inner orchestration: build DeltaView, collect candidates,
// evaluate each candidate, and assemble the per-connection fanout.
func (m *Manager) evaluate(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView) (CommitFanout, map[types.ConnectionID][]SubscriptionError) {
	activeCols := m.collectDeltaIndexColumns()
	dv := NewDeltaView(view, changeset, activeCols)
	defer dv.Release()
	candidateScratch := acquireCandidateScratch()
	defer releaseCandidateScratch(candidateScratch)
	candidates := m.collectCandidatesInto(changeset, view, candidateScratch)

	fanout := CommitFanout{}
	errs := make(map[types.ConnectionID][]SubscriptionError)

	for hash := range candidates {
		qs := m.registry.getQuery(hash)
		if qs == nil {
			continue
		}
		updates, err := m.evalQuerySafe(qs, dv)
		if err != nil {
			m.handleEvalError(txID, qs, err, errs)
			continue
		}
		if len(updates) == 0 {
			continue
		}
		for connID, subIDs := range qs.subscribers {
			for subID, delivery := range subIDs {
				for _, u := range updates {
					cloned := u
					cloned.SubscriptionID = subID
					cloned.QueryID = delivery.QueryID
					// Copy the outer update slices per subscriber; row payloads are
					// shared and must remain read-only downstream.
					if len(cloned.Inserts) > 0 {
						cloned.Inserts = append([]types.ProductValue(nil), cloned.Inserts...)
					}
					if len(cloned.Deletes) > 0 {
						cloned.Deletes = append([]types.ProductValue(nil), cloned.Deletes...)
					}
					fanout[connID] = append(fanout[connID], cloned)
				}
			}
		}
	}
	sortFanoutBySubscription(fanout)
	return fanout, errs
}

func sortFanoutBySubscription(fanout CommitFanout) {
	// Evaluation assembles fanout through hash/subscriber maps. Stabilize each
	// connection's payload before protocol delivery so multi-subscription updates
	// follow registration order (internal SubscriptionID allocation order), not
	// Go map iteration order.
	for connID, updates := range fanout {
		if len(updates) < 2 {
			continue
		}
		sort.SliceStable(updates, func(i, j int) bool {
			left, right := updates[i], updates[j]
			if left.SubscriptionID != right.SubscriptionID {
				return left.SubscriptionID < right.SubscriptionID
			}
			if left.TableID != right.TableID {
				return left.TableID < right.TableID
			}
			return left.TableName < right.TableName
		})
		fanout[connID] = updates
	}
}

func (m *Manager) handleEvalError(txID types.TxID, qs *queryState, err error, out map[types.ConnectionID][]SubscriptionError) {
	predRepr := fmt.Sprintf("%#v", qs.predicate)
	wrapped := fmt.Errorf("%w: %v", ErrSubscriptionEval, err)
	if m.observer != nil {
		m.observer.LogSubscriptionEvalError(txID, wrapped)
	}

	dropped := make(map[types.ConnectionID]struct{})
	for connID, subIDs := range qs.subscribers {
		for subID, delivery := range subIDs {
			out[connID] = append(out[connID], SubscriptionError{
				RequestID:      delivery.RequestID,
				SubscriptionID: subID,
				QueryHash:      qs.hash,
				Predicate:      predRepr,
				Message:        wrapped.Error(),
			})
		}
		dropped[connID] = struct{}{}
	}
	for connID := range dropped {
		m.signalDropped(connID)
	}
}

// collectDeltaIndexColumns gathers the active (table, column) pairs required
// by join delta evaluation. Single-table predicate filters are evaluated by
// scanning the transaction's inserted/deleted rows directly, so indexing those
// columns would add per-commit work without a reader.
func (m *Manager) collectDeltaIndexColumns() map[TableID][]ColID {
	out := make(map[TableID][]ColID, len(m.deltaIndexColumns))
	for table, cols := range m.deltaIndexColumns {
		if len(cols) == 0 {
			continue
		}
		list := make([]ColID, 0, len(cols))
		for col := range cols {
			list = append(list, col)
		}
		out[table] = list
	}
	return out
}

func (m *Manager) addDeltaIndexColumns(pred Predicate) {
	m.mutateDeltaIndexColumns(pred, 1)
}

func (m *Manager) removeDeltaIndexColumns(pred Predicate) {
	m.mutateDeltaIndexColumns(pred, -1)
}

func (m *Manager) mutateDeltaIndexColumns(pred Predicate, delta int) {
	if m.deltaIndexColumns == nil {
		m.deltaIndexColumns = make(map[TableID]map[ColID]int)
	}
	walkDeltaIndexColumns(pred, func(t TableID, c ColID) {
		cols, ok := m.deltaIndexColumns[t]
		if !ok {
			if delta <= 0 {
				return
			}
			cols = make(map[ColID]int)
			m.deltaIndexColumns[t] = cols
		}
		cols[c] += delta
		if cols[c] <= 0 {
			delete(cols, c)
		}
		if len(cols) == 0 {
			delete(m.deltaIndexColumns, t)
		}
	})
}

func walkDeltaIndexColumns(pred Predicate, visit func(TableID, ColID)) {
	var walk func(p Predicate)
	walk = func(p Predicate) {
		switch x := p.(type) {
		case And:
			if x.Left != nil {
				walk(x.Left)
			}
			if x.Right != nil {
				walk(x.Right)
			}
		case Or:
			if x.Left != nil {
				walk(x.Left)
			}
			if x.Right != nil {
				walk(x.Right)
			}
		case Join:
			visit(x.Left, x.LeftCol)
			visit(x.Right, x.RightCol)
			if x.Filter != nil {
				walk(x.Filter)
			}
		case CrossJoin:
			if x.Filter != nil {
				walk(x.Filter)
			}
		case MultiJoin:
			if x.Filter != nil {
				walk(x.Filter)
			}
		}
	}
	if pred != nil {
		walk(pred)
	}
}

// collectCandidatesInto walks the changeset and populates the provided scratch
// maps with the union of candidate query hashes across all three pruning tiers
// (SPEC-004 §7.2 step 3 / §7.3). Batched Tier 1 optimization: collect distinct
// values per tracked column, one lookup per distinct value.
func (m *Manager) collectCandidatesInto(cs *store.Changeset, view store.CommittedReadView, st *candidateScratch) map[QueryHash]struct{} {
	cands := st.candidates
	clear(cands)
	for tid, tc := range cs.Tables {
		if tc == nil {
			continue
		}

		// Tier 1: batched value-index lookup.
		m.indexes.Value.ForEachTrackedColumn(tid, func(col ColID) {
			for _, v := range collectDistinctChangedValues(st.distinct, col, tc) {
				m.indexes.Value.ForEachHash(tid, col, v, func(h QueryHash) {
					cands[h] = struct{}{}
				})
			}
		})

		// Tier 1b: batched range-index lookup.
		m.indexes.Range.ForEachTrackedColumn(tid, func(col ColID) {
			for _, v := range collectDistinctChangedValues(st.distinct, col, tc) {
				m.indexes.Range.ForEachHash(tid, col, v, func(h QueryHash) {
					cands[h] = struct{}{}
				})
			}
		})

		// Tier 2: join edges where this table is the LHS.
		addJoinCandidate := func(h QueryHash) {
			cands[h] = struct{}{}
		}
		collectJoinEdgeCandidates(m.indexes, tid, tc.Inserts, view, m.resolver, addJoinCandidate)
		collectJoinEdgeCandidates(m.indexes, tid, tc.Deletes, view, m.resolver, addJoinCandidate)
		collectJoinExistenceDeltaCandidates(m.indexes, tid, tc.Inserts, cs, addJoinCandidate)
		collectJoinExistenceDeltaCandidates(m.indexes, tid, tc.Deletes, cs, addJoinCandidate)

		// Tier 3: table fallback.
		m.indexes.Table.ForEachHash(tid, func(h QueryHash) {
			cands[h] = struct{}{}
		})
	}
	return cands
}

func collectDistinctChangedValues(distinct map[valueKey]Value, col ColID, tc *store.TableChangeset) map[valueKey]Value {
	clear(distinct)
	collectDistinctRows(distinct, col, tc.Inserts)
	collectDistinctRows(distinct, col, tc.Deletes)
	return distinct
}

func collectDistinctRows(distinct map[valueKey]Value, col ColID, rows []types.ProductValue) {
	forEachRowColumnValue(rows, col, func(v Value) {
		k := encodeValueKey(v)
		if _, ok := distinct[k]; !ok {
			distinct[k] = v
		}
	})
}

func forEachRowColumnValue(rows []types.ProductValue, col ColID, fn func(Value)) {
	for _, row := range rows {
		if int(col) < len(row) {
			fn(row[col])
		}
	}
}

// evalQuerySafe wraps evalQuery in a panic recovery so one broken
// subscription does not abort the whole evaluation loop (SPEC-004 §11.1).
func (m *Manager) evalQuerySafe(qs *queryState, dv *DeltaView) (updates []SubscriptionUpdate, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &evalPanic{hash: qs.hash, cause: r}
		}
	}()
	updates = m.evalQuery(qs, dv)
	return updates, nil
}

type evalPanic struct {
	hash  QueryHash
	cause any
}

func (e *evalPanic) Error() string {
	return "subscription: evaluation panic for query " + e.hash.String()
}
func (e *evalPanic) Unwrap() error { return ErrSubscriptionEval }

// evalQuery runs the appropriate delta evaluator for a query state.
// Single-table predicates produce one SubscriptionUpdate per referenced
// table; join predicates produce one SubscriptionUpdate carrying the joined
// rows (TableID = Join.Left by convention). SubscriptionID is filled in by
// the caller because it varies per subscriber.
func (m *Manager) evalQuery(qs *queryState, dv *DeltaView) []SubscriptionUpdate {
	if qs.aggregate != nil {
		return m.evalAggregateQuery(qs, dv)
	}
	switch p := qs.predicate.(type) {
	case Join:
		frags := EvalJoinDeltaFragments(dv, &p, m.resolver)
		lhsWidth := m.schema.ColumnCount(p.Left)
		projectJoinFragments(frags.Inserts[:], lhsWidth, p.ProjectRight)
		projectJoinFragments(frags.Deletes[:], lhsWidth, p.ProjectRight)
		ins, del := ReconcileJoinDelta(frags.Inserts[:], frags.Deletes[:])
		if len(ins) == 0 && len(del) == 0 {
			return nil
		}
		projected := p.ProjectedTable()
		columns := projectionUpdateColumns(m.columnsForUpdate(projected), qs.projection)
		return []SubscriptionUpdate{{
			TableID:   projected,
			TableName: m.schema.TableName(projected),
			Columns:   columns,
			Inserts:   projectRows(ins, qs.projection),
			Deletes:   projectRows(del, qs.projection),
		}}
	case CrossJoin:
		ins, del := evalCrossJoinDelta(dv, p)
		if len(ins) == 0 && len(del) == 0 {
			return nil
		}
		projected := p.ProjectedTable()
		columns := projectionUpdateColumns(m.columnsForUpdate(projected), qs.projection)
		return []SubscriptionUpdate{{
			TableID:   projected,
			TableName: m.schema.TableName(projected),
			Columns:   columns,
			Inserts:   projectRows(ins, qs.projection),
			Deletes:   projectRows(del, qs.projection),
		}}
	case MultiJoin:
		ins, del := evalMultiJoinDelta(dv, p)
		if len(ins) == 0 && len(del) == 0 {
			return nil
		}
		projected := p.ProjectedTable()
		columns := projectionUpdateColumns(m.columnsForUpdate(projected), qs.projection)
		return []SubscriptionUpdate{{
			TableID:   projected,
			TableName: m.schema.TableName(projected),
			Columns:   columns,
			Inserts:   projectRows(ins, qs.projection),
			Deletes:   projectRows(del, qs.projection),
		}}
	default:
		var updates []SubscriptionUpdate
		for _, t := range qs.predicate.Tables() {
			ins, del := EvalSingleTableDelta(dv, qs.predicate, t)
			if len(ins) == 0 && len(del) == 0 {
				continue
			}
			updates = append(updates, SubscriptionUpdate{
				TableID:   t,
				TableName: m.schema.TableName(t),
				Columns:   projectionUpdateColumns(m.columnsForUpdate(t), qs.projection),
				Inserts:   projectRows(ins, qs.projection),
				Deletes:   projectRows(del, qs.projection),
			})
		}
		return updates
	}
}

// projectJoinedRows slices each LHS++RHS-concatenated joined row down to the
// projected side. LHS projection returns row[:lhsWidth]; RHS projection
// returns row[lhsWidth:]. Short rows (malformed width) are skipped rather
// than panicking, mirroring the defensive width checks elsewhere in the
// evaluator. Reference: SubscriptionPlan::subscribed_table_id at
// reference/SpacetimeDB/crates/subscription/src/lib.rs:367.
func projectJoinedRows(rows []types.ProductValue, lhsWidth int, projectRight bool) []types.ProductValue {
	if len(rows) == 0 {
		return rows
	}
	out := make([]types.ProductValue, 0, len(rows))
	for _, row := range rows {
		if lhsWidth <= 0 || len(row) < lhsWidth {
			continue
		}
		if projectRight {
			out = append(out, row[lhsWidth:])
		} else {
			out = append(out, row[:lhsWidth])
		}
	}
	return out
}

func projectJoinFragments(fragments [][]types.ProductValue, lhsWidth int, projectRight bool) {
	for i := range fragments {
		fragments[i] = projectJoinedRows(fragments[i], lhsWidth, projectRight)
	}
}

func evalCrossJoinDelta(dv *DeltaView, p CrossJoin) (inserts, deletes []types.ProductValue) {
	if p.Filter != nil {
		if p.Left != p.Right {
			return evalFilteredCrossJoinDelta(dv, p)
		}
		before := crossJoinProjectedRows(p, projectedRowsBefore(dv, p.Left), projectedRowsBefore(dv, p.Right))
		after := crossJoinProjectedRows(p, tableRowsAfter(dv.CommittedView(), p.Left), tableRowsAfter(dv.CommittedView(), p.Right))
		return diffProjectedRowBags(before, after)
	}
	projectedTable := p.ProjectedTable()
	otherTable := crossJoinOtherTable(p)
	afterProjectedRows := tableRowsAfter(dv.CommittedView(), projectedTable)
	beforeProjectedRows := projectedRowsBefore(dv, projectedTable)
	afterOtherCount := rowCountAfter(dv.CommittedView(), otherTable)
	beforeOtherCount := rowCountBefore(dv, otherTable)
	return diffProjectedRowsWithMultiplicity(beforeProjectedRows, beforeOtherCount, afterProjectedRows, afterOtherCount)
}

func evalFilteredCrossJoinDelta(dv *DeltaView, p CrossJoin) (inserts, deletes []types.ProductValue) {
	leftInserts := dv.InsertedRows(p.Left)
	rightInserts := dv.InsertedRows(p.Right)
	leftDeletes := dv.DeletedRows(p.Left)
	rightDeletes := dv.DeletedRows(p.Right)

	leftAfter := tableRowsAfter(dv.CommittedView(), p.Left)
	rightAfter := tableRowsAfter(dv.CommittedView(), p.Right)
	leftBefore := projectedRowsBefore(dv, p.Left)
	rightBefore := projectedRowsBefore(dv, p.Right)

	leftAfterWithoutInserts := subtractProjectedRowsByKey(leftAfter, leftInserts)
	leftBeforeWithoutDeletes := subtractProjectedRowsByKey(leftBefore, leftDeletes)

	insertFromLeft := crossJoinProjectedRows(p, leftInserts, rightAfter)
	insertFromRight := crossJoinProjectedRows(p, leftAfterWithoutInserts, rightInserts)
	deleteFromLeft := crossJoinProjectedRows(p, leftDeletes, rightBefore)
	deleteFromRight := crossJoinProjectedRows(p, leftBeforeWithoutDeletes, rightDeletes)
	return ReconcileJoinDelta(
		[][]types.ProductValue{insertFromLeft, insertFromRight},
		[][]types.ProductValue{deleteFromLeft, deleteFromRight},
	)
}

func crossJoinProjectedRows(p CrossJoin, leftRows, rightRows []types.ProductValue) []types.ProductValue {
	var rows []types.ProductValue
	for _, leftRow := range leftRows {
		for _, rightRow := range rightRows {
			if !MatchJoinPair(p.Filter, p.Left, p.LeftAlias, leftRow, p.Right, p.RightAlias, rightRow) {
				continue
			}
			if p.ProjectRight {
				rows = append(rows, rightRow)
			} else {
				rows = append(rows, leftRow)
			}
		}
	}
	return rows
}

func crossJoinOtherTable(p CrossJoin) TableID {
	projected := p.ProjectedTable()
	if projected == p.Left {
		return p.Right
	}
	return p.Left
}

func tableRowsAfter(view store.CommittedReadView, table TableID) []types.ProductValue {
	if view == nil {
		return nil
	}
	var rows []types.ProductValue
	for _, row := range view.TableScan(table) {
		rows = append(rows, row)
	}
	return rows
}

func rowCountAfter(view store.CommittedReadView, table TableID) int {
	if view == nil {
		return 0
	}
	return view.RowCount(table)
}

func rowCountBefore(dv *DeltaView, table TableID) int {
	n := rowCountAfter(dv.CommittedView(), table) - len(dv.InsertedRows(table)) + len(dv.DeletedRows(table))
	if n < 0 {
		return 0
	}
	return n
}

func diffProjectedRowsWithMultiplicity(beforeRows []types.ProductValue, beforeMultiplier int, afterRows []types.ProductValue, afterMultiplier int) (inserts, deletes []types.ProductValue) {
	beforeCounts, beforeValues, beforeOrder := countProjectedRowsWithMultiplier(beforeRows, beforeMultiplier)
	afterCounts, afterValues, afterOrder := countProjectedRowsWithMultiplier(afterRows, afterMultiplier)
	for _, key := range afterOrder {
		if afterCounts[key] <= beforeCounts[key] {
			continue
		}
		for n := afterCounts[key] - beforeCounts[key]; n > 0; n-- {
			inserts = append(inserts, afterValues[key])
		}
	}
	for _, key := range beforeOrder {
		if beforeCounts[key] <= afterCounts[key] {
			continue
		}
		for n := beforeCounts[key] - afterCounts[key]; n > 0; n-- {
			deletes = append(deletes, beforeValues[key])
		}
	}
	return inserts, deletes
}

func countProjectedRowsWithMultiplier(rows []types.ProductValue, multiplier int) (map[string]uint64, map[string]types.ProductValue, []string) {
	counts := make(map[string]uint64, len(rows))
	values := make(map[string]types.ProductValue, len(rows))
	var order []string
	if multiplier <= 0 {
		return counts, values, order
	}
	for _, row := range rows {
		key := encodeRowKey(row)
		if _, ok := values[key]; !ok {
			values[key] = row
			order = append(order, key)
		}
		counts[key] += uint64(multiplier)
	}
	return counts, values, order
}

func diffProjectedRowBags(beforeRows, afterRows []types.ProductValue) (inserts, deletes []types.ProductValue) {
	return diffProjectedRowsWithMultiplicity(beforeRows, 1, afterRows, 1)
}

func projectedRowsBefore(dv *DeltaView, table TableID) []types.ProductValue {
	view := dv.CommittedView()
	var current []types.ProductValue
	if view != nil {
		for _, row := range view.TableScan(table) {
			current = append(current, row)
		}
	}
	remaining := subtractProjectedRowsByKey(current, dv.InsertedRows(table))
	remaining = append(remaining, dv.DeletedRows(table)...)
	return remaining
}

func subtractProjectedRowsByKey(current, inserted []types.ProductValue) []types.ProductValue {
	if len(current) == 0 {
		return nil
	}
	if len(inserted) == 0 {
		remaining := make([]types.ProductValue, 0, len(current))
		remaining = append(remaining, current...)
		return remaining
	}
	insertCounts := make(map[string]int, len(inserted))
	for _, row := range inserted {
		insertCounts[encodeRowKey(row)]++
	}
	remaining := make([]types.ProductValue, 0, len(current))
	for _, row := range current {
		key := encodeRowKey(row)
		if insertCounts[key] > 0 {
			insertCounts[key]--
			continue
		}
		remaining = append(remaining, row)
	}
	return remaining
}

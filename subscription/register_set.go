package subscription

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// dropSub removes a single (connID, subID) registry entry and, on
// last-ref, also evicts the query state + pruning-index placement.
// Used both by the set-based unregister path and the eval-error
// self-drop path (handleEvalError).
func (m *Manager) dropSub(connID types.ConnectionID, subID types.SubscriptionID) {
	hash, found := m.registry.hashForSub(connID, subID)
	if !found {
		return
	}
	qs := m.registry.getQuery(hash)
	_, last, _ := m.registry.removeSubscriber(connID, subID)
	if last && qs != nil {
		removeSubscriptionForResolver(m.indexes, qs.predicate, hash, m.resolver)
		m.removeDeltaIndexColumns(qs.predicate)
		m.registry.removeQueryState(hash)
	}
}

type initialRowCollector struct {
	ctx   context.Context
	limit int
	count int
}

func newInitialRowCollector(ctx context.Context, limit int) *initialRowCollector {
	if ctx == nil {
		ctx = context.Background()
	}
	return &initialRowCollector{ctx: ctx, limit: limit}
}

func (c *initialRowCollector) err() error {
	if c == nil || c.ctx == nil {
		return nil
	}
	return c.ctx.Err()
}

func (c *initialRowCollector) add(out *[]types.ProductValue, row types.ProductValue) error {
	if err := c.err(); err != nil {
		return err
	}
	if c.limit > 0 && c.count >= c.limit {
		return fmt.Errorf("%w: cap=%d", ErrInitialRowLimit, c.limit)
	}
	*out = append(*out, row)
	c.count++
	return nil
}

func (m *Manager) initialUpdates(ctx context.Context, pred Predicate, projection []ProjectionColumn, view store.CommittedReadView, subID types.SubscriptionID, queryID uint32) ([]SubscriptionUpdate, error) {
	if view == nil {
		return nil, nil
	}
	switch p := pred.(type) {
	case Join, CrossJoin, MultiJoin:
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var rows []types.ProductValue
		var err error
		switch p := p.(type) {
		case Join:
			rows, err = m.appendProjectedJoinRows(ctx, nil, view, p)
		case CrossJoin:
			rows, err = m.appendProjectedCrossJoinRows(ctx, nil, view, p)
		case MultiJoin:
			rows, err = m.appendProjectedMultiJoinRows(ctx, nil, view, p)
		}
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
		tableID := emittedTableID(pred)
		columns := projectionUpdateColumns(m.columnsForUpdate(tableID), projection)
		return []SubscriptionUpdate{{
			SubscriptionID: subID,
			QueryID:        queryID,
			TableID:        tableID,
			TableName:      m.schema.TableName(tableID),
			Columns:        columns,
			Inserts:        projectRows(rows, projection),
		}}, nil
	default:
		tables := pred.Tables()
		if len(tables) == 0 {
			return nil, nil
		}
		collector := newInitialRowCollector(ctx, m.InitialRowLimit)
		if err := collector.err(); err != nil {
			return nil, err
		}
		updates := make([]SubscriptionUpdate, 0, len(tables))
		for _, tableID := range tables {
			rows, err := m.initialRowsForTable(collector, pred, view, tableID)
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				continue
			}
			updates = append(updates, SubscriptionUpdate{
				SubscriptionID: subID,
				QueryID:        queryID,
				TableID:        tableID,
				TableName:      m.schema.TableName(tableID),
				Columns:        projectionUpdateColumns(m.columnsForUpdate(tableID), projection),
				Inserts:        projectRows(rows, projection),
			})
		}
		return updates, nil
	}
}

func (m *Manager) initialRowsForTable(collector *initialRowCollector, pred Predicate, view store.CommittedReadView, table TableID) ([]types.ProductValue, error) {
	if view == nil {
		return nil, nil
	}
	if err := collector.err(); err != nil {
		return nil, err
	}
	var out []types.ProductValue
	if m.resolver != nil {
		if eq, idxID, ok := initialIndexedEquality(pred, table, m.resolver); ok {
			key := store.NewIndexKey(eq.Value)
			for _, rid := range view.IndexSeek(eq.Table, idxID, key) {
				if err := collector.err(); err != nil {
					return nil, err
				}
				row, ok := view.GetRow(eq.Table, rid)
				if !ok {
					continue
				}
				if MatchRow(pred, table, row) {
					if err := collector.add(&out, row); err != nil {
						return nil, err
					}
				}
			}
			return out, nil
		}
		if r, idxID, ok := initialIndexedRange(pred, table, m.resolver); ok {
			lower := store.Bound{Value: r.Lower.Value, Inclusive: r.Lower.Inclusive, Unbounded: r.Lower.Unbounded}
			upper := store.Bound{Value: r.Upper.Value, Inclusive: r.Upper.Inclusive, Unbounded: r.Upper.Unbounded}
			for _, row := range view.IndexRange(table, idxID, lower, upper) {
				if err := collector.err(); err != nil {
					return nil, err
				}
				if !MatchRow(pred, table, row) {
					continue
				}
				if err := collector.add(&out, row); err != nil {
					return nil, err
				}
			}
			return out, nil
		}
	}
	for _, row := range view.TableScan(table) {
		if err := collector.err(); err != nil {
			return nil, err
		}
		if MatchRow(pred, table, row) {
			if err := collector.add(&out, row); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func initialIndexedEquality(pred Predicate, table TableID, resolver IndexResolver) (ColEq, IndexID, bool) {
	var zeroIdx IndexID
	if resolver == nil {
		return ColEq{}, zeroIdx, false
	}
	switch p := pred.(type) {
	case ColEq:
		if p.Table == table && p.Alias == 0 {
			if idxID, ok := resolver.IndexIDForColumn(p.Table, p.Column); ok {
				return p, idxID, true
			}
		}
	case And:
		if eq, idxID, ok := initialIndexedEquality(p.Left, table, resolver); ok {
			return eq, idxID, true
		}
		if eq, idxID, ok := initialIndexedEquality(p.Right, table, resolver); ok {
			return eq, idxID, true
		}
	}
	return ColEq{}, zeroIdx, false
}

func initialIndexedRange(pred Predicate, table TableID, resolver IndexResolver) (ColRange, IndexID, bool) {
	var zeroIdx IndexID
	if resolver == nil {
		return ColRange{}, zeroIdx, false
	}
	switch p := pred.(type) {
	case ColRange:
		if p.Table == table && p.Alias == 0 {
			if idxID, ok := resolver.IndexIDForColumn(p.Table, p.Column); ok {
				return p, idxID, true
			}
		}
	case And:
		if r, idxID, ok := initialIndexedRange(p.Left, table, resolver); ok {
			return r, idxID, true
		}
		if r, idxID, ok := initialIndexedRange(p.Right, table, resolver); ok {
			return r, idxID, true
		}
	}
	return ColRange{}, zeroIdx, false
}

func (m *Manager) appendProjectedCrossJoinRows(ctx context.Context, out []types.ProductValue, view store.CommittedReadView, p CrossJoin) ([]types.ProductValue, error) {
	if view == nil {
		return out, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	projectedTable := p.ProjectedTable()
	otherTable := crossJoinOtherTable(p)
	otherCount := view.RowCount(otherTable)
	if otherCount == 0 {
		return out, nil
	}
	add := func(row types.ProductValue) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.InitialRowLimit > 0 && len(out) >= m.InitialRowLimit {
			return fmt.Errorf("%w: cap=%d", ErrInitialRowLimit, m.InitialRowLimit)
		}
		out = append(out, row)
		return nil
	}
	if p.Filter != nil {
		for _, leftRow := range view.TableScan(p.Left) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, rightRow := range view.TableScan(p.Right) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if !MatchJoinPair(p.Filter, p.Left, p.LeftAlias, leftRow, p.Right, p.RightAlias, rightRow) {
					continue
				}
				if p.ProjectRight {
					if err := add(rightRow); err != nil {
						return nil, err
					}
				} else if err := add(leftRow); err != nil {
					return nil, err
				}
			}
		}
		return out, nil
	}
	for _, projectedRow := range view.TableScan(projectedTable) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for i := 0; i < otherCount; i++ {
			if err := add(projectedRow); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func (m *Manager) appendProjectedJoinRows(ctx context.Context, out []types.ProductValue, view store.CommittedReadView, p Join) ([]types.ProductValue, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.resolver == nil {
		return nil, fmt.Errorf("%w: manager has no IndexResolver (join=%d.%d=%d.%d)", ErrJoinIndexUnresolved, p.Left, p.LeftCol, p.Right, p.RightCol)
	}
	projectedTable := p.Left
	otherTable := p.Right
	projectedJoinCol := p.LeftCol
	otherJoinCol := p.RightCol
	orientedRows := func(projectedRow, otherRow types.ProductValue) (types.ProductValue, types.ProductValue) {
		if p.ProjectRight {
			return otherRow, projectedRow
		}
		return projectedRow, otherRow
	}
	if p.ProjectRight {
		projectedTable, otherTable = p.Right, p.Left
		projectedJoinCol, otherJoinCol = p.RightCol, p.LeftCol
	}
	otherIdx, hasOtherIdx := m.resolver.IndexIDForColumn(otherTable, otherJoinCol)
	projectedIdx, hasProjectedIdx := m.resolver.IndexIDForColumn(projectedTable, projectedJoinCol)
	if !hasOtherIdx {
		if !hasProjectedIdx {
			return nil, fmt.Errorf("%w: join=%d.%d=%d.%d", ErrJoinIndexUnresolved, p.Left, p.LeftCol, p.Right, p.RightCol)
		}
		return m.appendProjectedJoinRowsFromProjectedIndex(ctx, out, view, p, projectedTable, projectedJoinCol, projectedIdx, otherTable, otherJoinCol, orientedRows)
	}
	projectedCandidates, filterProjected := initialIndexedFilterRowIDs(view, p.Filter, projectedTable, m.resolver)
	otherCandidates, filterOther := initialIndexedFilterRowIDs(view, p.Filter, otherTable, m.resolver)
	if hasProjectedIdx && initialJoinScanCost(view, otherTable, otherCandidates, filterOther) < initialJoinScanCost(view, projectedTable, projectedCandidates, filterProjected) {
		return m.appendProjectedJoinRowsFromProjectedIndex(ctx, out, view, p, projectedTable, projectedJoinCol, projectedIdx, otherTable, otherJoinCol, orientedRows)
	}
	add := func(row types.ProductValue) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.InitialRowLimit > 0 && len(out) >= m.InitialRowLimit {
			return fmt.Errorf("%w: cap=%d", ErrInitialRowLimit, m.InitialRowLimit)
		}
		out = append(out, row)
		return nil
	}
	for projectedRID, projectedRow := range view.TableScan(projectedTable) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !initialRowIDAllowed(projectedCandidates, filterProjected, projectedRID) {
			continue
		}
		if int(projectedJoinCol) >= len(projectedRow) {
			continue
		}
		if hasOtherIdx {
			key := store.NewIndexKey(projectedRow[projectedJoinCol])
			for _, rid := range view.IndexSeek(otherTable, otherIdx, key) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if !initialRowIDAllowed(otherCandidates, filterOther, rid) {
					continue
				}
				otherRow, ok := view.GetRow(otherTable, rid)
				if !ok {
					continue
				}
				leftRow, rightRow := orientedRows(projectedRow, otherRow)
				if tryJoinFilter(leftRow, p.Left, rightRow, p.Right, &p) == nil {
					continue
				}
				if err := add(projectedRow); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

func (m *Manager) appendProjectedJoinRowsFromProjectedIndex(
	ctx context.Context,
	out []types.ProductValue,
	view store.CommittedReadView,
	p Join,
	projectedTable TableID,
	projectedJoinCol ColID,
	projectedIdx IndexID,
	otherTable TableID,
	otherJoinCol ColID,
	orientedRows func(types.ProductValue, types.ProductValue) (types.ProductValue, types.ProductValue),
) ([]types.ProductValue, error) {
	matchesByProjectedRow := make(map[types.RowID][]types.ProductValue)
	pending := 0
	projectedCandidates, filterProjected := initialIndexedFilterRowIDs(view, p.Filter, projectedTable, m.resolver)
	otherCandidates, filterOther := initialIndexedFilterRowIDs(view, p.Filter, otherTable, m.resolver)
	for otherRID, otherRow := range view.TableScan(otherTable) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !initialRowIDAllowed(otherCandidates, filterOther, otherRID) {
			continue
		}
		if int(otherJoinCol) >= len(otherRow) {
			continue
		}
		key := store.NewIndexKey(otherRow[otherJoinCol])
		for _, projectedRID := range view.IndexSeek(projectedTable, projectedIdx, key) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !initialRowIDAllowed(projectedCandidates, filterProjected, projectedRID) {
				continue
			}
			projectedRow, ok := view.GetRow(projectedTable, projectedRID)
			if !ok || int(projectedJoinCol) >= len(projectedRow) {
				continue
			}
			leftRow, rightRow := orientedRows(projectedRow, otherRow)
			if !joinPairMatches(leftRow, p.Left, rightRow, p.Right, &p) {
				continue
			}
			if m.InitialRowLimit > 0 && len(out)+pending >= m.InitialRowLimit {
				return nil, fmt.Errorf("%w: cap=%d", ErrInitialRowLimit, m.InitialRowLimit)
			}
			matchesByProjectedRow[projectedRID] = append(matchesByProjectedRow[projectedRID], projectedRow)
			pending++
		}
	}
	if pending == 0 {
		return out, nil
	}
	for projectedRID := range view.TableScan(projectedTable) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, matchesByProjectedRow[projectedRID]...)
	}
	return out, nil
}

func initialIndexedFilterRowIDs(view store.CommittedReadView, pred Predicate, table TableID, resolver IndexResolver) (map[types.RowID]struct{}, bool) {
	if view == nil || pred == nil || resolver == nil {
		return nil, false
	}
	if eq, idxID, ok := initialIndexedEquality(pred, table, resolver); ok {
		out := make(map[types.RowID]struct{})
		key := store.NewIndexKey(eq.Value)
		for _, rid := range view.IndexSeek(eq.Table, idxID, key) {
			out[rid] = struct{}{}
		}
		return out, true
	}
	if r, idxID, ok := initialIndexedRange(pred, table, resolver); ok {
		out := make(map[types.RowID]struct{})
		lower := store.Bound{Value: r.Lower.Value, Inclusive: r.Lower.Inclusive, Unbounded: r.Lower.Unbounded}
		upper := store.Bound{Value: r.Upper.Value, Inclusive: r.Upper.Inclusive, Unbounded: r.Upper.Unbounded}
		for rid := range view.IndexRange(r.Table, idxID, lower, upper) {
			out[rid] = struct{}{}
		}
		return out, true
	}
	return nil, false
}

func initialJoinScanCost(view store.CommittedReadView, table TableID, candidates map[types.RowID]struct{}, filtered bool) int {
	if filtered {
		return len(candidates)
	}
	return view.RowCount(table)
}

func initialRowIDAllowed(candidates map[types.RowID]struct{}, enabled bool, rid types.RowID) bool {
	if !enabled {
		return true
	}
	_, ok := candidates[rid]
	return ok
}

// emittedTableID returns the table ID whose row shape the subscription emits
// at the wire boundary. Join and CrossJoin carry an explicit projected side;
// every other predicate emits rows from its sole declared table. Zero is
// returned when the predicate carries no table (malformed).
func emittedTableID(p Predicate) TableID {
	switch x := p.(type) {
	case Join:
		return x.ProjectedTable()
	case CrossJoin:
		return x.ProjectedTable()
	case MultiJoin:
		return x.ProjectedTable()
	}
	tables := p.Tables()
	if len(tables) == 0 {
		return 0
	}
	return tables[0]
}

// RegisterSet atomically registers 1..N predicates under a single
// (ConnID, QueryID) key. Reference: add_subscription_multi at
// reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1023.
func (m *Manager) RegisterSet(
	req SubscriptionSetRegisterRequest,
	view store.CommittedReadView,
) (SubscriptionSetRegisterResult, error) {
	if len(req.Predicates) == 0 {
		return SubscriptionSetRegisterResult{}, fmt.Errorf("%w: subscription set requires at least one predicate", ErrInvalidPredicate)
	}
	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	canonicalPreds := make([]Predicate, 0, len(req.Predicates))
	projections := make([][]ProjectionColumn, len(req.Predicates))
	if req.ProjectionColumns != nil {
		if len(req.ProjectionColumns) != len(req.Predicates) {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("projection column set count = %d, want %d", len(req.ProjectionColumns), len(req.Predicates))
		}
		for i := range req.ProjectionColumns {
			projections[i] = copyProjectionColumns(req.ProjectionColumns[i])
		}
	}
	// Pre-validate every predicate before touching registry state.
	for i, p := range req.Predicates {
		if err := ctx.Err(); err != nil {
			return SubscriptionSetRegisterResult{}, err
		}
		if err := ValidatePredicate(p, m.schema); err != nil {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("predicate validation: %w", err)
		}
		canonical := canonicalizePredicate(p)
		if err := ValidatePredicate(canonical, m.schema); err != nil {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("predicate validation: %w", err)
		}
		if err := validateProjectionColumns(canonical, projections[i], m.schema); err != nil {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("projection validation: %w", err)
		}
		canonicalPreds = append(canonicalPreds, canonical)
	}
	var hashIdentities []*types.Identity
	switch {
	case req.PredicateHashIdentities != nil:
		if len(req.PredicateHashIdentities) != len(canonicalPreds) {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("predicate hash identity count = %d, want %d", len(req.PredicateHashIdentities), len(canonicalPreds))
		}
		hashIdentities = req.PredicateHashIdentities
	case req.ClientIdentity != nil:
		hashIdentities = make([]*types.Identity, len(canonicalPreds))
		for i := range hashIdentities {
			hashIdentities[i] = req.ClientIdentity
		}
	default:
		hashIdentities = make([]*types.Identity, len(canonicalPreds))
	}
	// Duplicate QueryID rejection.
	if byQ, ok := m.querySets[req.ConnID]; ok {
		if _, live := byQ[req.QueryID]; live {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("%w: conn=%x query=%d",
				ErrQueryIDAlreadyLive, req.ConnID[:4], req.QueryID)
		}
	}
	// Dedup identical predicates within this call.
	deduped := make([]Predicate, 0, len(canonicalPreds))
	dedupedProjections := make([][]ProjectionColumn, 0, len(canonicalPreds))
	dedupedHashIdentities := make([]*types.Identity, 0, len(canonicalPreds))
	seen := make(map[QueryHash]struct{}, len(canonicalPreds))
	for i, p := range canonicalPreds {
		h := ComputeQueryPlanHash(p, projections[i], hashIdentities[i])
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		deduped = append(deduped, p)
		dedupedProjections = append(dedupedProjections, projections[i])
		dedupedHashIdentities = append(dedupedHashIdentities, hashIdentities[i])
	}
	if uint64(len(deduped)) > uint64(^types.SubscriptionID(0))-uint64(m.nextSubID) {
		return SubscriptionSetRegisterResult{}, fmt.Errorf("%w: next=%d requested=%d", ErrSubscriptionIDOverflow, m.nextSubID, len(deduped))
	}
	// Allocate internal IDs + run initial snapshot per predicate.
	allocated := make([]types.SubscriptionID, 0, len(deduped))
	updates := make([]SubscriptionUpdate, 0, len(deduped))
	for i, p := range deduped {
		m.nextSubID++
		subID := m.nextSubID
		projection := dedupedProjections[i]
		hash := ComputeQueryPlanHash(p, projection, dedupedHashIdentities[i])
		// Reusing this hash on the same connection skips a duplicate initial
		// snapshot but still allocates an internal subscription ID.
		existing := m.registry.getQuery(hash)
		sameConnReuse := existing != nil && len(existing.subscribers[req.ConnID]) > 0
		var initial []SubscriptionUpdate
		if !sameConnReuse {
			var err error
			initial, err = m.initialUpdates(ctx, p, projection, view, subID, req.QueryID)
			if err != nil {
				// Unwind any partial state. dropSub handles registry maps + PruningIndexes
				// eviction on last-ref; each allocated sub is dropped independently.
				for _, sid := range allocated {
					m.dropSub(req.ConnID, sid)
				}
				return SubscriptionSetRegisterResult{}, fmt.Errorf("%w: %w", ErrInitialQuery, err)
			}
		}
		qs := existing
		if qs == nil {
			qs = m.registry.createQueryState(hash, p, projection)
			// SQLText is set only when the admission path knows the original
			// SQL string (Single subscribe). Multi leaves it empty —
			// reference `module_subscription_actor.rs:836` uses raw
			// `return_on_err!` on the unsubscribe path and does not apply
			// the `DBError::WithSql` suffix.
			qs.sqlText = req.SQLText
			placeSubscriptionForResolver(m.indexes, p, hash, m.resolver)
			m.addDeltaIndexColumns(p)
		}
		m.registry.addSubscriber(hash, req.ConnID, subID, req.RequestID, req.QueryID)
		allocated = append(allocated, subID)
		_ = qs
		updates = append(updates, initial...)
	}
	if m.querySets[req.ConnID] == nil {
		m.querySets[req.ConnID] = make(map[uint32][]types.SubscriptionID)
	}
	m.querySets[req.ConnID][req.QueryID] = allocated
	active := m.activeSets.Add(1)
	recordSubscriptionActive(m.observer, int(active))
	return SubscriptionSetRegisterResult{QueryID: req.QueryID, Update: updates}, nil
}

// UnregisterSet drops every internal subscription for (ConnID, QueryID).
// Subscriptions are removed before final-delta evaluation; a nil view skips
// final-delta computation for disconnect cleanup.
func (m *Manager) UnregisterSet(
	connID types.ConnectionID,
	queryID uint32,
	view store.CommittedReadView,
) (SubscriptionSetUnregisterResult, error) {
	return m.UnregisterSetContext(context.Background(), connID, queryID, view)
}

// UnregisterSetContext is UnregisterSet with cancellation support for the
// final snapshot evaluation performed on protocol unsubscribe.
func (m *Manager) UnregisterSetContext(
	ctx context.Context,
	connID types.ConnectionID,
	queryID uint32,
	view store.CommittedReadView,
) (SubscriptionSetUnregisterResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	byQ := m.querySets[connID]
	sids, ok := byQ[queryID]
	if !ok {
		return SubscriptionSetUnregisterResult{}, ErrSubscriptionNotFound
	}
	deletes := make([]SubscriptionUpdate, 0, len(sids))
	var evalErr error
	var evalSQL string
	for _, sid := range sids {
		hash, found := m.registry.hashForSub(connID, sid)
		if !found {
			continue
		}
		qs := m.registry.getQuery(hash)
		if qs == nil {
			continue
		}
		if view == nil || evalErr != nil {
			continue
		}
		initial, err := m.initialUpdates(ctx, qs.predicate, qs.projection, view, sid, queryID)
		if err != nil {
			evalErr = fmt.Errorf("%w: %w", ErrFinalQuery, err)
			evalSQL = qs.sqlText
			continue
		}
		for _, update := range initial {
			update.Deletes = update.Inserts
			update.Inserts = nil
			deletes = append(deletes, update)
		}
	}
	for _, sid := range sids {
		m.dropSub(connID, sid)
	}
	delete(byQ, queryID)
	if len(byQ) == 0 {
		delete(m.querySets, connID)
	}
	active := m.activeSets.Add(-1)
	recordSubscriptionActive(m.observer, int(active))
	if evalErr != nil {
		return SubscriptionSetUnregisterResult{QueryID: queryID, SQLText: evalSQL}, evalErr
	}
	return SubscriptionSetUnregisterResult{QueryID: queryID, Update: deletes}, nil
}

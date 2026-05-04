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
		m.removeActiveColumns(qs.predicate)
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

// initialQuery scans committed state and returns rows matching the
// predicate. Single-table predicates use a filtered table scan. Join
// predicates re-evaluate the full join against committed state and project
// each joined pair down to the subscription's SELECT side (Join.ProjectRight).
func (m *Manager) initialQuery(ctx context.Context, pred Predicate, view store.CommittedReadView) ([]types.ProductValue, error) {
	if view == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []types.ProductValue

	switch p := pred.(type) {
	case Join:
		joinedRows, err := m.appendProjectedJoinRows(ctx, out, view, p)
		if err != nil {
			return nil, err
		}
		out = joinedRows
	case CrossJoin:
		crossRows, err := m.appendProjectedCrossJoinRows(ctx, out, view, p)
		if err != nil {
			return nil, err
		}
		out = crossRows
	default:
		tables := pred.Tables()
		if len(tables) == 0 {
			return nil, nil
		}
		return m.initialRowsForTable(newInitialRowCollector(ctx, m.InitialRowLimit), pred, view, tables[0])
	}
	return out, nil
}

func (m *Manager) initialUpdates(ctx context.Context, pred Predicate, view store.CommittedReadView, subID types.SubscriptionID, queryID uint32) ([]SubscriptionUpdate, error) {
	if view == nil {
		return nil, nil
	}
	switch pred.(type) {
	case Join, CrossJoin:
		rows, err := m.initialQuery(ctx, pred, view)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
		tableID := emittedTableID(pred)
		return []SubscriptionUpdate{{
			SubscriptionID: subID,
			QueryID:        queryID,
			TableID:        tableID,
			TableName:      m.schema.TableName(tableID),
			Inserts:        rows,
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
				Inserts:        rows,
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
	// SPEC-001 §7.2 / SPEC-004: bare ColRange on an indexed column hits
	// view.IndexRange directly; the BTree binary-search start + ordered
	// range walk replaces the full TableScan + per-row bound recheck.
	// Compound shapes (And/Or), ColEq/ColNe/AllRows, or ranges on an
	// unindexed column stay on the TableScan+MatchRow fallback.
	if r, ok := pred.(ColRange); ok && r.Table == table && m.resolver != nil {
		if idxID, ok := m.resolver.IndexIDForColumn(r.Table, r.Column); ok {
			lower := store.Bound{Value: r.Lower.Value, Inclusive: r.Lower.Inclusive, Unbounded: r.Lower.Unbounded}
			upper := store.Bound{Value: r.Upper.Value, Inclusive: r.Upper.Inclusive, Unbounded: r.Upper.Unbounded}
			for _, row := range view.IndexRange(table, idxID, lower, upper) {
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
	if !hasOtherIdx {
		if _, ok := m.resolver.IndexIDForColumn(projectedTable, projectedJoinCol); !ok {
			return nil, fmt.Errorf("%w: join=%d.%d=%d.%d", ErrJoinIndexUnresolved, p.Left, p.LeftCol, p.Right, p.RightCol)
		}
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
	for _, projectedRow := range view.TableScan(projectedTable) {
		if err := ctx.Err(); err != nil {
			return nil, err
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
			continue
		}
		for _, otherRow := range view.TableScan(otherTable) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if int(otherJoinCol) >= len(otherRow) || !projectedRow[projectedJoinCol].Equal(otherRow[otherJoinCol]) {
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
	return out, nil
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
	// Pre-validate every predicate before touching registry state.
	for _, p := range req.Predicates {
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
	dedupedHashIdentities := make([]*types.Identity, 0, len(canonicalPreds))
	seen := make(map[QueryHash]struct{}, len(canonicalPreds))
	for i, p := range canonicalPreds {
		h := ComputeQueryHash(p, hashIdentities[i])
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		deduped = append(deduped, p)
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
		hash := ComputeQueryHash(p, dedupedHashIdentities[i])
		// Reusing this hash on the same connection skips a duplicate initial
		// snapshot but still allocates an internal subscription ID.
		existing := m.registry.getQuery(hash)
		sameConnReuse := existing != nil && len(existing.subscribers[req.ConnID]) > 0
		var initial []SubscriptionUpdate
		if !sameConnReuse {
			var err error
			initial, err = m.initialUpdates(ctx, p, view, subID, req.QueryID)
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
			qs = m.registry.createQueryState(hash, p)
			// SQLText is set only when the admission path knows the original
			// SQL string (Single subscribe). Multi leaves it empty —
			// reference `module_subscription_actor.rs:836` uses raw
			// `return_on_err!` on the unsubscribe path and does not apply
			// the `DBError::WithSql` suffix.
			qs.sqlText = req.SQLText
			placeSubscriptionForResolver(m.indexes, p, hash, m.resolver)
			m.addActiveColumns(p)
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
		initial, err := m.initialUpdates(ctx, qs.predicate, view, sid, queryID)
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

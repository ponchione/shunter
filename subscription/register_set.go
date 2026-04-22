package subscription

import (
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
		RemoveSubscription(m.indexes, qs.predicate, hash)
		m.registry.removeQueryState(hash)
	}
}

// removeDroppedSub removes a single internal subscription that the eval
// path has declared unrecoverable. Not exposed on the public manager
// surface — callers outside the package use UnregisterSet. Unlike
// dropSub it also culls the querySets entry the subID belonged to, so
// the corresponding (ConnID, QueryID) bucket does not retain a stale
// internal ID.
func (m *Manager) removeDroppedSub(connID types.ConnectionID, subID types.SubscriptionID) {
	m.dropSub(connID, subID)
	for qid, sids := range m.querySets[connID] {
		for i, s := range sids {
			if s == subID {
				m.querySets[connID][qid] = append(sids[:i], sids[i+1:]...)
				if len(m.querySets[connID][qid]) == 0 {
					delete(m.querySets[connID], qid)
				}
				if len(m.querySets[connID]) == 0 {
					delete(m.querySets, connID)
				}
				return
			}
		}
	}
}

// initialQuery scans committed state and returns rows matching the
// predicate. Single-table predicates use a filtered table scan. Join
// predicates re-evaluate the full join against committed state and project
// each joined pair down to the subscription's SELECT side (Join.ProjectRight).
func (m *Manager) initialQuery(pred Predicate, view store.CommittedReadView) ([]types.ProductValue, error) {
	if view == nil {
		return nil, nil
	}
	var out []types.ProductValue
	add := func(row types.ProductValue) error {
		if m.InitialRowLimit > 0 && len(out) >= m.InitialRowLimit {
			return fmt.Errorf("%w: cap=%d", ErrInitialRowLimit, m.InitialRowLimit)
		}
		out = append(out, row)
		return nil
	}

	switch p := pred.(type) {
	case Join:
		// Re-evaluate join: iterate one side and probe the other by join key.
		// Validation already confirmed an index exists on at least one side;
		// if the resolver disagrees on both sides, that is a contract violation,
		// not a user error — hard-fail instead of silently returning empty rows
		// (PHASE-5-DEFERRED §D).
		if m.resolver == nil {
			return nil, fmt.Errorf("%w: manager has no IndexResolver (join=%d.%d=%d.%d)", ErrJoinIndexUnresolved, p.Left, p.LeftCol, p.Right, p.RightCol)
		}
		// Each matched (lrow, rrow) pair is projected onto one side so the
		// caller sees rows shaped like the SELECT table, not concat LHS++RHS.
		project := func(lrow, rrow types.ProductValue) types.ProductValue {
			if p.ProjectRight {
				return rrow
			}
			return lrow
		}
		if rhsIdx, ok := m.resolver.IndexIDForColumn(p.Right, p.RightCol); ok {
			for _, lrow := range view.TableScan(p.Left) {
				if int(p.LeftCol) >= len(lrow) {
					continue
				}
				key := store.NewIndexKey(lrow[p.LeftCol])
				rowIDs := view.IndexSeek(p.Right, rhsIdx, key)
				for _, rid := range rowIDs {
					rrow, ok := view.GetRow(p.Right, rid)
					if !ok {
						continue
					}
					if joined := tryJoinFilter(lrow, p.Left, rrow, p.Right, &p); joined != nil {
						_ = joined
						if err := add(project(lrow, rrow)); err != nil {
							return nil, err
						}
					}
				}
			}
			break
		}
		lhsIdx, ok := m.resolver.IndexIDForColumn(p.Left, p.LeftCol)
		if !ok {
			return nil, fmt.Errorf("%w: join=%d.%d=%d.%d", ErrJoinIndexUnresolved, p.Left, p.LeftCol, p.Right, p.RightCol)
		}
		for _, rrow := range view.TableScan(p.Right) {
			if int(p.RightCol) >= len(rrow) {
				continue
			}
			key := store.NewIndexKey(rrow[p.RightCol])
			rowIDs := view.IndexSeek(p.Left, lhsIdx, key)
			for _, rid := range rowIDs {
				lrow, ok := view.GetRow(p.Left, rid)
				if !ok {
					continue
				}
				if joined := tryJoinFilter(lrow, p.Left, rrow, p.Right, &p); joined != nil {
					_ = joined
					if err := add(project(lrow, rrow)); err != nil {
						return nil, err
					}
				}
			}
		}
	case CrossJoinProjected:
		if view.RowCount(p.Other) == 0 {
			return nil, nil
		}
		for _, row := range view.TableScan(p.Projected) {
			if err := add(row); err != nil {
				return nil, err
			}
		}
	default:
		tables := pred.Tables()
		if len(tables) == 0 {
			return nil, nil
		}
		t := tables[0]
		// SPEC-001 §7.2 / SPEC-004: bare ColRange on an indexed column hits
		// view.IndexRange directly; the BTree binary-search start + ordered
		// range walk replaces the full TableScan + per-row bound recheck.
		// Compound shapes (And/Or), ColEq/ColNe/AllRows, or ranges on an
		// unindexed column stay on the TableScan+MatchRow fallback.
		if r, ok := pred.(ColRange); ok && m.resolver != nil {
			if idxID, ok := m.resolver.IndexIDForColumn(r.Table, r.Column); ok {
				lower := store.Bound{Value: r.Lower.Value, Inclusive: r.Lower.Inclusive, Unbounded: r.Lower.Unbounded}
				upper := store.Bound{Value: r.Upper.Value, Inclusive: r.Upper.Inclusive, Unbounded: r.Upper.Unbounded}
				for _, row := range view.IndexRange(t, idxID, lower, upper) {
					if err := add(row); err != nil {
						return nil, err
					}
				}
				break
			}
		}
		for _, row := range view.TableScan(t) {
			if MatchRow(pred, t, row) {
				if err := add(row); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

// emittedTableID returns the table ID whose row shape the subscription emits
// at the wire boundary. Join and CrossJoinProjected carry an explicit
// projected side; every other predicate emits rows from its sole declared
// table. Zero is returned when the predicate carries no table (malformed).
func emittedTableID(p Predicate) TableID {
	switch x := p.(type) {
	case Join:
		return x.ProjectedTable()
	case CrossJoinProjected:
		return x.Projected
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
	// Pre-validate every predicate before touching registry state.
	for _, p := range req.Predicates {
		if err := ValidatePredicate(p, m.schema); err != nil {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("predicate validation: %w", err)
		}
	}
	// Duplicate QueryID rejection.
	if byQ, ok := m.querySets[req.ConnID]; ok {
		if _, live := byQ[req.QueryID]; live {
			return SubscriptionSetRegisterResult{}, fmt.Errorf("%w: conn=%x query=%d",
				ErrQueryIDAlreadyLive, req.ConnID[:4], req.QueryID)
		}
	}
	// Dedup identical predicates within this call.
	deduped := make([]Predicate, 0, len(req.Predicates))
	seen := make(map[QueryHash]struct{}, len(req.Predicates))
	for _, p := range req.Predicates {
		h := ComputeQueryHash(p, req.ClientIdentity)
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		deduped = append(deduped, p)
	}
	// Allocate internal IDs + run initial snapshot per predicate.
	allocated := make([]types.SubscriptionID, 0, len(deduped))
	updates := make([]SubscriptionUpdate, 0, len(deduped))
	for _, p := range deduped {
		m.nextSubID++
		subID := m.nextSubID
		hash := ComputeQueryHash(p, req.ClientIdentity)
		rows, err := m.initialQuery(p, view)
		if err != nil {
			// Unwind any partial state. dropSub handles registry maps + PruningIndexes
			// eviction on last-ref; each allocated sub is dropped independently.
			for _, sid := range allocated {
				m.dropSub(req.ConnID, sid)
			}
			return SubscriptionSetRegisterResult{}, fmt.Errorf("initial query: %w", err)
		}
		qs := m.registry.getQuery(hash)
		if qs == nil {
			qs = m.registry.createQueryState(hash, p)
			PlaceSubscription(m.indexes, p, hash)
		}
		m.registry.addSubscriber(hash, req.ConnID, subID, req.RequestID)
		allocated = append(allocated, subID)
		_ = qs
		if len(rows) > 0 {
			tableID := emittedTableID(p)
			updates = append(updates, SubscriptionUpdate{
				SubscriptionID: subID,
				TableID:        tableID,
				TableName:      m.schema.TableName(tableID),
				Inserts:        rows,
			})
		}
	}
	if m.querySets[req.ConnID] == nil {
		m.querySets[req.ConnID] = make(map[uint32][]types.SubscriptionID)
	}
	m.querySets[req.ConnID][req.QueryID] = allocated
	return SubscriptionSetRegisterResult{QueryID: req.QueryID, Update: updates}, nil
}

// UnregisterSet drops every internal subscription registered under
// (ConnID, QueryID). Reference: remove_subscription at
// reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:841.
// If view is nil, no final-delta rows are computed and Update is empty —
// suitable for disconnect-time cleanup.
func (m *Manager) UnregisterSet(
	connID types.ConnectionID,
	queryID uint32,
	view store.CommittedReadView,
) (SubscriptionSetUnregisterResult, error) {
	byQ := m.querySets[connID]
	sids, ok := byQ[queryID]
	if !ok {
		return SubscriptionSetUnregisterResult{}, ErrSubscriptionNotFound
	}
	deletes := make([]SubscriptionUpdate, 0, len(sids))
	for _, sid := range sids {
		hash, found := m.registry.hashForSub(connID, sid)
		if !found {
			continue
		}
		qs := m.registry.getQuery(hash)
		if qs == nil {
			continue
		}
		if view != nil {
			rows, err := m.initialQuery(qs.predicate, view)
			// Final-delta errors are non-fatal — the subscription still drops.
			if err == nil && len(rows) > 0 {
				tableID := emittedTableID(qs.predicate)
				deletes = append(deletes, SubscriptionUpdate{
					SubscriptionID: sid,
					TableID:        tableID,
					TableName:      m.schema.TableName(tableID),
					Deletes:        rows,
				})
			}
		}
		m.dropSub(connID, sid)
	}
	delete(byQ, queryID)
	if len(byQ) == 0 {
		delete(m.querySets, connID)
	}
	return SubscriptionSetUnregisterResult{QueryID: queryID, Update: deletes}, nil
}

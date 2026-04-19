package subscription

import (
	"fmt"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// dropSub removes a single (connID, subID) registry entry and, on
// last-ref, also evicts the query state + pruning-index placement.
// Mirrors Unregister's semantics — the plain registry.unregisterSingle
// only touches the registry maps and would leak PruningIndexes rows.
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
			// Unwind any partial state, including pruning-index placement
			// (mirror legacy Unregister semantics — plain unregisterSingle
			// only touches the registry maps).
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
			tables := p.Tables()
			var tableID TableID
			if len(tables) > 0 {
				tableID = tables[0]
			}
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
				tables := qs.predicate.Tables()
				var tableID TableID
				if len(tables) > 0 {
					tableID = tables[0]
				}
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

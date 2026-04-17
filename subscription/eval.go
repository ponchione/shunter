package subscription

import (
	"fmt"
	"log"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// memoizedResult holds per-query-hash encoded delta bytes for one evaluation
// cycle. Encoding is lazy — the binary / json slices stay nil until the
// first client of that format needs them (SPEC-004 §7.4). Actual encoding
// lives in the protocol layer (Phase 7/8); Phase 5 only plumbs the cache.
type memoizedResult struct {
	binary []byte
	json   []byte
}

// EvalAndBroadcast runs the post-commit evaluation loop (SPEC-004 §7.2).
// Fills a CommitFanout and hands it to the fan-out worker inbox.
//
// Called synchronously on the executor goroutine; changeset is read-only.
// When no subscriptions are active the function returns immediately.
func (m *Manager) EvalAndBroadcast(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView, meta PostCommitMeta) {
	if !m.registry.hasActive() || changeset == nil || changeset.IsEmpty() {
		return
	}
	fanout, errs := m.evaluate(txID, changeset, view)
	if m.inbox != nil {
		m.inbox <- FanOutMessage{
			TxID:         txID,
			TxDurable:    meta.TxDurable,
			Fanout:       fanout,
			Errors:       errs,
			CallerConnID: meta.CallerConnID,
			CallerResult: meta.CallerResult,
		}
	}
}

// evaluate is the inner orchestration: build DeltaView, collect candidates,
// evaluate each candidate, and assemble the per-connection fanout.
func (m *Manager) evaluate(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView) (CommitFanout, map[types.ConnectionID][]SubscriptionError) {
	_ = txID

	activeCols := m.collectActiveColumns()
	dv := NewDeltaView(view, changeset, activeCols)
	defer dv.Release()
	candidateScratch := acquireCandidateScratch()
	defer releaseCandidateScratch(candidateScratch)
	candidates := m.collectCandidatesInto(changeset, view, candidateScratch)

	fanout := CommitFanout{}
	errs := make(map[types.ConnectionID][]SubscriptionError)
	memo := make(map[QueryHash]*memoizedResult)
	_ = memo

	for hash := range candidates {
		qs := m.registry.getQuery(hash)
		if qs == nil {
			continue
		}
		updates, err := m.evalQuerySafe(qs, dv)
		if err != nil {
			m.handleEvalError(qs, err, errs)
			continue
		}
		if len(updates) == 0 {
			continue
		}
		memo[hash] = &memoizedResult{}
		for connID, subIDs := range qs.subscribers {
			for subID := range subIDs {
				for _, u := range updates {
					u.SubscriptionID = subID
					fanout[connID] = append(fanout[connID], u)
				}
			}
		}
	}
	return fanout, errs
}

func (m *Manager) handleEvalError(qs *queryState, err error, out map[types.ConnectionID][]SubscriptionError) {
	predRepr := fmt.Sprintf("%#v", qs.predicate)
	wrapped := fmt.Errorf("%w: %v", ErrSubscriptionEval, err)
	log.Printf("subscription: evaluation error for query %s predicate=%s: %v", qs.hash, predRepr, wrapped)

	type doomedSub struct {
		connID types.ConnectionID
		subID  types.SubscriptionID
	}
	var doomed []doomedSub
	for connID, subIDs := range qs.subscribers {
		for subID := range subIDs {
			out[connID] = append(out[connID], SubscriptionError{
				SubscriptionID: subID,
				QueryHash:      qs.hash,
				Predicate:      predRepr,
				Message:        wrapped.Error(),
			})
			doomed = append(doomed, doomedSub{connID: connID, subID: subID})
		}
	}
	for _, sub := range doomed {
		_ = m.Unregister(sub.connID, sub.subID)
	}
}

// collectActiveColumns gathers every (table, column) referenced by an active
// predicate. Used to decide which delta indexes NewDeltaView should build.
func (m *Manager) collectActiveColumns() map[TableID][]ColID {
	tmp := make(map[TableID]map[ColID]struct{})
	ensure := func(t TableID, c ColID) {
		cols, ok := tmp[t]
		if !ok {
			cols = make(map[ColID]struct{})
			tmp[t] = cols
		}
		cols[c] = struct{}{}
	}
	var walk func(p Predicate)
	walk = func(p Predicate) {
		switch x := p.(type) {
		case ColEq:
			ensure(x.Table, x.Column)
		case ColRange:
			ensure(x.Table, x.Column)
		case And:
			if x.Left != nil {
				walk(x.Left)
			}
			if x.Right != nil {
				walk(x.Right)
			}
		case Join:
			ensure(x.Left, x.LeftCol)
			ensure(x.Right, x.RightCol)
			if x.Filter != nil {
				walk(x.Filter)
			}
		}
	}
	for _, qs := range m.registry.byHash {
		walk(qs.predicate)
	}
	out := make(map[TableID][]ColID, len(tmp))
	for t, cols := range tmp {
		list := make([]ColID, 0, len(cols))
		for c := range cols {
			list = append(list, c)
		}
		out[t] = list
	}
	return out
}

// collectCandidates walks the changeset and returns the union of candidate
// query hashes across all three pruning tiers (SPEC-004 §7.2 step 3 / §7.3).
func (m *Manager) collectCandidates(cs *store.Changeset, view store.CommittedReadView) map[QueryHash]struct{} {
	st := acquireCandidateScratch()
	defer releaseCandidateScratch(st)
	out := m.collectCandidatesInto(cs, view, st)
	copied := make(map[QueryHash]struct{}, len(out))
	for h := range out {
		copied[h] = struct{}{}
	}
	return copied
}

// collectCandidatesInto walks the changeset and populates the provided scratch
// maps with the union of candidate query hashes across all three pruning tiers
// (SPEC-004 §7.2 step 3 / §7.3). Batched Tier 1 optimization: collect distinct
// values per tracked column, one lookup per distinct value.
func (m *Manager) collectCandidatesInto(cs *store.Changeset, view store.CommittedReadView, st *candidateScratch) map[QueryHash]struct{} {
	cands := st.candidates
	for h := range cands {
		delete(cands, h)
	}
	for tid, tc := range cs.Tables {
		if tc == nil {
			continue
		}

		// Tier 1: batched value-index lookup.
		for _, col := range m.indexes.Value.TrackedColumns(tid) {
			distinct := st.distinct
			for k := range distinct {
				delete(distinct, k)
			}
			collectDistinct := func(rows []types.ProductValue) {
				for _, row := range rows {
					if int(col) >= len(row) {
						continue
					}
					k := encodeValueKey(row[col])
					if _, ok := distinct[k]; !ok {
						distinct[k] = row[col]
					}
				}
			}
			collectDistinct(tc.Inserts)
			collectDistinct(tc.Deletes)
			for _, v := range distinct {
				for _, h := range m.indexes.Value.Lookup(tid, col, v) {
					cands[h] = struct{}{}
				}
			}
		}

		// Tier 2: join edges where this table is the LHS.
		if view != nil && m.resolver != nil {
			for _, edge := range m.indexes.JoinEdge.EdgesForTable(tid) {
				rhsIdx, ok := m.resolver.IndexIDForColumn(edge.RHSTable, edge.RHSJoinCol)
				if !ok {
					continue
				}
				probe := func(row types.ProductValue) {
					if int(edge.LHSJoinCol) >= len(row) {
						return
					}
					key := store.NewIndexKey(row[edge.LHSJoinCol])
					rowIDs := view.IndexSeek(edge.RHSTable, rhsIdx, key)
					for _, rid := range rowIDs {
						rhsRow, ok := view.GetRow(edge.RHSTable, rid)
						if !ok {
							continue
						}
						if int(edge.RHSFilterCol) >= len(rhsRow) {
							continue
						}
						for _, h := range m.indexes.JoinEdge.Lookup(edge, rhsRow[edge.RHSFilterCol]) {
							cands[h] = struct{}{}
						}
					}
				}
				for _, row := range tc.Inserts {
					probe(row)
				}
				for _, row := range tc.Deletes {
					probe(row)
				}
			}
		}

		// Tier 3: table fallback.
		for _, h := range m.indexes.Table.Lookup(tid) {
			cands[h] = struct{}{}
		}
	}
	return cands
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
	switch p := qs.predicate.(type) {
	case Join:
		frags := EvalJoinDeltaFragments(dv, &p, m.resolver)
		ins, del := ReconcileJoinDelta(frags.Inserts[:], frags.Deletes[:])
		if len(ins) == 0 && len(del) == 0 {
			return nil
		}
		name := m.schema.TableName(p.Left)
		if rname := m.schema.TableName(p.Right); rname != "" {
			if name == "" {
				name = rname
			} else {
				name = name + "+" + rname
			}
		}
		return []SubscriptionUpdate{{
			TableID:   p.Left,
			TableName: name,
			Inserts:   ins,
			Deletes:   del,
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
				Inserts:   ins,
				Deletes:   del,
			})
		}
		return updates
	}
}

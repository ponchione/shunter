package subscription

import (
	"fmt"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// Register validates, compiles, and registers a subscription. The returned
// InitialRows reflect committed state captured inside the executor command
// that runs Register — no commit can slip between initial materialization
// and subscription activation (SPEC-004 §4.1).
func (m *Manager) Register(req SubscriptionRegisterRequest, view store.CommittedReadView) (SubscriptionRegisterResult, error) {
	if err := ValidatePredicate(req.Predicate, m.schema); err != nil {
		return SubscriptionRegisterResult{}, err
	}

	hash := ComputeQueryHash(req.Predicate, req.ClientIdentity)

	// Execute initial query up front so we can honor the row limit before
	// mutating any registry state.
	initial, err := m.initialQuery(req.Predicate, view)
	if err != nil {
		return SubscriptionRegisterResult{}, err
	}

	// Dedup check: if the query state already exists, reuse it.
	qs := m.registry.getQuery(hash)
	if qs == nil {
		qs = m.registry.createQueryState(hash, req.Predicate)
		PlaceSubscription(m.indexes, req.Predicate, hash)
	}
	m.registry.addSubscriber(hash, req.ConnID, req.SubscriptionID, req.RequestID)
	_ = qs

	return SubscriptionRegisterResult{
		SubscriptionID: req.SubscriptionID,
		InitialRows:    initial,
	}, nil
}

// initialQuery scans committed state and returns rows matching the
// predicate. Single-table predicates use a filtered table scan. Join
// predicates re-evaluate the full join against committed state.
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
		if rhsIdx, ok := m.resolver.IndexIDForColumn(p.Right, p.RightCol); ok {
			for _, lrow := range func() []types.ProductValue {
				var ls []types.ProductValue
				for _, row := range iterateAll(view, p.Left) {
					ls = append(ls, row)
				}
				return ls
			}() {
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
						if err := add(joined); err != nil {
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
		for _, rrow := range func() []types.ProductValue {
			var rs []types.ProductValue
			for _, row := range iterateAll(view, p.Right) {
				rs = append(rs, row)
			}
			return rs
		}() {
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
					if err := add(joined); err != nil {
						return nil, err
					}
				}
			}
		}
	default:
		tables := pred.Tables()
		if len(tables) == 0 {
			return nil, nil
		}
		t := tables[0]
		for _, row := range iterateAll(view, t) {
			if MatchRow(pred, t, row) {
				if err := add(row); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

// iterateAll materializes every row from the committed view's TableScan.
// Small helper so callers can range over a slice instead of iter.Seq2.
func iterateAll(view store.CommittedReadView, t TableID) []types.ProductValue {
	var out []types.ProductValue
	for _, row := range view.TableScan(t) {
		out = append(out, row)
	}
	return out
}

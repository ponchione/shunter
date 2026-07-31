package subscription

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponchione/shunter/internal/querywork"
	"github.com/ponchione/shunter/store"
)

const (
	// DefaultMultiJoinMaxRelations bounds live multi-way join graph breadth.
	DefaultMultiJoinMaxRelations = 8
	// DefaultMultiJoinMaxRowsPerRelation bounds each materialized live input.
	DefaultMultiJoinMaxRowsPerRelation = 100_000
	// DefaultMultiJoinMaxWork is the historical name of the work limit shared
	// by every live snapshot and delta evaluation.
	DefaultMultiJoinMaxWork = 1_000_000
)

func (m *Manager) withWorkBudget(ctx context.Context) context.Context {
	return querywork.WithBudget(ctx, m.MaxMultiJoinWork)
}

func chargeSubscriptionWork(ctx context.Context) error {
	return chargeSubscriptionWorkN(ctx, 1)
}

func chargeSubscriptionWorkN(ctx context.Context, n uint64) error {
	if err := querywork.ChargeN(ctx, n); err != nil {
		var exhausted *querywork.ExhaustedError
		if errors.As(err, &exhausted) {
			return NewQuotaError(ErrSubscriptionWorkLimit, "work", exhausted.Used, exhausted.Limit)
		}
		return fmt.Errorf("%w: %v", ErrSubscriptionWorkLimit, err)
	}
	return nil
}

func chargeSubscriptionWorkProduct(ctx context.Context, left, right int) error {
	if left < 0 || right < 0 {
		return fmt.Errorf("%w: negative work cardinality %d * %d", ErrSubscriptionWorkLimit, left, right)
	}
	product := uint64(left) * uint64(right)
	if left != 0 && product/uint64(left) != uint64(right) {
		product = ^uint64(0)
	}
	return chargeSubscriptionWorkN(ctx, product)
}

func chargeMultiJoinWork(ctx context.Context) error {
	if err := querywork.Charge(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrMultiJoinLimit, err)
	}
	return nil
}

func (m *Manager) checkMultiJoinLimits(ctx context.Context, pred Predicate, view store.CommittedReadView) error {
	multi, ok := pred.(MultiJoin)
	if !ok {
		return nil
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := m.checkMultiJoinRelationLimit(multi); err != nil {
		return err
	}
	if m.MaxMultiJoinRowsPerRelation <= 0 || view == nil {
		return nil
	}
	for i, rel := range multi.Relations {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		rows := view.RowCount(rel.Table)
		if rows > m.MaxMultiJoinRowsPerRelation {
			return fmt.Errorf("%w: relation=%d table=%d rows=%d max=%d",
				ErrMultiJoinLimit, i, rel.Table, rows, m.MaxMultiJoinRowsPerRelation)
		}
	}
	return nil
}

func (m *Manager) checkMultiJoinDeltaLimits(ctx context.Context, pred Predicate, dv *DeltaView) error {
	multi, ok := pred.(MultiJoin)
	if !ok {
		return nil
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := m.checkMultiJoinRelationLimit(multi); err != nil {
		return err
	}
	if m.MaxMultiJoinRowsPerRelation <= 0 || dv == nil || dv.CommittedView() == nil {
		return nil
	}
	for i, rel := range multi.Relations {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		afterRows := rowCountAfter(dv, rel.Table)
		if afterRows > m.MaxMultiJoinRowsPerRelation {
			return fmt.Errorf("%w: relation=%d table=%d rows=%d max=%d phase=after",
				ErrMultiJoinLimit, i, rel.Table, afterRows, m.MaxMultiJoinRowsPerRelation)
		}
		beforeRows := afterRows - len(dv.InsertedRows(rel.Table)) + len(dv.DeletedRows(rel.Table))
		if beforeRows > m.MaxMultiJoinRowsPerRelation {
			return fmt.Errorf("%w: relation=%d table=%d rows=%d max=%d phase=before",
				ErrMultiJoinLimit, i, rel.Table, beforeRows, m.MaxMultiJoinRowsPerRelation)
		}
	}
	return nil
}

func (m *Manager) collectMultiJoinDeltaLimitErrors(dv *DeltaView) map[QueryHash]error {
	if m.MaxMultiJoinRelations <= 0 && m.MaxMultiJoinRowsPerRelation <= 0 {
		return nil
	}
	var out map[QueryHash]error
	for hash, qs := range m.registry.byHash {
		if qs == nil {
			continue
		}
		if err := m.checkMultiJoinDeltaLimits(context.Background(), qs.predicate, dv); err != nil {
			if out == nil {
				out = make(map[QueryHash]error)
			}
			out[hash] = err
		}
	}
	return out
}

func (m *Manager) checkMultiJoinRelationLimit(multi MultiJoin) error {
	if m.MaxMultiJoinRelations <= 0 || len(multi.Relations) <= m.MaxMultiJoinRelations {
		return nil
	}
	return fmt.Errorf("%w: relations=%d max=%d", ErrMultiJoinLimit, len(multi.Relations), m.MaxMultiJoinRelations)
}

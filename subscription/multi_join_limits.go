package subscription

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/store"
)

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
	view := dv.CommittedView()
	for i, rel := range multi.Relations {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		afterRows := view.RowCount(rel.Table)
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

func (m *Manager) checkMultiJoinRelationLimit(multi MultiJoin) error {
	if m.MaxMultiJoinRelations <= 0 || len(multi.Relations) <= m.MaxMultiJoinRelations {
		return nil
	}
	return fmt.Errorf("%w: relations=%d max=%d", ErrMultiJoinLimit, len(multi.Relations), m.MaxMultiJoinRelations)
}

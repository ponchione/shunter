package protocol

import (
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// AdmissionRelation records one relation instance referenced by an admitted
// subscription predicate.
type AdmissionRelation struct {
	Relation int
	Table    schema.TableID
	Alias    uint8
}

// AdmissionColumnRef records one side of an admitted join condition.
type AdmissionColumnRef struct {
	Relation int
	Table    schema.TableID
	Column   types.ColID
	Alias    uint8
	Indexed  bool
}

// AdmissionJoinCondition records a binary join condition in admission order.
type AdmissionJoinCondition struct {
	Left  AdmissionColumnRef
	Right AdmissionColumnRef
}

// AdmissionProjectedRelation reports the relation index that supplies a
// predicate's table-shaped rows. Non-join predicates return relation 0, and
// nil/empty predicates return -1.
func AdmissionProjectedRelation(pred subscription.Predicate) int {
	switch p := pred.(type) {
	case subscription.Join:
		if p.ProjectRight {
			return 1
		}
		return 0
	case subscription.CrossJoin:
		if p.ProjectRight {
			return 1
		}
		return 0
	case subscription.MultiJoin:
		return p.ProjectedRelation
	default:
		if pred == nil || len(pred.Tables()) == 0 {
			return -1
		}
		return 0
	}
}

// AdmissionJoinGraph extracts relation instances and join conditions from an
// admitted predicate. Index flags are populated when sl is non-nil.
func AdmissionJoinGraph(pred subscription.Predicate, sl SchemaLookup) ([]AdmissionRelation, []AdmissionJoinCondition) {
	if pred == nil {
		return nil, nil
	}
	switch p := pred.(type) {
	case subscription.Join:
		relations := []AdmissionRelation{
			{Relation: 0, Table: p.Left, Alias: p.LeftAlias},
			{Relation: 1, Table: p.Right, Alias: p.RightAlias},
		}
		conditions := []AdmissionJoinCondition{{
			Left: AdmissionColumnRef{
				Relation: 0,
				Table:    p.Left,
				Column:   p.LeftCol,
				Alias:    p.LeftAlias,
				Indexed:  sl != nil && sl.HasIndex(p.Left, p.LeftCol),
			},
			Right: AdmissionColumnRef{
				Relation: 1,
				Table:    p.Right,
				Column:   p.RightCol,
				Alias:    p.RightAlias,
				Indexed:  sl != nil && sl.HasIndex(p.Right, p.RightCol),
			},
		}}
		return relations, conditions
	case subscription.CrossJoin:
		return []AdmissionRelation{
			{Relation: 0, Table: p.Left, Alias: p.LeftAlias},
			{Relation: 1, Table: p.Right, Alias: p.RightAlias},
		}, nil
	case subscription.MultiJoin:
		relations := make([]AdmissionRelation, len(p.Relations))
		for i, relation := range p.Relations {
			relations[i] = AdmissionRelation{
				Relation: i,
				Table:    relation.Table,
				Alias:    relation.Alias,
			}
		}
		conditions := make([]AdmissionJoinCondition, len(p.Conditions))
		for i, condition := range p.Conditions {
			conditions[i] = AdmissionJoinCondition{
				Left:  admissionColumnRefFromMultiJoin(condition.Left, sl),
				Right: admissionColumnRefFromMultiJoin(condition.Right, sl),
			}
		}
		return relations, conditions
	default:
		tables := pred.Tables()
		relations := make([]AdmissionRelation, len(tables))
		for i, table := range tables {
			relations[i] = AdmissionRelation{
				Relation: i,
				Table:    table,
			}
		}
		return relations, nil
	}
}

func admissionColumnRefFromMultiJoin(ref subscription.MultiJoinColumnRef, sl SchemaLookup) AdmissionColumnRef {
	return AdmissionColumnRef{
		Relation: ref.Relation,
		Table:    ref.Table,
		Column:   ref.Column,
		Alias:    ref.Alias,
		Indexed:  sl != nil && sl.HasIndex(ref.Table, ref.Column),
	}
}

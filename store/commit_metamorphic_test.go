package store

import (
	"maps"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestCommitIndependentTransactionsOrderMetamorphicEquivalence(t *testing.T) {
	const seed = uint64(0x0c01117e)
	ops := []independentCommitOp{
		{label: "alpha", rows: []commitMetamorphicRow{{pk: 1, name: "alice"}, {pk: 3, name: "carol"}}},
		{label: "beta", rows: []commitMetamorphicRow{{pk: 2, name: "bob"}, {pk: 4, name: "dave"}}},
		{label: "gamma", rows: []commitMetamorphicRow{{pk: 5, name: "eve"}}},
	}
	orders := []struct {
		name  string
		order []int
	}{
		{name: "forward", order: []int{0, 1, 2}},
		{name: "reverse", order: []int{2, 1, 0}},
		{name: "interleaved", order: []int{1, 0, 2}},
	}

	var baseline map[uint64]string
	for orderIndex, order := range orders {
		got := runIndependentCommitOrder(t, seed, orderIndex, ops, order.order)
		if orderIndex == 0 {
			baseline = got
			continue
		}
		if !maps.Equal(got, baseline) {
			t.Fatalf("seed=%#x order_index=%d runtime_config=ops=%d/order=%s operation=compare-committed-rows observed=%v expected=%v",
				seed, orderIndex, len(ops), order.name, got, baseline)
		}
	}
}

type independentCommitOp struct {
	label string
	rows  []commitMetamorphicRow
}

type commitMetamorphicRow struct {
	pk   uint64
	name string
}

func runIndependentCommitOrder(t *testing.T, seed uint64, orderIndex int, ops []independentCommitOp, order []int) map[uint64]string {
	t.Helper()
	cs, reg := buildTestState()
	for opIndex, opID := range order {
		op := ops[opID]
		tx := NewTransaction(cs, reg)
		for _, row := range op.rows {
			if _, err := tx.Insert(0, mkRow(row.pk, row.name)); err != nil {
				t.Fatalf("seed=%#x order_index=%d op=%d runtime_config=ops=%d operation=%s.Insert(%d,%q) observed_error=%v expected=nil",
					seed, orderIndex, opIndex, len(ops), op.label, row.pk, row.name, err)
			}
		}
		if _, err := Commit(cs, tx); err != nil {
			t.Fatalf("seed=%#x order_index=%d op=%d runtime_config=ops=%d operation=%s.Commit observed_error=%v expected=nil",
				seed, orderIndex, opIndex, len(ops), op.label, err)
		}
	}
	return collectCommittedPlayerRowsByPK(t, cs)
}

func collectCommittedPlayerRowsByPK(t *testing.T, cs *CommittedState) map[uint64]string {
	t.Helper()
	snap := cs.Snapshot()
	defer snap.Close()
	rows := map[uint64]string{}
	for _, row := range snap.TableScan(0) {
		rows[row[0].AsUint64()] = row[1].AsString()
	}
	for pk, name := range rows {
		ids := snap.IndexSeek(0, 0, NewIndexKey(types.NewUint64(pk)))
		if len(ids) != 1 {
			t.Fatalf("operation=collect-committed-rows observed_index_ids=%v expected=one-id-for-pk-%d", ids, pk)
		}
		indexedRow, ok := snap.GetRow(0, ids[0])
		if !ok || indexedRow[1].AsString() != name {
			t.Fatalf("operation=collect-committed-rows observed_index_row=(ok=%v row=%v) expected_name=%q", ok, indexedRow, name)
		}
	}
	return rows
}

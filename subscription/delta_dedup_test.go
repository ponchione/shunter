package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func mkRow(vals ...any) types.ProductValue {
	row := make(types.ProductValue, 0, len(vals))
	for _, v := range vals {
		switch x := v.(type) {
		case uint64:
			row = append(row, types.NewUint64(x))
		case string:
			row = append(row, types.NewString(x))
		default:
			panic("unhandled test value")
		}
	}
	return row
}

func TestReconcileInsertOnly(t *testing.T) {
	ins := [][]types.ProductValue{{mkRow(uint64(1), "a")}, {mkRow(uint64(2), "b")}}
	del := [][]types.ProductValue{}
	i, d := ReconcileJoinDelta(ins, del)
	if len(i) != 2 || len(d) != 0 {
		t.Fatalf("ins/del = %d/%d, want 2/0", len(i), len(d))
	}
}

func TestReconcileDeleteOnly(t *testing.T) {
	ins := [][]types.ProductValue{}
	del := [][]types.ProductValue{{mkRow(uint64(1), "a")}}
	i, d := ReconcileJoinDelta(ins, del)
	if len(i) != 0 || len(d) != 1 {
		t.Fatalf("ins/del = %d/%d, want 0/1", len(i), len(d))
	}
}

func TestReconcileCancellation(t *testing.T) {
	row := mkRow(uint64(1), "a")
	ins := [][]types.ProductValue{{row}}
	del := [][]types.ProductValue{{row}}
	i, d := ReconcileJoinDelta(ins, del)
	if len(i) != 0 || len(d) != 0 {
		t.Fatalf("full cancel expected, got %d/%d", len(i), len(d))
	}
}

func TestReconcileMultiplicityInsertHeavy(t *testing.T) {
	row := mkRow(uint64(1), "a")
	ins := [][]types.ProductValue{{row, row, row}}
	del := [][]types.ProductValue{{row}}
	i, d := ReconcileJoinDelta(ins, del)
	if len(i) != 2 || len(d) != 0 {
		t.Fatalf("ins/del = %d/%d, want 2/0", len(i), len(d))
	}
}

func TestReconcileMultiplicityDeleteHeavy(t *testing.T) {
	row := mkRow(uint64(1), "a")
	ins := [][]types.ProductValue{{row}}
	del := [][]types.ProductValue{{row, row, row}}
	i, d := ReconcileJoinDelta(ins, del)
	if len(i) != 0 || len(d) != 2 {
		t.Fatalf("ins/del = %d/%d, want 0/2", len(i), len(d))
	}
}

func TestReconcileSemijoinMultiplicityPreserved(t *testing.T) {
	// One LHS row joining 3 RHS rows; delete one RHS row → delta shows 1 delete.
	lhs := mkRow(uint64(1), "a")
	rhs1 := mkRow(uint64(10))
	rhs2 := mkRow(uint64(11))
	rhs3 := mkRow(uint64(12))
	joined := func(a, b types.ProductValue) types.ProductValue {
		out := append(types.ProductValue{}, a...)
		out = append(out, b...)
		return out
	}
	// All three pairs were in committed — only deletion fragment D1 (dT1(-) join T2')
	// or D2 (T1' join dT2(-)) emits. Simulate D2 for rhs1 delete.
	del := [][]types.ProductValue{{joined(lhs, rhs1)}}
	ins := [][]types.ProductValue{}
	i, d := ReconcileJoinDelta(ins, del)
	if len(i) != 0 || len(d) != 1 {
		t.Fatalf("expected 1 delete, got ins=%d del=%d", len(i), len(d))
	}
	_ = rhs2
	_ = rhs3
}

func TestReconcileEmpty(t *testing.T) {
	i, d := ReconcileJoinDelta(nil, nil)
	if len(i) != 0 || len(d) != 0 {
		t.Fatalf("empty → empty, got %d/%d", len(i), len(d))
	}
}

func TestReconcileStructurallyEqualFromDifferentFragments(t *testing.T) {
	// Two rows constructed independently but with identical content must
	// compare equal in the bag dedup.
	r1 := mkRow(uint64(1), "a")
	r2 := mkRow(uint64(1), "a")
	ins := [][]types.ProductValue{{r1}}
	del := [][]types.ProductValue{{r2}}
	i, d := ReconcileJoinDelta(ins, del)
	if len(i) != 0 || len(d) != 0 {
		t.Fatalf("structurally equal rows should cancel, got %d/%d", len(i), len(d))
	}
}

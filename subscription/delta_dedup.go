package subscription

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/types"
)

// ReconcileJoinDelta cancels insert/delete join fragments with bag semantics.
// Scratch state is pooled to avoid hot-path allocation churn.
func ReconcileJoinDelta(insertFragments, deleteFragments [][]types.ProductValue) (inserts, deletes []types.ProductValue) {
	inserts, deletes, _ = reconcileJoinDelta(context.Background(), insertFragments, deleteFragments)
	return inserts, deletes
}

func reconcileJoinDelta(ctx context.Context, insertFragments, deleteFragments [][]types.ProductValue) (inserts, deletes []types.ProductValue, err error) {
	st := dedupPool.Get().(*dedupState)
	defer func() {
		st.clear()
		dedupPool.Put(st)
	}()

	for _, frag := range insertFragments {
		for _, row := range frag {
			incrementRowBag(st.insertRows, &st.insertOrder, row)
		}
	}

	for _, frag := range deleteFragments {
		for _, row := range frag {
			if decrementRowCount(st.insertRows, row) {
				continue
			}
			incrementRowBag(st.deleteRows, &st.deleteOrder, row)
		}
	}

	inserts, err = appendReconciledRows(ctx, inserts, st.insertOrder, st.insertRows, "insert")
	if err != nil {
		return nil, nil, err
	}
	deletes, err = appendReconciledRows(ctx, deletes, st.deleteOrder, st.deleteRows, "delete")
	if err != nil {
		return nil, nil, err
	}
	return inserts, deletes, nil
}

func appendReconciledRows(
	ctx context.Context,
	out []types.ProductValue,
	order []countedRowRef,
	rows map[uint64]countedRowBucket,
	label string,
) ([]types.ProductValue, error) {
	for _, ref := range order {
		row := rows[ref.hash].row(ref.overflowIndex)
		n := row.count
		if n < 0 {
			panic(fmt.Sprintf("subscription: negative %s count %d for row key", label, n))
		}
		for i := 0; i < n; i++ {
			if err := chargeDeltaRow(ctx, row.row); err != nil {
				return nil, err
			}
			out = append(out, row.row)
		}
	}
	return out, nil
}

// encodeRowKey returns a deterministic byte string identifying row for use
// as a map key. The length prefix prevents ambiguity across rows of
// different column counts. Not a wire format.
func encodeRowKey(row types.ProductValue) string {
	enc := acquireCanonicalEncoder()
	defer releaseCanonicalEncoder(enc)
	enc.writeLen(len(row))
	for _, v := range row {
		encodeValue(enc, v)
	}
	return string(enc.buf)
}

package subscription

import (
	"fmt"

	"github.com/ponchione/shunter/types"
)

// ReconcileJoinDelta cancels insert/delete join fragments with bag semantics.
// Scratch state is pooled to avoid hot-path allocation churn.
func ReconcileJoinDelta(insertFragments, deleteFragments [][]types.ProductValue) (inserts, deletes []types.ProductValue) {
	st := dedupPool.Get().(*dedupState)
	defer func() {
		st.clear()
		dedupPool.Put(st)
	}()

	for _, frag := range insertFragments {
		for _, row := range frag {
			key := encodeRowKey(row)
			if _, ok := st.insertRows[key]; !ok {
				st.insertRows[key] = row
				st.insertOrder = append(st.insertOrder, key)
			}
			st.insertCounts[key]++
		}
	}

	for _, frag := range deleteFragments {
		for _, row := range frag {
			key := encodeRowKey(row)
			if st.insertCounts[key] > 0 {
				st.insertCounts[key]--
			} else {
				if _, ok := st.deleteRows[key]; !ok {
					st.deleteRows[key] = row
					st.deleteOrder = append(st.deleteOrder, key)
				}
				st.deleteCounts[key]++
			}
		}
	}

	inserts = appendReconciledRows(inserts, st.insertOrder, st.insertCounts, st.insertRows, "insert")
	deletes = appendReconciledRows(deletes, st.deleteOrder, st.deleteCounts, st.deleteRows, "delete")
	return inserts, deletes
}

func appendReconciledRows(
	out []types.ProductValue,
	order []string,
	counts map[string]int,
	rows map[string]types.ProductValue,
	label string,
) []types.ProductValue {
	for _, k := range order {
		n := counts[k]
		if n < 0 {
			panic(fmt.Sprintf("subscription: negative %s count %d for row key", label, n))
		}
		for i := 0; i < n; i++ {
			out = append(out, rows[k])
		}
	}
	return out
}

// encodeRowKey returns a deterministic byte string identifying row for use
// as a map key. The length prefix prevents ambiguity across rows of
// different column counts. Not a wire format.
func encodeRowKey(row types.ProductValue) string {
	enc := acquireCanonicalEncoder()
	defer releaseCanonicalEncoder(enc)
	enc.writeU32(uint32(len(row)))
	for _, v := range row {
		encodeValue(enc, v)
	}
	return string(enc.buf)
}

package subscription

import (
	"fmt"

	"github.com/ponchione/shunter/types"
)

// ReconcileJoinDelta resolves the 8 join fragments using bag semantics
// (SPEC-004 §6.3). Rows appearing in both insert and delete fragments cancel
// one-for-one; remaining positive counts become inserts, remaining delete
// counts become deletes. Multiplicity is preserved.
//
// Keyed by a canonical byte encoding of the row so structurally identical
// rows from different fragments match without any interface{} comparison.
// Internal scratch state is pooled (see dedupPool) so repeated evaluation
// on the hot path does not churn allocations (SPEC-004 §9.2).
func ReconcileJoinDelta(insertFragments, deleteFragments [][]types.ProductValue) (inserts, deletes []types.ProductValue) {
	st := dedupPool.Get().(*dedupState)
	defer func() {
		st.clear()
		dedupPool.Put(st)
	}()

	for _, frag := range insertFragments {
		for _, row := range frag {
			key := encodeRowKey(row)
			st.insertCounts[key]++
			if _, ok := st.insertRows[key]; !ok {
				st.insertRows[key] = row
			}
		}
	}

	for _, frag := range deleteFragments {
		for _, row := range frag {
			key := encodeRowKey(row)
			if st.insertCounts[key] > 0 {
				st.insertCounts[key]--
			} else {
				st.deleteCounts[key]++
				if _, ok := st.deleteRows[key]; !ok {
					st.deleteRows[key] = row
				}
			}
		}
	}

	for k, n := range st.insertCounts {
		if n < 0 {
			panic(fmt.Sprintf("subscription: negative insert count %d for row key", n))
		}
		for i := 0; i < n; i++ {
			inserts = append(inserts, st.insertRows[k])
		}
	}
	for k, n := range st.deleteCounts {
		if n < 0 {
			panic(fmt.Sprintf("subscription: negative delete count %d for row key", n))
		}
		for i := 0; i < n; i++ {
			deletes = append(deletes, st.deleteRows[k])
		}
	}
	return inserts, deletes
}

// encodeRowKey returns a deterministic byte string identifying row for use
// as a map key. The length prefix prevents ambiguity across rows of
// different column counts. Not a wire format.
func encodeRowKey(row types.ProductValue) string {
	enc := encoderPool.Get().(*canonicalEncoder)
	defer func() {
		enc.reset()
		encoderPool.Put(enc)
	}()
	enc.writeU32(uint32(len(row)))
	for _, v := range row {
		encodeValue(enc, v)
	}
	return string(enc.buf)
}

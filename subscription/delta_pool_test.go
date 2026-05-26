package subscription

import (
	"reflect"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestCanonicalEncoderPoolReturnsClearedDefaultSizedBuffers(t *testing.T) {
	for i := 0; i < 8; i++ {
		enc := acquireCanonicalEncoder()
		if len(enc.buf) != 0 {
			t.Fatalf("iteration %d: acquireCanonicalEncoder len = %d, want 0", i, len(enc.buf))
		}
		if cap(enc.buf) != pooledBufferDefaultCap {
			t.Fatalf("iteration %d: acquireCanonicalEncoder cap = %d, want %d", i, cap(enc.buf), pooledBufferDefaultCap)
		}

		enc.writeByte(byte(i))
		releaseCanonicalEncoder(enc)

		reused := acquireCanonicalEncoder()
		if len(reused.buf) != 0 {
			t.Fatalf("iteration %d: reused encoder len = %d, want 0", i, len(reused.buf))
		}
		if cap(reused.buf) != pooledBufferDefaultCap {
			t.Fatalf("iteration %d: reused encoder cap = %d, want %d", i, cap(reused.buf), pooledBufferDefaultCap)
		}
		reused.writeByte(9)
		if reused.buf[0] != 9 {
			t.Fatalf("iteration %d: reused encoder first byte = %d, want 9", i, reused.buf[0])
		}
		releaseCanonicalEncoder(reused)
	}
}

func TestCanonicalEncoderPoolDropsOversizedBuffers(t *testing.T) {
	enc := acquireCanonicalEncoder()
	enc.buf = make([]byte, 1, pooledBufferDefaultCap*2)
	releaseCanonicalEncoder(enc)

	next := acquireCanonicalEncoder()
	if cap(next.buf) != pooledBufferDefaultCap {
		t.Fatalf("next encoder cap = %d, want %d", cap(next.buf), pooledBufferDefaultCap)
	}
	releaseCanonicalEncoder(next)
}

func TestProductValueSlicePoolDropsOversizedSlices(t *testing.T) {
	oversized := make([]types.ProductValue, 1, pooledProductValueSliceMaxCap*2)
	ptr := slicePtr(oversized)
	releaseProductValueSlice(oversized)

	next := acquireProductValueSlice(1)
	next = append(next, types.ProductValue{types.NewUint64(1)})
	if slicePtr(next) == ptr {
		t.Fatal("oversized ProductValue slice backing array should not be retained in the pool")
	}
	releaseProductValueSlice(next)
}

func TestCandidateScratchReleaseClearsMapsBeforeReuse(t *testing.T) {
	for i := 0; i < 8; i++ {
		st := acquireCandidateScratch()
		st.candidates[hashN(byte(i+1))] = struct{}{}
		st.distinct[encodeValueKey(types.NewString("x"))] = types.NewUint64(uint64(i + 1))
		st.distinctKeys = append(st.distinctKeys, encodeValueKey(types.NewString("retained-key")))
		releaseCandidateScratch(st)

		reused := acquireCandidateScratch()
		if len(reused.candidates) != 0 {
			t.Fatalf("iteration %d: reused candidate set len = %d, want 0", i, len(reused.candidates))
		}
		if len(reused.distinct) != 0 {
			t.Fatalf("iteration %d: reused distinct map len = %d, want 0", i, len(reused.distinct))
		}
		if len(reused.distinctKeys) != 0 {
			t.Fatalf("iteration %d: reused distinct key scratch len = %d, want 0", i, len(reused.distinctKeys))
		}
		reused.candidates[hashN(byte(i+100))] = struct{}{}
		reused.distinct[encodeValueKey(types.NewString("fresh"))] = types.NewUint64(uint64(i + 100))
		reused.distinctKeys = append(reused.distinctKeys, encodeValueKey(types.NewString("fresh-key")))
		releaseCandidateScratch(reused)
	}
}

func TestDeltaViewReleaseClearsInsertDeleteSlicesBeforeReuse(t *testing.T) {
	for i := 0; i < 8; i++ {
		cs1 := simpleChangeset(1,
			[]types.ProductValue{{types.NewUint64(1), types.NewString("a")}, {types.NewUint64(2), types.NewString("b")}},
			[]types.ProductValue{{types.NewUint64(3), types.NewString("c")}, {types.NewUint64(4), types.NewString("d")}},
		)
		dv1 := NewDeltaView(nil, cs1, map[TableID][]ColID{1: {0}})
		dv1.Release()

		cs2 := simpleChangeset(1,
			[]types.ProductValue{{types.NewUint64(uint64(i + 9)), types.NewString("z")}},
			[]types.ProductValue{{types.NewUint64(uint64(i + 8)), types.NewString("y")}},
		)
		dv2 := NewDeltaView(nil, cs2, map[TableID][]ColID{1: {0}})

		if got := dv2.InsertedRows(1); len(got) != 1 || got[0][0].AsUint64() != uint64(i+9) {
			t.Fatalf("iteration %d: reused inserts = %v, want one fresh row", i, got)
		}
		if got := dv2.DeletedRows(1); len(got) != 1 || got[0][0].AsUint64() != uint64(i+8) {
			t.Fatalf("iteration %d: reused deletes = %v, want one fresh row", i, got)
		}
		dv2.Release()
	}
}

func slicePtr[T any](s []T) uintptr {
	return reflect.ValueOf(&s[0]).Pointer()
}

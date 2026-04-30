package subscription

import (
	"reflect"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestBufferPoolReturnsClearedDefaultSizedBuffers(t *testing.T) {
	for i := 0; i < 8; i++ {
		buf := acquirePooledBuffer()
		if len(buf) != 0 {
			t.Fatalf("iteration %d: acquirePooledBuffer len = %d, want 0", i, len(buf))
		}
		if cap(buf) != pooledBufferDefaultCap {
			t.Fatalf("iteration %d: acquirePooledBuffer cap = %d, want %d", i, cap(buf), pooledBufferDefaultCap)
		}

		buf = append(buf, byte(i), byte(i+1), byte(i+2))
		releasePooledBuffer(buf)

		reused := acquirePooledBuffer()
		if len(reused) != 0 {
			t.Fatalf("iteration %d: reused buffer len = %d, want 0", i, len(reused))
		}
		if cap(reused) != pooledBufferDefaultCap {
			t.Fatalf("iteration %d: reused buffer cap = %d, want %d", i, cap(reused), pooledBufferDefaultCap)
		}
		reused = append(reused, 9)
		if reused[0] != 9 {
			t.Fatalf("iteration %d: reused buffer first byte = %d, want 9", i, reused[0])
		}
		releasePooledBuffer(reused)
	}
}

func TestBufferPoolDropsOversizedBuffers(t *testing.T) {
	oversized := make([]byte, 0, pooledBufferDefaultCap*2)
	ptr := slicePtr(oversized[:1])
	releasePooledBuffer(oversized)

	next := acquirePooledBuffer()
	if cap(next) != pooledBufferDefaultCap {
		t.Fatalf("next buffer cap = %d, want %d", cap(next), pooledBufferDefaultCap)
	}
	if slicePtr(next[:1]) == ptr {
		t.Fatalf("oversized buffer backing array should not be retained in the pool")
	}
}

func TestCandidateScratchReleaseClearsMapsBeforeReuse(t *testing.T) {
	for i := 0; i < 8; i++ {
		st := acquireCandidateScratch()
		st.candidates[hashN(byte(i+1))] = struct{}{}
		st.distinct[encodeValueKey(types.NewString("x"))] = types.NewUint64(uint64(i + 1))
		releaseCandidateScratch(st)

		reused := acquireCandidateScratch()
		if len(reused.candidates) != 0 {
			t.Fatalf("iteration %d: reused candidate set len = %d, want 0", i, len(reused.candidates))
		}
		if len(reused.distinct) != 0 {
			t.Fatalf("iteration %d: reused distinct map len = %d, want 0", i, len(reused.distinct))
		}
		reused.candidates[hashN(byte(i+100))] = struct{}{}
		reused.distinct[encodeValueKey(types.NewString("fresh"))] = types.NewUint64(uint64(i + 100))
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

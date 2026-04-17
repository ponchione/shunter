package subscription

import (
	"reflect"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestBufferPoolReusesDefaultSizedBuffers(t *testing.T) {
	buf := acquirePooledBuffer()
	if len(buf) != 0 {
		t.Fatalf("acquirePooledBuffer len = %d, want 0", len(buf))
	}
	if cap(buf) != pooledBufferDefaultCap {
		t.Fatalf("acquirePooledBuffer cap = %d, want %d", cap(buf), pooledBufferDefaultCap)
	}

	buf = append(buf, 1, 2, 3)
	ptr := slicePtr(buf)
	releasePooledBuffer(buf)

	reused := acquirePooledBuffer()
	if len(reused) != 0 {
		t.Fatalf("reused buffer len = %d, want 0", len(reused))
	}
	if cap(reused) != pooledBufferDefaultCap {
		t.Fatalf("reused buffer cap = %d, want %d", cap(reused), pooledBufferDefaultCap)
	}
	if slicePtr(reused[:1]) != ptr {
		t.Fatalf("expected pooled buffer backing array to be reused")
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

func TestCandidateScratchReusedAndCleared(t *testing.T) {
	st := acquireCandidateScratch()
	st.candidates[hashN(1)] = struct{}{}
	st.distinct["x"] = types.NewUint64(1)
	candPtr := reflect.ValueOf(st.candidates).Pointer()
	distinctPtr := reflect.ValueOf(st.distinct).Pointer()
	releaseCandidateScratch(st)

	reused := acquireCandidateScratch()
	defer releaseCandidateScratch(reused)
	if len(reused.candidates) != 0 {
		t.Fatalf("reused candidate set len = %d, want 0", len(reused.candidates))
	}
	if len(reused.distinct) != 0 {
		t.Fatalf("reused distinct map len = %d, want 0", len(reused.distinct))
	}
	if reflect.ValueOf(reused.candidates).Pointer() != candPtr {
		t.Fatalf("expected candidate set map allocation to be reused")
	}
	if reflect.ValueOf(reused.distinct).Pointer() != distinctPtr {
		t.Fatalf("expected distinct-value map allocation to be reused")
	}
}

func TestDeltaViewReleaseReusesInsertDeleteBackingSlices(t *testing.T) {
	cs1 := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(1), types.NewString("a")}, {types.NewUint64(2), types.NewString("b")}},
		[]types.ProductValue{{types.NewUint64(3), types.NewString("c")}, {types.NewUint64(4), types.NewString("d")}},
	)
	dv1 := NewDeltaView(nil, cs1, map[TableID][]ColID{1: {0}})
	insertPtr := slicePtr(dv1.InsertedRows(1)[:1])
	deletePtr := slicePtr(dv1.DeletedRows(1)[:1])
	dv1.Release()

	cs2 := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(9), types.NewString("z")}},
		[]types.ProductValue{{types.NewUint64(8), types.NewString("y")}},
	)
	dv2 := NewDeltaView(nil, cs2, map[TableID][]ColID{1: {0}})
	defer dv2.Release()

	if got := dv2.InsertedRows(1); len(got) != 1 || got[0][0].AsUint64() != 9 {
		t.Fatalf("reused inserts = %v, want one fresh row", got)
	}
	if got := dv2.DeletedRows(1); len(got) != 1 || got[0][0].AsUint64() != 8 {
		t.Fatalf("reused deletes = %v, want one fresh row", got)
	}
	if slicePtr(dv2.InsertedRows(1)[:1]) != insertPtr {
		t.Fatalf("expected insert backing storage to be reused")
	}
	if slicePtr(dv2.DeletedRows(1)[:1]) != deletePtr {
		t.Fatalf("expected delete backing storage to be reused")
	}
}

func slicePtr[T any](s []T) uintptr {
	return reflect.ValueOf(&s[0]).Pointer()
}

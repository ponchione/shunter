package subscription

import (
	"fmt"
	"testing"

	"github.com/ponchione/shunter/types"
)

func FuzzComputeQueryHashCanonicalization(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Add([]byte("same-table canonicalization"))
	f.Add([]byte{0xff, 0, 0x7f, 0x80, 0x40, 0x20, 0x10})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := newQueryHashFuzzReader(data)
		table := TableID(1 + r.u32()%4)
		a := r.leaf(table, 0)
		b := r.leaf(table, 0)
		c := r.leaf(table, 0)
		label := queryHashFuzzLabel(data, table, a, b, c)

		assertFuzzQueryHashDeterministic(t, label, a)
		assertFuzzQueryHashEqual(t, label, "same-table And child order", And{Left: a, Right: b}, And{Left: b, Right: a})
		assertFuzzQueryHashEqual(t, label, "same-table Or child order", Or{Left: a, Right: b}, Or{Left: b, Right: a})
		assertFuzzQueryHashEqual(t, label, "same-table And grouping", And{Left: And{Left: a, Right: b}, Right: c}, And{Left: c, Right: And{Left: b, Right: a}})
		assertFuzzQueryHashEqual(t, label, "same-table Or grouping", Or{Left: Or{Left: a, Right: b}, Right: c}, Or{Left: c, Right: Or{Left: b, Right: a}})
		assertFuzzQueryHashEqual(t, label, "duplicate And leaf", a, And{Left: a, Right: a})
		assertFuzzQueryHashEqual(t, label, "duplicate Or leaf", a, Or{Left: a, Right: a})
		assertFuzzQueryHashEqual(t, label, "Or absorption", a, Or{Left: a, Right: And{Left: a, Right: b}})
		assertFuzzQueryHashEqual(t, label, "And absorption", a, And{Left: a, Right: Or{Left: a, Right: b}})

		clientA := r.identity()
		clientB := clientA
		clientB[31] ^= 1
		query := And{Left: a, Right: Or{Left: b, Right: c}}
		assertFuzzQueryHashParameterized(t, label, query, clientA, clientB)

		leftCol := ColID(r.u32() % 8)
		rightCol := ColID(r.u32() % 8)
		projectRight := r.bool()
		selfJoin := Join{
			Left:         table,
			Right:        table,
			LeftCol:      leftCol,
			RightCol:     rightCol,
			LeftAlias:    0,
			RightAlias:   1,
			ProjectRight: projectRight,
			Filter:       And{Left: a, Right: b},
		}
		selfJoinSwapped := selfJoin
		selfJoinSwapped.Filter = And{Left: b, Right: a}
		assertFuzzQueryHashEqual(t, label, "self-join filter child order", selfJoin, selfJoinSwapped)

		aliasDrift := selfJoin
		aliasDrift.Filter = And{Left: withFuzzLeafAlias(a, 1), Right: b}
		assertFuzzQueryHashDifferent(t, label, "self-join filter alias", selfJoin, aliasDrift)
	})
}

type queryHashFuzzReader struct {
	data []byte
	pos  int
}

func newQueryHashFuzzReader(data []byte) *queryHashFuzzReader {
	return &queryHashFuzzReader{data: data}
}

func (r *queryHashFuzzReader) byte() byte {
	if r.pos >= len(r.data) {
		b := byte(17 + r.pos*37)
		r.pos++
		return b
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

func (r *queryHashFuzzReader) bool() bool {
	return r.byte()%2 == 0
}

func (r *queryHashFuzzReader) u32() uint32 {
	var out uint32
	for i := 0; i < 4; i++ {
		out = (out << 8) | uint32(r.byte())
	}
	return out
}

func (r *queryHashFuzzReader) u64() uint64 {
	var out uint64
	for i := 0; i < 8; i++ {
		out = (out << 8) | uint64(r.byte())
	}
	return out
}

func (r *queryHashFuzzReader) identity() types.Identity {
	var id types.Identity
	for i := range id {
		id[i] = r.byte()
	}
	return id
}

func (r *queryHashFuzzReader) leaf(table TableID, alias uint8) Predicate {
	col := ColID(r.u32() % 8)
	switch r.byte() % 3 {
	case 0:
		return ColEq{Table: table, Column: col, Alias: alias, Value: r.value()}
	case 1:
		return ColNe{Table: table, Column: col, Alias: alias, Value: r.value()}
	default:
		return ColRange{Table: table, Column: col, Alias: alias, Lower: r.bound(), Upper: r.bound()}
	}
}

func (r *queryHashFuzzReader) bound() Bound {
	if r.byte()%5 == 0 {
		return Bound{Unbounded: true}
	}
	return Bound{Value: r.value(), Inclusive: r.bool()}
}

func (r *queryHashFuzzReader) value() Value {
	switch r.byte() % 12 {
	case 0:
		return types.NewBool(r.bool())
	case 1:
		return types.NewInt64(int64(r.u64()))
	case 2:
		return types.NewUint64(r.u64())
	case 3:
		return types.NewString(r.asciiString(16))
	case 4:
		return types.NewBytes(r.bytes(16))
	case 5:
		return types.NewInt128(int64(r.u64()), r.u64())
	case 6:
		return types.NewUint128(r.u64(), r.u64())
	case 7:
		return types.NewInt256(int64(r.u64()), r.u64(), r.u64(), r.u64())
	case 8:
		return types.NewUint256(r.u64(), r.u64(), r.u64(), r.u64())
	case 9:
		return types.NewTimestamp(int64(r.u64()))
	case 10:
		v, err := types.NewFloat32(float32(int32(r.u32()%200_001)-100_000) / 10)
		if err != nil {
			panic(err)
		}
		return v
	default:
		return types.NewArrayString(r.stringArray())
	}
}

func (r *queryHashFuzzReader) asciiString(max int) string {
	n := int(r.byte() % byte(max+1))
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = 'a' + r.byte()%26
	}
	return string(buf)
}

func (r *queryHashFuzzReader) bytes(max int) []byte {
	n := int(r.byte() % byte(max+1))
	out := make([]byte, n)
	for i := range out {
		out[i] = r.byte()
	}
	return out
}

func (r *queryHashFuzzReader) stringArray() []string {
	n := int(r.byte() % 5)
	out := make([]string, n)
	for i := range out {
		out[i] = r.asciiString(8)
	}
	return out
}

func withFuzzLeafAlias(pred Predicate, alias uint8) Predicate {
	switch p := pred.(type) {
	case ColEq:
		p.Alias = alias
		return p
	case ColNe:
		p.Alias = alias
		return p
	case ColRange:
		p.Alias = alias
		return p
	default:
		panic("fuzz generated non-leaf predicate")
	}
}

func assertFuzzQueryHashDeterministic(t *testing.T, label string, pred Predicate) {
	t.Helper()
	h1 := ComputeQueryHash(pred, nil)
	h2 := ComputeQueryHash(pred, nil)
	if h1 != h2 {
		t.Fatalf("query hash is not deterministic: first=%s second=%s %s", h1, h2, label)
	}
}

func assertFuzzQueryHashEqual(t *testing.T, label, invariant string, left, right Predicate) {
	t.Helper()
	leftHash := ComputeQueryHash(left, nil)
	rightHash := ComputeQueryHash(right, nil)
	if leftHash != rightHash {
		t.Fatalf("%s changed hash: left=%s right=%s %s", invariant, leftHash, rightHash, label)
	}
}

func assertFuzzQueryHashDifferent(t *testing.T, label, invariant string, left, right Predicate) {
	t.Helper()
	leftHash := ComputeQueryHash(left, nil)
	rightHash := ComputeQueryHash(right, nil)
	if leftHash == rightHash {
		t.Fatalf("%s did not change hash: hash=%s %s", invariant, leftHash, label)
	}
}

func assertFuzzQueryHashParameterized(t *testing.T, label string, pred Predicate, clientA, clientB types.Identity) {
	t.Helper()
	base := ComputeQueryHash(pred, nil)
	a1 := ComputeQueryHash(pred, &clientA)
	a2 := ComputeQueryHash(pred, &clientA)
	b := ComputeQueryHash(pred, &clientB)
	if a1 != a2 {
		t.Fatalf("parameterized hash is not deterministic: first=%s second=%s %s", a1, a2, label)
	}
	if base == a1 {
		t.Fatalf("client identity did not change base hash: hash=%s %s", base, label)
	}
	if a1 == b {
		t.Fatalf("different client identities hashed equally: hash=%s %s", a1, label)
	}
}

func queryHashFuzzLabel(data []byte, table TableID, a, b, c Predicate) string {
	if len(data) <= 64 {
		return fmt.Sprintf("seed_len=%d seed=%x table=%d a=%#v b=%#v c=%#v", len(data), data, table, a, b, c)
	}
	return fmt.Sprintf("seed_len=%d seed_prefix=%x table=%d a=%#v b=%#v c=%#v", len(data), data[:64], table, a, b, c)
}

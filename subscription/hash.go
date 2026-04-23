package subscription

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"sort"
	"sync"

	"lukechampine.com/blake3"

	"github.com/ponchione/shunter/types"
)

// QueryHash is the 32-byte blake3 digest of a predicate's canonical form.
// It is used as the deduplication key across the subscription manager and
// pruning indexes.
type QueryHash [32]byte

// String returns the hex encoding of the hash.
func (h QueryHash) String() string { return hex.EncodeToString(h[:]) }

// Canonical serialization tags. These are internal — not a wire format.
// Only requirement is determinism within a single binary version.
const (
	tagColEq     byte = 0x01
	tagColNe     byte = 0x02
	tagColRange  byte = 0x03
	tagAnd       byte = 0x04
	tagAllRows   byte = 0x05
	tagJoin      byte = 0x06
	tagOr        byte = 0x07
	tagCrossJoin byte = 0x08
	tagNoRows    byte = 0x09
)

// Within a canonical Bound encoding.
const (
	boundUnbounded byte = 0x00
	boundExclusive byte = 0x01
	boundInclusive byte = 0x02
)

var encoderPool = sync.Pool{
	New: func() any { return &canonicalEncoder{} },
}

type canonicalEncoder struct {
	buf []byte
}

func acquireCanonicalEncoder() *canonicalEncoder {
	enc := encoderPool.Get().(*canonicalEncoder)
	if enc.buf == nil {
		enc.buf = acquirePooledBuffer()
	} else {
		enc.buf = enc.buf[:0]
	}
	return enc
}

func releaseCanonicalEncoder(enc *canonicalEncoder) {
	releasePooledBuffer(enc.buf)
	enc.buf = nil
	encoderPool.Put(enc)
}

func (e *canonicalEncoder) reset() { e.buf = e.buf[:0] }

func (e *canonicalEncoder) writeByte(b byte) { e.buf = append(e.buf, b) }

func (e *canonicalEncoder) writeU32(v uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], v)
	e.buf = append(e.buf, tmp[:]...)
}

func (e *canonicalEncoder) writeU64(v uint64) {
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], v)
	e.buf = append(e.buf, tmp[:]...)
}

func isAllRowsPredicate(pred Predicate) bool {
	_, ok := pred.(AllRows)
	return ok
}

func isNoRowsPredicate(pred Predicate) bool {
	_, ok := pred.(NoRows)
	return ok
}

func sameCanonicalTable(left, right Predicate) bool {
	leftTable, ok := canonicalGroupTable(left)
	if !ok {
		return false
	}
	rightTable, ok := canonicalGroupTable(right)
	if !ok {
		return false
	}
	return leftTable == rightTable
}

func singlePredicateTable(pred Predicate) (TableID, bool) {
	if pred == nil {
		return 0, false
	}
	tables := pred.Tables()
	if len(tables) != 1 {
		return 0, false
	}
	return tables[0], true
}

func containsJoinLikePredicate(pred Predicate) bool {
	switch p := pred.(type) {
	case nil:
		return false
	case And:
		return containsJoinLikePredicate(p.Left) || containsJoinLikePredicate(p.Right)
	case Or:
		return containsJoinLikePredicate(p.Left) || containsJoinLikePredicate(p.Right)
	case Join, CrossJoin:
		return true
	default:
		return false
	}
}

func canonicalGroupTable(pred Predicate) (TableID, bool) {
	if containsJoinLikePredicate(pred) {
		return 0, false
	}
	return singlePredicateTable(pred)
}

func canReorderCommutativeChildren(left, right Predicate) bool {
	leftTable, ok := canonicalGroupTable(left)
	if !ok {
		return false
	}
	rightTable, ok := canonicalGroupTable(right)
	if !ok {
		return false
	}
	return leftTable == rightTable
}

func canonicalPredicateBytes(pred Predicate) []byte {
	enc := acquireCanonicalEncoder()
	defer releaseCanonicalEncoder(enc)
	encodePredicate(enc, pred)
	return append([]byte(nil), enc.buf...)
}

func orderCanonicalChildren(left, right Predicate) (Predicate, Predicate) {
	if !canReorderCommutativeChildren(left, right) {
		return left, right
	}
	leftBytes := canonicalPredicateBytes(left)
	rightBytes := canonicalPredicateBytes(right)
	if bytes.Compare(leftBytes, rightBytes) <= 0 {
		return left, right
	}
	return right, left
}

func flattenCanonicalAnd(pred Predicate, table TableID, out []Predicate) []Predicate {
	switch p := pred.(type) {
	case And:
		if predTable, ok := canonicalGroupTable(p); ok && predTable == table {
			out = flattenCanonicalAnd(p.Left, table, out)
			out = flattenCanonicalAnd(p.Right, table, out)
			return out
		}
	}
	return append(out, pred)
}

func flattenCanonicalOr(pred Predicate, table TableID, out []Predicate) []Predicate {
	switch p := pred.(type) {
	case Or:
		if predTable, ok := canonicalGroupTable(p); ok && predTable == table {
			out = flattenCanonicalOr(p.Left, table, out)
			out = flattenCanonicalOr(p.Right, table, out)
			return out
		}
	}
	return append(out, pred)
}

func sortCanonicalPredicates(preds []Predicate) {
	if len(preds) < 2 {
		return
	}
	type canonicalPredicate struct {
		pred Predicate
		key  []byte
	}
	ordered := make([]canonicalPredicate, len(preds))
	for i, pred := range preds {
		ordered[i] = canonicalPredicate{pred: pred, key: canonicalPredicateBytes(pred)}
	}
	sort.Slice(ordered, func(i, j int) bool {
		return bytes.Compare(ordered[i].key, ordered[j].key) < 0
	})
	for i := range ordered {
		preds[i] = ordered[i].pred
	}
}

func dedupeCanonicalPredicates(preds []Predicate) []Predicate {
	if len(preds) < 2 {
		return preds
	}
	out := preds[:1]
	prevKey := canonicalPredicateBytes(preds[0])
	for _, pred := range preds[1:] {
		key := canonicalPredicateBytes(pred)
		if bytes.Equal(prevKey, key) {
			continue
		}
		out = append(out, pred)
		prevKey = key
	}
	return out
}

func absorbCanonicalPredicates(preds []Predicate, groupTag byte, table TableID) []Predicate {
	if len(preds) < 2 {
		return preds
	}
	present := make(map[string]struct{}, len(preds))
	for _, pred := range preds {
		present[string(canonicalPredicateBytes(pred))] = struct{}{}
	}
	out := preds[:0]
	for _, pred := range preds {
		if shouldAbsorbCanonicalPredicate(pred, groupTag, table, present) {
			continue
		}
		out = append(out, pred)
	}
	if len(out) == 0 {
		return preds
	}
	return out
}

func shouldAbsorbCanonicalPredicate(pred Predicate, groupTag byte, table TableID, present map[string]struct{}) bool {
	var targetChildren []Predicate
	switch groupTag {
	case tagOr:
		andPred, ok := pred.(And)
		if !ok {
			return false
		}
		if predTable, ok := canonicalGroupTable(andPred); !ok || predTable != table {
			return false
		}
		targetChildren = flattenCanonicalAnd(andPred, table, nil)
	case tagAnd:
		orPred, ok := pred.(Or)
		if !ok {
			return false
		}
		if predTable, ok := canonicalGroupTable(orPred); !ok || predTable != table {
			return false
		}
		targetChildren = flattenCanonicalOr(orPred, table, nil)
	default:
		return false
	}
	for _, child := range targetChildren {
		if _, ok := present[string(canonicalPredicateBytes(child))]; ok {
			return true
		}
	}
	return false
}

func rebuildCanonicalAnd(preds []Predicate) Predicate {
	if len(preds) == 0 {
		return nil
	}
	result := preds[0]
	for i := 1; i < len(preds); i++ {
		result = And{Left: result, Right: preds[i]}
	}
	return result
}

func rebuildCanonicalOr(preds []Predicate) Predicate {
	if len(preds) == 0 {
		return nil
	}
	result := preds[0]
	for i := 1; i < len(preds); i++ {
		result = Or{Left: result, Right: preds[i]}
	}
	return result
}

func orderSelfJoinCanonicalChildren(left, right Predicate) (Predicate, Predicate) {
	leftBytes := canonicalPredicateBytes(left)
	rightBytes := canonicalPredicateBytes(right)
	if bytes.Compare(leftBytes, rightBytes) <= 0 {
		return left, right
	}
	return right, left
}

func flattenSelfJoinCanonicalAnd(pred Predicate, out []Predicate) []Predicate {
	switch p := pred.(type) {
	case And:
		out = flattenSelfJoinCanonicalAnd(p.Left, out)
		out = flattenSelfJoinCanonicalAnd(p.Right, out)
		return out
	default:
		return append(out, pred)
	}
}

func flattenSelfJoinCanonicalOr(pred Predicate, out []Predicate) []Predicate {
	switch p := pred.(type) {
	case Or:
		out = flattenSelfJoinCanonicalOr(p.Left, out)
		out = flattenSelfJoinCanonicalOr(p.Right, out)
		return out
	default:
		return append(out, pred)
	}
}

func canonicalizeSelfJoinFilter(pred Predicate) Predicate {
	switch p := pred.(type) {
	case And:
		left := canonicalizeSelfJoinFilter(p.Left)
		right := canonicalizeSelfJoinFilter(p.Right)
		if left == nil || right == nil {
			return And{Left: left, Right: right}
		}
		children := flattenSelfJoinCanonicalAnd(And{Left: left, Right: right}, nil)
		sortCanonicalPredicates(children)
		return rebuildCanonicalAnd(children)
	case Or:
		left := canonicalizeSelfJoinFilter(p.Left)
		right := canonicalizeSelfJoinFilter(p.Right)
		if left == nil || right == nil {
			return Or{Left: left, Right: right}
		}
		children := flattenSelfJoinCanonicalOr(Or{Left: left, Right: right}, nil)
		sortCanonicalPredicates(children)
		return rebuildCanonicalOr(children)
	case Join, CrossJoin:
		return p
	default:
		return pred
	}
}

func canonicalizePredicate(pred Predicate) Predicate {
	switch p := pred.(type) {
	case And:
		left := canonicalizePredicate(p.Left)
		right := canonicalizePredicate(p.Right)
		if left == nil || right == nil {
			return And{Left: left, Right: right}
		}
		if isNoRowsPredicate(left) && sameCanonicalTable(left, right) {
			return left
		}
		if isNoRowsPredicate(right) && sameCanonicalTable(left, right) {
			return right
		}
		if isAllRowsPredicate(left) && sameCanonicalTable(left, right) {
			return right
		}
		if isAllRowsPredicate(right) && sameCanonicalTable(left, right) {
			return left
		}
		combined := And{Left: left, Right: right}
		if table, ok := canonicalGroupTable(combined); ok {
			children := flattenCanonicalAnd(combined, table, nil)
			sortCanonicalPredicates(children)
			children = dedupeCanonicalPredicates(children)
			children = absorbCanonicalPredicates(children, tagAnd, table)
			return rebuildCanonicalAnd(children)
		}
		left, right = orderCanonicalChildren(left, right)
		return And{Left: left, Right: right}
	case Or:
		left := canonicalizePredicate(p.Left)
		right := canonicalizePredicate(p.Right)
		if left == nil || right == nil {
			return Or{Left: left, Right: right}
		}
		if isAllRowsPredicate(left) && sameCanonicalTable(left, right) {
			return left
		}
		if isAllRowsPredicate(right) && sameCanonicalTable(left, right) {
			return right
		}
		if isNoRowsPredicate(left) && sameCanonicalTable(left, right) {
			return right
		}
		if isNoRowsPredicate(right) && sameCanonicalTable(left, right) {
			return left
		}
		combined := Or{Left: left, Right: right}
		if table, ok := canonicalGroupTable(combined); ok {
			children := flattenCanonicalOr(combined, table, nil)
			sortCanonicalPredicates(children)
			children = dedupeCanonicalPredicates(children)
			children = absorbCanonicalPredicates(children, tagOr, table)
			return rebuildCanonicalOr(children)
		}
		left, right = orderCanonicalChildren(left, right)
		return Or{Left: left, Right: right}
	case Join:
		if p.Filter == nil {
			return p
		}
		if p.Left == p.Right {
			p.Filter = canonicalizeSelfJoinFilter(p.Filter)
			return p
		}
		p.Filter = canonicalizePredicate(p.Filter)
		return p
	default:
		return pred
	}
}

// ComputeQueryHash returns the blake3-32 digest of the predicate's canonical
// form. When clientID is non-nil, the identity is appended so structurally
// identical predicates from different clients produce different hashes
// (parameterized form, SPEC-004 §3.4).
func ComputeQueryHash(pred Predicate, clientID *types.Identity) QueryHash {
	if pred == nil {
		panic("subscription: ComputeQueryHash on nil predicate")
	}
	pred = canonicalizePredicate(pred)
	enc := acquireCanonicalEncoder()
	defer releaseCanonicalEncoder(enc)
	encodePredicate(enc, pred)
	if clientID != nil {
		enc.buf = append(enc.buf, clientID[:]...)
	}
	h := QueryHash(blake3.Sum256(enc.buf))
	return h
}

func encodePredicate(e *canonicalEncoder, pred Predicate) {
	switch p := pred.(type) {
	case ColEq:
		e.writeByte(tagColEq)
		e.writeU32(uint32(p.Table))
		e.writeU32(uint32(p.Column))
		e.writeByte(p.Alias)
		encodeValue(e, p.Value)
	case ColNe:
		e.writeByte(tagColNe)
		e.writeU32(uint32(p.Table))
		e.writeU32(uint32(p.Column))
		e.writeByte(p.Alias)
		encodeValue(e, p.Value)
	case ColRange:
		e.writeByte(tagColRange)
		e.writeU32(uint32(p.Table))
		e.writeU32(uint32(p.Column))
		e.writeByte(p.Alias)
		encodeBound(e, p.Lower)
		encodeBound(e, p.Upper)
	case And:
		e.writeByte(tagAnd)
		encodePredicate(e, p.Left)
		encodePredicate(e, p.Right)
	case Or:
		e.writeByte(tagOr)
		encodePredicate(e, p.Left)
		encodePredicate(e, p.Right)
	case AllRows:
		e.writeByte(tagAllRows)
		e.writeU32(uint32(p.Table))
	case NoRows:
		e.writeByte(tagNoRows)
		e.writeU32(uint32(p.Table))
	case Join:
		e.writeByte(tagJoin)
		e.writeU32(uint32(p.Left))
		e.writeU32(uint32(p.Right))
		e.writeU32(uint32(p.LeftCol))
		e.writeU32(uint32(p.RightCol))
		e.writeByte(p.LeftAlias)
		e.writeByte(p.RightAlias)
		if p.ProjectRight {
			e.writeByte(1)
		} else {
			e.writeByte(0)
		}
		if p.Filter == nil {
			e.writeByte(0)
		} else {
			e.writeByte(1)
			encodePredicate(e, p.Filter)
		}
	case CrossJoin:
		e.writeByte(tagCrossJoin)
		e.writeU32(uint32(p.Left))
		e.writeU32(uint32(p.Right))
		e.writeByte(p.LeftAlias)
		e.writeByte(p.RightAlias)
		if p.ProjectRight {
			e.writeByte(1)
		} else {
			e.writeByte(0)
		}
	default:
		// Sealed interface — no external impls reach this point.
		panic("subscription: unknown predicate type")
	}
}

func encodeBound(e *canonicalEncoder, b Bound) {
	if b.Unbounded {
		e.writeByte(boundUnbounded)
		return
	}
	if b.Inclusive {
		e.writeByte(boundInclusive)
	} else {
		e.writeByte(boundExclusive)
	}
	encodeValue(e, b.Value)
}

func encodeValue(e *canonicalEncoder, v Value) {
	k := v.Kind()
	e.writeByte(byte(k))
	switch k {
	case types.KindBool:
		if v.AsBool() {
			e.writeByte(1)
		} else {
			e.writeByte(0)
		}
	case types.KindInt8:
		e.writeU64(uint64(int64(v.AsInt8())))
	case types.KindInt16:
		e.writeU64(uint64(int64(v.AsInt16())))
	case types.KindInt32:
		e.writeU64(uint64(int64(v.AsInt32())))
	case types.KindInt64:
		e.writeU64(uint64(v.AsInt64()))
	case types.KindUint8:
		e.writeU64(uint64(v.AsUint8()))
	case types.KindUint16:
		e.writeU64(uint64(v.AsUint16()))
	case types.KindUint32:
		e.writeU64(uint64(v.AsUint32()))
	case types.KindUint64:
		e.writeU64(v.AsUint64())
	case types.KindFloat32:
		e.writeU32(math.Float32bits(v.AsFloat32()))
	case types.KindFloat64:
		e.writeU64(math.Float64bits(v.AsFloat64()))
	case types.KindString:
		s := v.AsString()
		e.writeU32(uint32(len(s)))
		e.buf = append(e.buf, s...)
	case types.KindBytes:
		b := v.AsBytes()
		e.writeU32(uint32(len(b)))
		e.buf = append(e.buf, b...)
	case types.KindInt128:
		hi, lo := v.AsInt128()
		e.writeU64(uint64(hi))
		e.writeU64(lo)
	case types.KindUint128:
		hi, lo := v.AsUint128()
		e.writeU64(hi)
		e.writeU64(lo)
	case types.KindInt256:
		w0, w1, w2, w3 := v.AsInt256()
		e.writeU64(uint64(w0))
		e.writeU64(w1)
		e.writeU64(w2)
		e.writeU64(w3)
	case types.KindUint256:
		w0, w1, w2, w3 := v.AsUint256()
		e.writeU64(w0)
		e.writeU64(w1)
		e.writeU64(w2)
		e.writeU64(w3)
	case types.KindTimestamp:
		e.writeU64(uint64(v.AsTimestamp()))
	case types.KindArrayString:
		xs := v.AsArrayString()
		e.writeU32(uint32(len(xs)))
		for _, s := range xs {
			e.writeU32(uint32(len(s)))
			e.buf = append(e.buf, s...)
		}
	default:
		panic("subscription: encodeValue unhandled kind")
	}
}

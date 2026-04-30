package commitlog

import "github.com/ponchione/shunter/types"

type fuzzByteReader struct {
	data []byte
	pos  int
}

func newFuzzByteReader(data []byte) *fuzzByteReader {
	return &fuzzByteReader{data: data}
}

func (r *fuzzByteReader) byte() byte {
	if r.pos >= len(r.data) {
		b := byte(19 + r.pos*29)
		r.pos++
		return b
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

func (r *fuzzByteReader) txID(max uint64) types.TxID {
	var out uint64
	for i := 0; i < 8; i++ {
		out = (out << 8) | uint64(r.byte())
	}
	if max == 0 {
		return types.TxID(out)
	}
	return types.TxID(out % (max + 1))
}

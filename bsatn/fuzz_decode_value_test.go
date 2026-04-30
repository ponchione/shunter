package bsatn

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ponchione/shunter/types"
)

func FuzzDecodeValue(f *testing.F) {
	for _, v := range []types.Value{
		types.NewBool(true),
		types.NewInt64(-42),
		types.NewUint64(42),
		mustFuzzFloat32(f, 1.5),
		mustFuzzFloat64(f, -2.25),
		types.NewString("alice"),
		types.NewBytes([]byte{0xde, 0xad, 0xbe, 0xef}),
		types.NewArrayString([]string{"north", "", "south"}),
		types.NewInt128(-1, ^uint64(0)),
		types.NewUint128(^uint64(0), ^uint64(0)),
		types.NewInt256(-1, ^uint64(0), ^uint64(0), ^uint64(0)),
		types.NewUint256(1, 2, 3, 4),
		types.NewTimestamp(1_739_201_130_000_000),
	} {
		encoded := mustAppendFuzzValue(f, v)
		f.Add(encoded)
		for n := 0; n < len(encoded); n++ {
			f.Add(append([]byte(nil), encoded[:n]...))
		}
	}

	f.Add([]byte{})
	f.Add([]byte{0xff})
	f.Add([]byte{TagString, 0x01, 0, 0, 0, 0xff})
	f.Add([]byte{TagArrayString, 0x01, 0, 0, 0, 0x01, 0, 0, 0, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		if !boundedFuzzValueInput(data) {
			return
		}

		got, err := DecodeValue(bytes.NewReader(data))
		if err != nil {
			assertClassifiedFuzzBSATNError(t, "DecodeValue", data, err)
			return
		}

		appended, err := AppendValue(nil, got)
		if err != nil {
			t.Fatalf("AppendValue accepted value: %v %s", err, fuzzBSATNInputLabel(data))
		}
		if EncodedValueSize(got) != len(appended) {
			t.Fatalf("EncodedValueSize = %d, encoded len = %d %s", EncodedValueSize(got), len(appended), fuzzBSATNInputLabel(data))
		}
		var written bytes.Buffer
		if err := EncodeValue(&written, got); err != nil {
			t.Fatalf("EncodeValue accepted value: %v %s", err, fuzzBSATNInputLabel(data))
		}
		if !bytes.Equal(appended, written.Bytes()) {
			t.Fatalf("append/write encoding mismatch: append=%x write=%x %s", appended, written.Bytes(), fuzzBSATNInputLabel(data))
		}

		decodedAgain, err := DecodeValue(bytes.NewReader(appended))
		if err != nil {
			t.Fatalf("canonical decode failed: %v encoded=%x original=%s", err, appended, fuzzBSATNInputLabel(data))
		}
		if !got.Equal(decodedAgain) {
			t.Fatalf("canonical round trip mismatch: encoded=%x original=%s", appended, fuzzBSATNInputLabel(data))
		}
		appendedAgain, err := AppendValue(nil, decodedAgain)
		if err != nil {
			t.Fatalf("AppendValue decoded value: %v encoded=%x original=%s", err, appended, fuzzBSATNInputLabel(data))
		}
		if !bytes.Equal(appended, appendedAgain) {
			t.Fatalf("canonical encoding is unstable: first=%x second=%x original=%s", appended, appendedAgain, fuzzBSATNInputLabel(data))
		}
	})
}

func mustFuzzFloat32(tb testing.TB, x float32) types.Value {
	tb.Helper()
	v, err := types.NewFloat32(x)
	if err != nil {
		tb.Fatalf("NewFloat32 seed: %v", err)
	}
	return v
}

func mustFuzzFloat64(tb testing.TB, x float64) types.Value {
	tb.Helper()
	v, err := types.NewFloat64(x)
	if err != nil {
		tb.Fatalf("NewFloat64 seed: %v", err)
	}
	return v
}

func mustAppendFuzzValue(tb testing.TB, v types.Value) []byte {
	tb.Helper()
	encoded, err := AppendValue(nil, v)
	if err != nil {
		tb.Fatalf("AppendValue seed: %v", err)
	}
	return encoded
}

func boundedFuzzValueInput(data []byte) bool {
	if len(data) > maxFuzzProductValueInputBytes {
		return false
	}
	if len(data) == 0 {
		return true
	}
	switch data[0] {
	case TagString, TagBytes:
		if len(data) < 5 {
			return true
		}
		n := binary.LittleEndian.Uint32(data[1:5])
		return n <= maxFuzzProductValueBlobBytes
	case TagArrayString:
		if len(data) < 5 {
			return true
		}
		count := binary.LittleEndian.Uint32(data[1:5])
		if count > maxFuzzProductValueArrayCount {
			return false
		}
		pos := 5
		for range count {
			if len(data)-pos < 4 {
				return true
			}
			n := binary.LittleEndian.Uint32(data[pos : pos+4])
			if n > maxFuzzProductValueBlobBytes {
				return false
			}
			pos += 4 + int(n)
			if pos > len(data) {
				return true
			}
		}
	}
	return true
}

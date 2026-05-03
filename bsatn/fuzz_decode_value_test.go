package bsatn

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ponchione/shunter/types"
)

func FuzzDecodeValue(f *testing.F) {
	for _, seed := range decodeValueFuzzSeeds(f) {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if !boundedFuzzValueInput(data) {
			return
		}
		assertDecodeValueFuzzInput(t, data)
	})
}

func decodeValueFuzzSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	var seeds [][]byte
	for _, v := range []types.Value{
		types.NewBool(true),
		types.NewInt64(-42),
		types.NewUint64(42),
		mustFuzzFloat32(tb, 1.5),
		mustFuzzFloat64(tb, -2.25),
		types.NewString("alice"),
		types.NewBytes([]byte{0xde, 0xad, 0xbe, 0xef}),
		types.NewArrayString([]string{"north", "", "south"}),
		types.NewInt128(-1, ^uint64(0)),
		types.NewUint128(^uint64(0), ^uint64(0)),
		types.NewInt256(-1, ^uint64(0), ^uint64(0), ^uint64(0)),
		types.NewUint256(1, 2, 3, 4),
		types.NewTimestamp(1_739_201_130_000_000),
		types.NewUUID([16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}),
	} {
		encoded := mustAppendFuzzValue(tb, v)
		seeds = append(seeds, encoded)
		for n := 0; n < len(encoded); n++ {
			seeds = append(seeds, append([]byte(nil), encoded[:n]...))
		}
	}

	seeds = append(seeds,
		[]byte{},
		[]byte{0xff},
		[]byte{TagString, 0x01, 0, 0, 0, 0xff},
		[]byte{TagArrayString, 0x01, 0, 0, 0, 0x01, 0, 0, 0, 0xff},
	)
	return seeds
}

func assertDecodeValueFuzzInput(tb testing.TB, data []byte) {
	tb.Helper()
	if err := checkDecodeValueFuzzInput(data); err != nil {
		tb.Fatal(err)
	}
}

func checkDecodeValueFuzzInput(data []byte) error {
	got, err := DecodeValue(bytes.NewReader(data))
	if err != nil {
		if err := checkClassifiedFuzzBSATNError("DecodeValue", data, err); err != nil {
			return err
		}
		return nil
	}

	appended, err := AppendValue(nil, got)
	if err != nil {
		return fmt.Errorf("AppendValue accepted value: %v %s", err, fuzzBSATNInputLabel(data))
	}
	if EncodedValueSize(got) != len(appended) {
		return fmt.Errorf("EncodedValueSize = %d, encoded len = %d %s", EncodedValueSize(got), len(appended), fuzzBSATNInputLabel(data))
	}
	var written bytes.Buffer
	if err := EncodeValue(&written, got); err != nil {
		return fmt.Errorf("EncodeValue accepted value: %v %s", err, fuzzBSATNInputLabel(data))
	}
	if !bytes.Equal(appended, written.Bytes()) {
		return fmt.Errorf("append/write encoding mismatch: append=%x write=%x %s", appended, written.Bytes(), fuzzBSATNInputLabel(data))
	}

	decodedAgain, err := DecodeValue(bytes.NewReader(appended))
	if err != nil {
		return fmt.Errorf("canonical decode failed: %v encoded=%x original=%s", err, appended, fuzzBSATNInputLabel(data))
	}
	if !got.Equal(decodedAgain) {
		return fmt.Errorf("canonical round trip mismatch: encoded=%x original=%s", appended, fuzzBSATNInputLabel(data))
	}
	appendedAgain, err := AppendValue(nil, decodedAgain)
	if err != nil {
		return fmt.Errorf("AppendValue decoded value: %v encoded=%x original=%s", err, appended, fuzzBSATNInputLabel(data))
	}
	if !bytes.Equal(appended, appendedAgain) {
		return fmt.Errorf("canonical encoding is unstable: first=%x second=%x original=%s", appended, appendedAgain, fuzzBSATNInputLabel(data))
	}
	return nil
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

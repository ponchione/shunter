package bsatn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

var fuzzProductValueSchema = &schema.TableSchema{
	Name: "fuzz_products",
	Columns: []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: types.KindUint64},
		{Index: 1, Name: "active", Type: types.KindBool},
		{Index: 2, Name: "name", Type: types.KindString},
		{Index: 3, Name: "payload", Type: types.KindBytes},
		{Index: 4, Name: "labels", Type: types.KindArrayString},
		{Index: 5, Name: "signed_wide", Type: types.KindInt128},
		{Index: 6, Name: "stamp", Type: types.KindTimestamp},
		{Index: 7, Name: "wide", Type: types.KindUint256},
	},
}

func FuzzDecodeProductValueFromBytes(f *testing.F) {
	for _, seed := range decodeProductValueFuzzSeeds(f) {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if !boundedFuzzProductValueInput(data, fuzzProductValueSchema) {
			return
		}
		assertDecodeProductValueFuzzInput(t, data)
	})
}

func decodeProductValueFuzzSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	var seeds [][]byte
	for _, row := range []types.ProductValue{
		{
			types.NewUint64(1),
			types.NewBool(false),
			types.NewString("alice"),
			types.NewBytes([]byte{0xde, 0xad}),
			types.NewArrayString([]string{"admin", "north"}),
			types.NewInt128(-1, ^uint64(0)),
			types.NewTimestamp(1_739_201_130_000_000),
			types.NewUint256(1, 2, 3, 4),
		},
		{
			types.NewUint64(0),
			types.NewBool(true),
			types.NewString(""),
			types.NewBytes(nil),
			types.NewArrayString(nil),
			types.NewInt128(0, 0),
			types.NewTimestamp(0),
			types.NewUint256(0, 0, 0, 0),
		},
	} {
		encoded := mustAppendFuzzProductValue(tb, row)
		seeds = append(seeds, encoded)
		for n := 0; n < len(encoded); n++ {
			seeds = append(seeds, append([]byte(nil), encoded[:n]...))
		}
		withTrailing := append(append([]byte(nil), encoded...), 0xff)
		seeds = append(seeds, withTrailing)
	}

	seeds = append(seeds,
		[]byte{},
		[]byte{TagUint64},
		[]byte{TagString, 0x7f, 0, 0, 0},
		fuzzProductValueWithInvalidNameUTF8(),
	)
	return seeds
}

func assertDecodeProductValueFuzzInput(tb testing.TB, data []byte) {
	tb.Helper()
	if err := checkDecodeProductValueFuzzInput(data); err != nil {
		tb.Fatal(err)
	}
}

func checkDecodeProductValueFuzzInput(data []byte) error {
	fromBytes, fromBytesErr := DecodeProductValueFromBytes(data, fuzzProductValueSchema)
	fromReader, fromReaderErr := DecodeProductValue(bytes.NewReader(data), fuzzProductValueSchema)
	if fromBytesErr != nil {
		if err := checkClassifiedFuzzBSATNError("DecodeProductValueFromBytes", data, fromBytesErr); err != nil {
			return err
		}
	}
	if fromReaderErr != nil {
		if err := checkClassifiedFuzzBSATNError("DecodeProductValue", data, fromReaderErr); err != nil {
			return err
		}
	}
	if (fromBytesErr == nil) != (fromReaderErr == nil) {
		return fmt.Errorf("decode success mismatch: fromBytesErr=%v fromReaderErr=%v %s", fromBytesErr, fromReaderErr, fuzzBSATNInputLabel(data))
	}
	if fromBytesErr != nil {
		return nil
	}
	if !fromBytes.Equal(fromReader) {
		return fmt.Errorf("decode value mismatch %s", fuzzBSATNInputLabel(data))
	}

	appended, err := AppendProductValue(nil, fromBytes)
	if err != nil {
		return fmt.Errorf("AppendProductValue accepted row: %v %s", err, fuzzBSATNInputLabel(data))
	}
	var written bytes.Buffer
	if err := EncodeProductValue(&written, fromBytes); err != nil {
		return fmt.Errorf("EncodeProductValue accepted row: %v %s", err, fuzzBSATNInputLabel(data))
	}
	if !bytes.Equal(appended, written.Bytes()) {
		return fmt.Errorf("append/write encoding mismatch: append=%x write=%x %s", appended, written.Bytes(), fuzzBSATNInputLabel(data))
	}

	decodedAgain, err := DecodeProductValueFromBytes(appended, fuzzProductValueSchema)
	if err != nil {
		return fmt.Errorf("canonical decode failed: %v encoded=%x original=%s", err, appended, fuzzBSATNInputLabel(data))
	}
	if !fromBytes.Equal(decodedAgain) {
		return fmt.Errorf("canonical round trip mismatch: encoded=%x original=%s", appended, fuzzBSATNInputLabel(data))
	}
	appendedAgain, err := AppendProductValue(nil, decodedAgain)
	if err != nil {
		return fmt.Errorf("AppendProductValue decoded row: %v encoded=%x original=%s", err, appended, fuzzBSATNInputLabel(data))
	}
	if !bytes.Equal(appended, appendedAgain) {
		return fmt.Errorf("canonical encoding is unstable: first=%x second=%x original=%s", appended, appendedAgain, fuzzBSATNInputLabel(data))
	}
	return nil
}

func mustAppendFuzzProductValue(tb testing.TB, row types.ProductValue) []byte {
	tb.Helper()
	encoded, err := AppendProductValue(nil, row)
	if err != nil {
		tb.Fatalf("AppendProductValue seed: %v", err)
	}
	return encoded
}

func fuzzProductValueWithInvalidNameUTF8() []byte {
	row := types.ProductValue{
		types.NewUint64(7),
		types.NewBool(true),
		types.NewString("ok"),
		types.NewBytes([]byte{1}),
		types.NewArrayString([]string{"x"}),
		types.NewInt128(0, 1),
		types.NewTimestamp(1),
		types.NewUint256(0, 0, 0, 1),
	}
	encoded, err := AppendProductValue(nil, row)
	if err != nil {
		panic(err)
	}
	// id: tag+8, active: tag+1, name tag+u32 length, then the name payload.
	namePayload := 9 + 2 + 1 + 4
	encoded[namePayload] = 0xff
	return encoded
}

const (
	maxFuzzProductValueInputBytes = 512
	maxFuzzProductValueBlobBytes  = 128
	maxFuzzProductValueArrayCount = 8
)

func boundedFuzzProductValueInput(data []byte, ts *schema.TableSchema) bool {
	if len(data) > maxFuzzProductValueInputBytes {
		return false
	}
	pos := 0
	for _, col := range ts.Columns {
		if pos >= len(data) {
			return true
		}
		tag := data[pos]
		pos++
		if tag != byte(col.Type) {
			return true
		}
		switch col.Type {
		case types.KindBool, types.KindInt8, types.KindUint8:
			pos++
		case types.KindInt16, types.KindUint16:
			pos += 2
		case types.KindInt32, types.KindUint32, types.KindFloat32:
			pos += 4
		case types.KindInt64, types.KindUint64, types.KindFloat64, types.KindTimestamp:
			pos += 8
		case types.KindInt128, types.KindUint128:
			pos += 16
		case types.KindInt256, types.KindUint256:
			pos += 32
		case types.KindString, types.KindBytes:
			n, ok := readFuzzU32(data, pos)
			if !ok {
				return true
			}
			if n > maxFuzzProductValueBlobBytes {
				return false
			}
			pos += 4 + int(n)
		case types.KindArrayString:
			count, ok := readFuzzU32(data, pos)
			if !ok {
				return true
			}
			if count > maxFuzzProductValueArrayCount {
				return false
			}
			pos += 4
			for range count {
				n, ok := readFuzzU32(data, pos)
				if !ok {
					return true
				}
				if n > maxFuzzProductValueBlobBytes {
					return false
				}
				pos += 4 + int(n)
				if pos > len(data) {
					return true
				}
			}
		}
		if pos > len(data) {
			return true
		}
	}
	return true
}

func readFuzzU32(data []byte, pos int) (uint32, bool) {
	if pos < 0 || len(data)-pos < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(data[pos : pos+4]), true
}

func checkClassifiedFuzzBSATNError(op string, data []byte, err error) error {
	var shapeErr *RowShapeMismatchError
	var tagErr *TypeTagMismatchError
	var unknownTagErr *UnknownValueTagError
	if errors.Is(err, ErrRowLengthMismatch) ||
		errors.Is(err, ErrInvalidUTF8) ||
		errors.Is(err, types.ErrInvalidFloat) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.As(err, &shapeErr) ||
		errors.As(err, &tagErr) ||
		errors.As(err, &unknownTagErr) {
		return nil
	}
	return fmt.Errorf("%s returned unclassified error %T: %v %s", op, err, err, fuzzBSATNInputLabel(data))
}

func fuzzBSATNInputLabel(data []byte) string {
	if len(data) <= 80 {
		return fmt.Sprintf("len=%d data=%x", len(data), data)
	}
	return fmt.Sprintf("len=%d data_prefix=%x", len(data), data[:80])
}

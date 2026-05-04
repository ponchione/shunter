package schema

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGoTypeToValueKindScalars(t *testing.T) {
	cases := []struct {
		goType reflect.Type
		want   ValueKind
	}{
		{reflect.TypeFor[bool](), KindBool},
		{reflect.TypeFor[int8](), KindInt8},
		{reflect.TypeFor[uint8](), KindUint8},
		{reflect.TypeFor[int16](), KindInt16},
		{reflect.TypeFor[uint16](), KindUint16},
		{reflect.TypeFor[int32](), KindInt32},
		{reflect.TypeFor[uint32](), KindUint32},
		{reflect.TypeFor[int64](), KindInt64},
		{reflect.TypeFor[uint64](), KindUint64},
		{reflect.TypeFor[float32](), KindFloat32},
		{reflect.TypeFor[float64](), KindFloat64},
		{reflect.TypeFor[string](), KindString},
		{reflect.TypeFor[time.Time](), KindTimestamp},
		{reflect.TypeFor[time.Duration](), KindDuration},
		{reflect.TypeFor[[]byte](), KindBytes},
		{reflect.TypeFor[[16]byte](), KindUUID},
	}
	for _, c := range cases {
		got, err := GoTypeToValueKind(c.goType)
		if err != nil {
			t.Errorf("%v: unexpected error: %v", c.goType, err)
			continue
		}
		if got != c.want {
			t.Errorf("%v: got %v, want %v", c.goType, got, c.want)
		}
	}
}

type Score int64
type ID uint64
type Blob []byte
type UUIDBytes [16]byte
type EventTime time.Time
type NamedByte uint8
type NotBlob []NamedByte

func TestGoTypeToValueKindNamedTypes(t *testing.T) {
	cases := []struct {
		goType reflect.Type
		want   ValueKind
	}{
		{reflect.TypeFor[Score](), KindInt64},
		{reflect.TypeFor[ID](), KindUint64},
		{reflect.TypeFor[Blob](), KindBytes},
		{reflect.TypeFor[UUIDBytes](), KindUUID},
		{reflect.TypeFor[EventTime](), KindTimestamp},
	}
	for _, c := range cases {
		got, err := GoTypeToValueKind(c.goType)
		if err != nil {
			t.Errorf("%v: unexpected error: %v", c.goType, err)
			continue
		}
		if got != c.want {
			t.Errorf("%v: got %v, want %v", c.goType, got, c.want)
		}
	}
}

func TestGoTypeToValueKindUnsupported(t *testing.T) {
	unsupported := []reflect.Type{
		nil,
		reflect.TypeFor[[]string](),
		reflect.TypeFor[NotBlob](),
		reflect.TypeFor[map[string]int](),
		reflect.TypeFor[*int64](),
		reflect.TypeFor[int](),
		reflect.TypeFor[uint](),
		reflect.TypeFor[any](),
		reflect.TypeFor[struct{}](),
		reflect.TypeFor[chan int](),
		reflect.TypeFor[func()](),
	}
	for _, rt := range unsupported {
		_, err := GoTypeToValueKind(rt)
		if err == nil {
			t.Errorf("%v: expected error, got nil", rt)
			continue
		}
		if !errors.Is(err, ErrUnsupportedFieldType) {
			t.Errorf("%v: error should wrap ErrUnsupportedFieldType, got: %v", rt, err)
		}
	}
}

func TestGoTypeToValueKindErrorIncludesTypeName(t *testing.T) {
	type rejectedCase struct {
		name string
		rt   reflect.Type
	}
	for _, tc := range []rejectedCase{{name: "map[string]int", rt: reflect.TypeFor[map[string]int]()}, {name: "<nil>", rt: nil}} {
		_, err := GoTypeToValueKind(tc.rt)
		if err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
		if !strings.Contains(err.Error(), tc.name) {
			t.Fatalf("%s: error %q should include rejected type name", tc.name, err.Error())
		}
	}
}

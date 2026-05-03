package schema

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrUnsupportedFieldType is returned when a Go type cannot be mapped to a ValueKind.
var ErrUnsupportedFieldType = errors.New("unsupported field type")

var byteSliceType = reflect.TypeFor[[]byte]()
var byteElemType = reflect.TypeFor[byte]()

// GoTypeToValueKind maps a Go reflect.Type to the corresponding ValueKind.
// Named types are resolved through their underlying kind.
// Returns ErrUnsupportedFieldType for unsupported types.
func GoTypeToValueKind(t reflect.Type) (ValueKind, error) {
	if t == nil {
		return 0, fmt.Errorf("%w: <nil>", ErrUnsupportedFieldType)
	}

	// Check []byte before generic slice rejection.
	if t == byteSliceType || (t.Kind() == reflect.Slice && t.Elem() == byteElemType) {
		return KindBytes, nil
	}
	if t.Kind() == reflect.Array && t.Len() == 16 && t.Elem() == byteElemType {
		return KindUUID, nil
	}

	switch t.Kind() {
	case reflect.Bool:
		return KindBool, nil
	case reflect.Int8:
		return KindInt8, nil
	case reflect.Uint8:
		return KindUint8, nil
	case reflect.Int16:
		return KindInt16, nil
	case reflect.Uint16:
		return KindUint16, nil
	case reflect.Int32:
		return KindInt32, nil
	case reflect.Uint32:
		return KindUint32, nil
	case reflect.Int64:
		return KindInt64, nil
	case reflect.Uint64:
		return KindUint64, nil
	case reflect.Float32:
		return KindFloat32, nil
	case reflect.Float64:
		return KindFloat64, nil
	case reflect.String:
		return KindString, nil
	case reflect.Int, reflect.Uint:
		return 0, fmt.Errorf("%w: %s (use explicit-width int64 or uint64)", ErrUnsupportedFieldType, t)
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedFieldType, t)
	}
}

package contractworkflow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// JSONRow is a product row converted to JSON-ready values keyed by column name.
type JSONRow map[string]any

// ProductValueToJSONRow converts a schema-aligned ProductValue to a JSON-ready object.
func ProductValueToJSONRow(product schema.ProductSchemaExport, row types.ProductValue) (JSONRow, error) {
	columns, err := productColumnsForBSATN(product)
	if err != nil {
		return nil, err
	}
	return productValueToJSONRowForColumns(columns, row)
}

// DecodedQueryRowsToJSONRows converts decoded declared-query rows to JSON-ready objects.
func DecodedQueryRowsToJSONRows(decoded DecodedQueryRows) ([]JSONRow, error) {
	rows := make([]JSONRow, len(decoded.Rows))
	for i, row := range decoded.Rows {
		converted, err := productValueToJSONRowForColumns(decoded.Columns, row)
		if err != nil {
			return nil, fmt.Errorf("convert query %q row %d to JSON: %w", decoded.Name, i, err)
		}
		rows[i] = converted
	}
	return rows, nil
}

func productValueToJSONRowForColumns(columns []schema.ColumnSchema, row types.ProductValue) (JSONRow, error) {
	if len(row) != len(columns) {
		return nil, fmt.Errorf("%w: row has %d values, want %d columns", ErrProductValueShape, len(row), len(columns))
	}
	out := make(JSONRow, len(columns))
	for i, column := range columns {
		value, err := valueToJSON(column, row[i])
		if err != nil {
			return nil, err
		}
		out[column.Name] = value
	}
	return out, nil
}

func valueToJSON(column schema.ColumnSchema, value types.Value) (any, error) {
	if value.Kind() != column.Type {
		return nil, fmt.Errorf("%w: field %q has %s value, want %s", ErrProductValueShape, column.Name, value.Kind(), column.Type)
	}
	if value.IsNull() {
		if !column.Nullable {
			return nil, fmt.Errorf("%w: field %q is null but column is not nullable", ErrProductValueShape, column.Name)
		}
		return nil, nil
	}

	switch value.Kind() {
	case types.KindBool:
		return value.AsBool(), nil
	case types.KindInt8:
		return value.AsInt8(), nil
	case types.KindUint8:
		return value.AsUint8(), nil
	case types.KindInt16:
		return value.AsInt16(), nil
	case types.KindUint16:
		return value.AsUint16(), nil
	case types.KindInt32:
		return value.AsInt32(), nil
	case types.KindUint32:
		return value.AsUint32(), nil
	case types.KindInt64:
		return value.AsInt64(), nil
	case types.KindUint64:
		return value.AsUint64(), nil
	case types.KindFloat32:
		return value.AsFloat32(), nil
	case types.KindFloat64:
		return value.AsFloat64(), nil
	case types.KindString:
		return value.AsString(), nil
	case types.KindBytes:
		return base64.StdEncoding.EncodeToString(value.AsBytes()), nil
	case types.KindInt128:
		hi, lo := value.AsInt128()
		return signedWideString(hi, lo), nil
	case types.KindUint128:
		hi, lo := value.AsUint128()
		return unsignedWideString(hi, lo), nil
	case types.KindInt256:
		w0, w1, w2, w3 := value.AsInt256()
		return signedWideString(w0, w1, w2, w3), nil
	case types.KindUint256:
		w0, w1, w2, w3 := value.AsUint256()
		return unsignedWideString(w0, w1, w2, w3), nil
	case types.KindTimestamp:
		return value.AsTimestamp(), nil
	case types.KindArrayString:
		return value.AsArrayString(), nil
	case types.KindUUID:
		return value.UUIDString(), nil
	case types.KindDuration:
		return value.AsDurationMicros(), nil
	case types.KindJSON:
		return json.RawMessage(value.AsJSON()), nil
	default:
		return nil, fmt.Errorf("%w: field %q has unsupported kind %s", ErrUnsupportedArgumentType, column.Name, value.Kind())
	}
}

func signedWideString(high int64, words ...uint64) string {
	out := big.NewInt(high)
	for _, word := range words {
		out.Lsh(out, 64)
		out.Add(out, new(big.Int).SetUint64(word))
	}
	return out.String()
}

func unsignedWideString(words ...uint64) string {
	out := new(big.Int)
	for _, word := range words {
		out.Lsh(out, 64)
		out.Add(out, new(big.Int).SetUint64(word))
	}
	return out.String()
}

package contractworkflow

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/internal/wideint"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

var (
	ErrInvalidArgumentJSON     = errors.New("invalid argument JSON")
	ErrUnsupportedArgumentType = errors.New("unsupported argument type")
	ErrUnsupportedSurfaceKind  = errors.New("unsupported contract argument surface kind")
)

// ArgumentSurfaceKind identifies a contract surface that accepts BSATN product arguments.
type ArgumentSurfaceKind string

const (
	// ArgumentSurfaceReducer selects a reducer Args schema.
	ArgumentSurfaceReducer ArgumentSurfaceKind = "reducer"
	// ArgumentSurfaceProcedure selects a procedure Args schema.
	ArgumentSurfaceProcedure ArgumentSurfaceKind = "procedure"
	// ArgumentSurfaceDeclaredQuery selects a declared-query Parameters schema.
	ArgumentSurfaceDeclaredQuery ArgumentSurfaceKind = "declared_query"
)

// ReducerCallRequest describes a contract-validated reducer call request.
type ReducerCallRequest struct {
	Name      string
	Arguments []byte
}

// DeclaredQueryRequest describes a contract-validated declared-query request.
type DeclaredQueryRequest struct {
	Name          string
	Parameters    []byte
	HasParameters bool
}

// ProcedureCallRequest describes a contract-validated procedure call request.
type ProcedureCallRequest struct {
	Name      string
	Arguments []byte
}

// ProductValueFromJSON decodes a JSON object into schema-ordered product values.
func ProductValueFromJSON(product schema.ProductSchemaExport, data []byte) (types.ProductValue, error) {
	fields, err := decodeArgumentObject(data)
	if err != nil {
		return nil, err
	}

	columnsByName := make(map[string]schema.ProductColumnExport, len(product.Columns))
	for _, column := range product.Columns {
		columnsByName[column.Name] = column
	}
	for name := range fields {
		if _, ok := columnsByName[name]; !ok {
			return nil, fmt.Errorf("%w: unknown field %q", ErrInvalidArgumentJSON, name)
		}
	}

	out := make(types.ProductValue, len(product.Columns))
	for i, column := range product.Columns {
		raw, ok := fields[column.Name]
		if !ok {
			return nil, fmt.Errorf("%w: missing required field %q", ErrInvalidArgumentJSON, column.Name)
		}
		value, err := argumentValueFromJSON(column, raw)
		if err != nil {
			return nil, err
		}
		out[i] = value
	}
	return out, nil
}

// EncodeProductValueArguments decodes JSON arguments and encodes them as BSATN.
func EncodeProductValueArguments(product schema.ProductSchemaExport, data []byte) ([]byte, error) {
	row, err := ProductValueFromJSON(product, data)
	if err != nil {
		return nil, err
	}
	columns, err := productColumnsForBSATN(product)
	if err != nil {
		return nil, err
	}
	return bsatn.AppendProductValueForColumns(nil, row, columns)
}

// EncodeReducerArguments encodes JSON arguments for a named reducer contract surface.
func EncodeReducerArguments(contract shunter.ModuleContract, name string, data []byte) ([]byte, error) {
	product, err := ReducerArgumentSchema(contract, name)
	if err != nil {
		return nil, err
	}
	return EncodeProductValueArguments(product, data)
}

// PrepareReducerCallRequest validates a reducer and prepares its encoded argument request shape.
func PrepareReducerCallRequest(contract shunter.ModuleContract, name string, data []byte) (ReducerCallRequest, error) {
	reducer, ok := FindReducer(contract, name)
	surfaceName, encoded, err := prepareRequiredArgumentRequest("reducer", name, reducer.Name, reducer.Args, ok, data)
	if err != nil {
		return ReducerCallRequest{}, err
	}
	return ReducerCallRequest{
		Name:      surfaceName,
		Arguments: encoded,
	}, nil
}

// EncodeProcedureArguments encodes JSON arguments for a named procedure contract surface.
func EncodeProcedureArguments(contract shunter.ModuleContract, name string, data []byte) ([]byte, error) {
	product, err := ProcedureArgumentSchema(contract, name)
	if err != nil {
		return nil, err
	}
	return EncodeProductValueArguments(product, data)
}

// PrepareProcedureCallRequest validates a procedure and prepares its encoded argument request shape.
func PrepareProcedureCallRequest(contract shunter.ModuleContract, name string, data []byte) (ProcedureCallRequest, error) {
	procedure, ok := FindProcedure(contract, name)
	surfaceName, encoded, err := prepareRequiredArgumentRequest("procedure", name, procedure.Name, procedure.Args, ok, data)
	if err != nil {
		return ProcedureCallRequest{}, err
	}
	return ProcedureCallRequest{
		Name:      surfaceName,
		Arguments: encoded,
	}, nil
}

func prepareRequiredArgumentRequest(kind, lookupName, surfaceName string, args *schema.ProductSchemaExport, found bool, data []byte) (string, []byte, error) {
	if !found {
		return "", nil, fmt.Errorf("%w: %s %q", ErrSurfaceNotFound, kind, strings.TrimSpace(lookupName))
	}
	if args == nil {
		return "", nil, fmt.Errorf("%w: %s %q", ErrArgumentSchemaMissing, kind, surfaceName)
	}
	encoded, err := EncodeProductValueArguments(*args, data)
	if err != nil {
		return "", nil, err
	}
	return surfaceName, encoded, nil
}

// EncodeQueryArguments encodes JSON arguments for a named declared-query contract surface.
func EncodeQueryArguments(contract shunter.ModuleContract, name string, data []byte) ([]byte, error) {
	product, err := QueryArgumentSchema(contract, name)
	if err != nil {
		return nil, err
	}
	return EncodeProductValueArguments(product, data)
}

// EncodeOptionalQueryArguments validates a declared query and encodes JSON parameters when present.
func EncodeOptionalQueryArguments(contract shunter.ModuleContract, name string, data []byte, hasArguments bool) ([]byte, bool, error) {
	query, ok := FindQuery(contract, name)
	if !ok {
		return nil, false, fmt.Errorf("%w: query %q", ErrSurfaceNotFound, strings.TrimSpace(name))
	}
	return encodeOptionalQueryArguments(query, data, hasArguments)
}

// PrepareDeclaredQueryRequest validates a declared query and prepares its encoded parameter request shape.
func PrepareDeclaredQueryRequest(contract shunter.ModuleContract, name string, data []byte, hasArguments bool) (DeclaredQueryRequest, error) {
	query, ok := FindQuery(contract, name)
	if !ok {
		return DeclaredQueryRequest{}, fmt.Errorf("%w: query %q", ErrSurfaceNotFound, strings.TrimSpace(name))
	}
	encoded, hasParameters, err := encodeOptionalQueryArguments(query, data, hasArguments)
	if err != nil {
		return DeclaredQueryRequest{}, err
	}
	return DeclaredQueryRequest{
		Name:          query.Name,
		Parameters:    encoded,
		HasParameters: hasParameters,
	}, nil
}

func encodeOptionalQueryArguments(query shunter.QueryDescription, data []byte, hasArguments bool) ([]byte, bool, error) {
	if query.Parameters == nil || len(query.Parameters.Columns) == 0 {
		if hasArguments {
			fields, err := decodeArgumentObject(data)
			if err != nil {
				return nil, false, err
			}
			if len(fields) != 0 {
				return nil, false, fmt.Errorf("%w: query %q does not accept arguments", ErrInvalidArgumentJSON, query.Name)
			}
		}
		return nil, false, nil
	}
	if !hasArguments {
		return nil, false, fmt.Errorf("%w: query %q requires %d argument(s)", ErrArgumentSchemaMissing, query.Name, len(query.Parameters.Columns))
	}
	encoded, err := EncodeProductValueArguments(*query.Parameters, data)
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

// EncodeSurfaceArguments encodes JSON arguments for a named reducer or declared-query surface.
func EncodeSurfaceArguments(contract shunter.ModuleContract, kind ArgumentSurfaceKind, name string, data []byte) ([]byte, error) {
	switch kind {
	case ArgumentSurfaceReducer:
		return EncodeReducerArguments(contract, name, data)
	case ArgumentSurfaceProcedure:
		return EncodeProcedureArguments(contract, name, data)
	case ArgumentSurfaceDeclaredQuery:
		return EncodeQueryArguments(contract, name, data)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedSurfaceKind, kind)
	}
}

func productColumnsForBSATN(product schema.ProductSchemaExport) ([]schema.ColumnSchema, error) {
	columns := make([]schema.ColumnSchema, len(product.Columns))
	for i, column := range product.Columns {
		kind, ok := schema.ParseValueKindExportString(column.Type)
		if !ok {
			return nil, fmt.Errorf("%w: field %q has type %q", ErrUnsupportedArgumentType, column.Name, column.Type)
		}
		columns[i] = schema.ColumnSchema{
			Index:    i,
			Name:     column.Name,
			Type:     kind,
			Nullable: column.Nullable,
		}
	}
	return columns, nil
}

func decodeArgumentObject(data []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgumentJSON, err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("%w: arguments must be a JSON object", ErrInvalidArgumentJSON)
	}

	fields := map[string]json.RawMessage{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidArgumentJSON, err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("%w: object key token %T", ErrInvalidArgumentJSON, keyTok)
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("%w: duplicate field %q", ErrInvalidArgumentJSON, key)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidArgumentJSON, key, err)
		}
		fields[key] = raw
	}
	end, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgumentJSON, err)
	}
	if delim, ok := end.(json.Delim); !ok || delim != '}' {
		return nil, fmt.Errorf("%w: expected object end", ErrInvalidArgumentJSON)
	}
	if tok, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("%w: trailing token %v", ErrInvalidArgumentJSON, tok)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgumentJSON, err)
	}
	return fields, nil
}

func argumentValueFromJSON(column schema.ProductColumnExport, raw json.RawMessage) (types.Value, error) {
	kind, ok := schema.ParseValueKindExportString(column.Type)
	if !ok {
		return types.Value{}, fmt.Errorf("%w: field %q has type %q", ErrUnsupportedArgumentType, column.Name, column.Type)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if column.Nullable {
			return types.NewNull(kind), nil
		}
		return types.Value{}, fmt.Errorf("%w: field %q is not nullable", ErrInvalidArgumentJSON, column.Name)
	}

	switch kind {
	case types.KindBool:
		var v bool
		if err := decodeArgumentRaw(column.Name, raw, &v); err != nil {
			return types.Value{}, err
		}
		return types.NewBool(v), nil
	case types.KindInt8:
		v, err := decodeArgumentInt(column.Name, raw, 8)
		return types.NewInt8(int8(v)), err
	case types.KindUint8:
		v, err := decodeArgumentUint(column.Name, raw, 8)
		return types.NewUint8(uint8(v)), err
	case types.KindInt16:
		v, err := decodeArgumentInt(column.Name, raw, 16)
		return types.NewInt16(int16(v)), err
	case types.KindUint16:
		v, err := decodeArgumentUint(column.Name, raw, 16)
		return types.NewUint16(uint16(v)), err
	case types.KindInt32:
		v, err := decodeArgumentInt(column.Name, raw, 32)
		return types.NewInt32(int32(v)), err
	case types.KindUint32:
		v, err := decodeArgumentUint(column.Name, raw, 32)
		return types.NewUint32(uint32(v)), err
	case types.KindInt64:
		v, err := decodeArgumentInt(column.Name, raw, 64)
		return types.NewInt64(v), err
	case types.KindUint64:
		v, err := decodeArgumentUint(column.Name, raw, 64)
		return types.NewUint64(v), err
	case types.KindFloat32:
		v, err := decodeArgumentFloat(column.Name, raw, 32)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewFloat32(float32(v))
	case types.KindFloat64:
		v, err := decodeArgumentFloat(column.Name, raw, 64)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewFloat64(v)
	case types.KindString:
		v, err := decodeArgumentString(column.Name, raw)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewString(v), nil
	case types.KindBytes:
		v, err := decodeArgumentString(column.Name, raw)
		if err != nil {
			return types.Value{}, err
		}
		decoded, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return types.Value{}, fmt.Errorf("%w: field %q must be base64 bytes: %v", ErrInvalidArgumentJSON, column.Name, err)
		}
		return types.NewBytesOwned(decoded), nil
	case types.KindInt128:
		return decodeArgumentWide(column.Name, raw, "int128", wideint.IsInt128, wideint.Int128)
	case types.KindUint128:
		return decodeArgumentWide(column.Name, raw, "uint128", wideint.IsUint128, wideint.Uint128)
	case types.KindInt256:
		return decodeArgumentWide(column.Name, raw, "int256", wideint.IsInt256, wideint.Int256)
	case types.KindUint256:
		return decodeArgumentWide(column.Name, raw, "uint256", wideint.IsUint256, wideint.Uint256)
	case types.KindTimestamp:
		v, err := decodeArgumentInt(column.Name, raw, 64)
		return types.NewTimestamp(v), err
	case types.KindArrayString:
		var v []string
		if err := decodeArgumentRaw(column.Name, raw, &v); err != nil {
			return types.Value{}, err
		}
		return types.NewArrayStringOwned(v), nil
	case types.KindUUID:
		v, err := decodeArgumentString(column.Name, raw)
		if err != nil {
			return types.Value{}, err
		}
		return types.ParseUUID(v)
	case types.KindDuration:
		v, err := decodeArgumentInt(column.Name, raw, 64)
		return types.NewDuration(v), err
	case types.KindJSON:
		v, err := types.NewJSON(raw)
		if err != nil {
			return types.Value{}, fmt.Errorf("%w: field %q: %v", ErrInvalidArgumentJSON, column.Name, err)
		}
		return v, nil
	default:
		return types.Value{}, fmt.Errorf("%w: field %q has type %q", ErrUnsupportedArgumentType, column.Name, column.Type)
	}
}

func decodeArgumentRaw(columnName string, raw json.RawMessage, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: field %q: %v", ErrInvalidArgumentJSON, columnName, err)
	}
	if tok, err := dec.Token(); err == nil {
		return fmt.Errorf("%w: field %q has trailing token %v", ErrInvalidArgumentJSON, columnName, tok)
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: field %q: %v", ErrInvalidArgumentJSON, columnName, err)
	}
	return nil
}

func decodeArgumentString(columnName string, raw json.RawMessage) (string, error) {
	var v string
	if err := decodeArgumentRaw(columnName, raw, &v); err != nil {
		return "", err
	}
	return v, nil
}

func decodeArgumentInt(columnName string, raw json.RawMessage, bitSize int) (int64, error) {
	text, err := decodeArgumentDecimalText(columnName, raw, fmt.Sprintf("int%d", bitSize))
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(text, 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("%w: field %q must be an int%d: %v", ErrInvalidArgumentJSON, columnName, bitSize, err)
	}
	return v, nil
}

func decodeArgumentUint(columnName string, raw json.RawMessage, bitSize int) (uint64, error) {
	text, err := decodeArgumentDecimalText(columnName, raw, fmt.Sprintf("uint%d", bitSize))
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(text, 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("%w: field %q must be a uint%d: %v", ErrInvalidArgumentJSON, columnName, bitSize, err)
	}
	return v, nil
}

func decodeArgumentFloat(columnName string, raw json.RawMessage, bitSize int) (float64, error) {
	var n json.Number
	if err := decodeArgumentRaw(columnName, raw, &n); err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(n.String(), bitSize)
	if err != nil || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%w: field %q must be a float%d: %v", ErrInvalidArgumentJSON, columnName, bitSize, err)
	}
	return v, nil
}

func decodeArgumentDecimalText(columnName string, raw json.RawMessage, typeName string) (string, error) {
	var n json.Number
	if err := decodeArgumentRaw(columnName, raw, &n); err == nil {
		return n.String(), nil
	}
	var s string
	if err := decodeArgumentRaw(columnName, raw, &s); err == nil {
		return s, nil
	}
	return "", fmt.Errorf("%w: field %q must be a %s", ErrInvalidArgumentJSON, columnName, typeName)
}

func decodeArgumentBigInt(columnName string, raw json.RawMessage, typeName string) (*big.Int, error) {
	text, err := decodeArgumentDecimalText(columnName, raw, typeName)
	if err != nil {
		return nil, err
	}
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("%w: field %q must be a %s", ErrInvalidArgumentJSON, columnName, typeName)
	}
	return n, nil
}

func decodeArgumentWide(columnName string, raw json.RawMessage, typeName string, inRange func(*big.Int) bool, mk func(*big.Int) types.Value) (types.Value, error) {
	n, err := decodeArgumentBigInt(columnName, raw, typeName)
	if err != nil {
		return types.Value{}, err
	}
	if !inRange(n) {
		return types.Value{}, fmt.Errorf("%w: field %q must be a %s", ErrInvalidArgumentJSON, columnName, typeName)
	}
	return mk(n), nil
}

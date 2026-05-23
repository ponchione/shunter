package contractworkflow

import (
	"fmt"
	"strings"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// DecodedQueryRows is a declared-query RowList decoded through contract metadata.
type DecodedQueryRows struct {
	Name      string
	TableName string
	Columns   []schema.ColumnSchema
	Rows      []types.ProductValue
}

// DecodedReducerResult is a reducer return value decoded through contract metadata.
type DecodedReducerResult struct {
	Name    string
	Columns []schema.ColumnSchema
	Row     types.ProductValue
}

// DecodeReducerResult decodes reducer return BSATN through its contract result schema.
func DecodeReducerResult(contract shunter.ModuleContract, name string, result []byte) (DecodedReducerResult, error) {
	reducer, ok := FindReducer(contract, name)
	if !ok {
		return DecodedReducerResult{}, fmt.Errorf("%w: reducer %q", ErrSurfaceNotFound, strings.TrimSpace(name))
	}
	if reducer.Result == nil {
		return DecodedReducerResult{}, fmt.Errorf("%w: reducer %q", ErrResultSchemaMissing, reducer.Name)
	}
	columns, err := productColumnsForBSATN(*reducer.Result)
	if err != nil {
		return DecodedReducerResult{}, err
	}
	tableSchema := &schema.TableSchema{Name: reducer.Name + "_result", Columns: columns}
	row, err := bsatn.DecodeProductValueFromBytes(result, tableSchema)
	if err != nil {
		return DecodedReducerResult{}, fmt.Errorf("decode reducer %q result: %w", reducer.Name, err)
	}
	return DecodedReducerResult{
		Name:    reducer.Name,
		Columns: columns,
		Row:     row,
	}, nil
}

// DecodeReducerResultJSONRow decodes reducer return BSATN to a JSON-ready row.
func DecodeReducerResultJSONRow(contract shunter.ModuleContract, name string, result []byte) (JSONRow, error) {
	decoded, err := DecodeReducerResult(contract, name, result)
	if err != nil {
		return nil, err
	}
	return productValueToJSONRowForColumns(decoded.Columns, decoded.Row)
}

// DecodeQueryRows decodes a declared-query RowList through its contract row schema.
func DecodeQueryRows(contract shunter.ModuleContract, name, tableName string, rowList []byte) (DecodedQueryRows, error) {
	query, ok := FindQuery(contract, name)
	if !ok {
		return DecodedQueryRows{}, fmt.Errorf("%w: query %q", ErrSurfaceNotFound, strings.TrimSpace(name))
	}
	if query.RowSchema == nil {
		return DecodedQueryRows{}, fmt.Errorf("%w: query %q", ErrResultSchemaMissing, query.Name)
	}
	if query.ResultShape != nil && query.ResultShape.Table != "" && tableName != query.ResultShape.Table {
		return DecodedQueryRows{}, fmt.Errorf("%w: query %q table %q, want %q", ErrResultTableMismatch, query.Name, tableName, query.ResultShape.Table)
	}

	columns, err := productColumnsForBSATN(*query.RowSchema)
	if err != nil {
		return DecodedQueryRows{}, err
	}
	rawRows, err := protocol.DecodeRowList(rowList)
	if err != nil {
		return DecodedQueryRows{}, fmt.Errorf("decode query %q RowList: %w", query.Name, err)
	}

	tableSchema := &schema.TableSchema{Name: tableName, Columns: columns}
	rows := make([]types.ProductValue, len(rawRows))
	for i, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, tableSchema)
		if err != nil {
			return DecodedQueryRows{}, fmt.Errorf("decode query %q row %d: %w", query.Name, i, err)
		}
		rows[i] = row
	}

	return DecodedQueryRows{
		Name:      query.Name,
		TableName: tableName,
		Columns:   columns,
		Rows:      rows,
	}, nil
}

// DecodeQueryResponse decodes a declared-query response through its contract row schema.
func DecodeQueryResponse(contract shunter.ModuleContract, name string, response protocol.OneOffQueryResponse) (DecodedQueryRows, error) {
	if len(response.Tables) != 1 {
		return DecodedQueryRows{}, fmt.Errorf("%w: query %q has %d tables, want 1", ErrResultTableCount, strings.TrimSpace(name), len(response.Tables))
	}
	table := response.Tables[0]
	return DecodeQueryRows(contract, name, table.TableName, table.Rows)
}

// DecodeQueryResponseJSONRows decodes a declared-query response to JSON-ready rows.
func DecodeQueryResponseJSONRows(contract shunter.ModuleContract, name string, response protocol.OneOffQueryResponse) ([]JSONRow, error) {
	decoded, err := DecodeQueryResponse(contract, name, response)
	if err != nil {
		return nil, err
	}
	return DecodedQueryRowsToJSONRows(decoded)
}

// DecodeQueryResponseJSONResult decodes a declared-query response to JSON-ready rows with metadata.
func DecodeQueryResponseJSONResult(contract shunter.ModuleContract, name string, response protocol.OneOffQueryResponse) (JSONQueryRows, error) {
	decoded, err := DecodeQueryResponse(contract, name, response)
	if err != nil {
		return JSONQueryRows{}, err
	}
	return DecodedQueryRowsToJSONResult(decoded)
}

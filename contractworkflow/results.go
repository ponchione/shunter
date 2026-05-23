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

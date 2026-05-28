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

// DecodeSQLQueryResponseJSONResult decodes a raw SQL one-off response through
// the result shape compiled from contract schema metadata.
func DecodeSQLQueryResponseJSONResult(contract shunter.ModuleContract, sqlText string, response protocol.OneOffQueryResponse) (JSONQueryRows, error) {
	if len(response.Tables) != 1 {
		return JSONQueryRows{}, fmt.Errorf("%w: SQL query has %d tables, want 1", ErrResultTableCount, len(response.Tables))
	}
	lookup, err := newContractSchemaLookup(contract.Schema)
	if err != nil {
		return JSONQueryRows{}, err
	}
	var caller types.Identity
	compiled, err := protocol.CompileSQLQueryString(sqlText, lookup, &caller, protocol.SQLQueryValidationOptions{
		AllowLimit:      true,
		AllowProjection: true,
		AllowOrderBy:    true,
		AllowOffset:     true,
	})
	if err != nil {
		return JSONQueryRows{}, err
	}
	table := response.Tables[0]
	if compiled.TableName() != "" && table.TableName != compiled.TableName() {
		return JSONQueryRows{}, fmt.Errorf("%w: SQL query table %q, want %q", ErrResultTableMismatch, table.TableName, compiled.TableName())
	}
	columns := compiled.ResultColumns(lookup)
	rawRows, err := protocol.DecodeRowList(table.Rows)
	if err != nil {
		return JSONQueryRows{}, fmt.Errorf("decode SQL query RowList: %w", err)
	}

	tableSchema := &schema.TableSchema{Name: table.TableName, Columns: columns}
	rows := make([]types.ProductValue, len(rawRows))
	for i, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, tableSchema)
		if err != nil {
			return JSONQueryRows{}, fmt.Errorf("decode SQL query row %d: %w", i, err)
		}
		rows[i] = row
	}
	return DecodedQueryRowsToJSONResult(DecodedQueryRows{
		Name:      strings.TrimSpace(sqlText),
		TableName: table.TableName,
		Columns:   columns,
		Rows:      rows,
	})
}

type contractSchemaLookup struct {
	tablesByID   map[schema.TableID]schema.TableSchema
	tablesByName map[string]schema.TableSchema
	indexByCol   map[contractTableColumn]schema.IndexID
}

type contractTableColumn struct {
	table schema.TableID
	col   types.ColID
}

func newContractSchemaLookup(export schema.SchemaExport) (*contractSchemaLookup, error) {
	lookup := &contractSchemaLookup{
		tablesByID:   make(map[schema.TableID]schema.TableSchema, len(export.Tables)),
		tablesByName: make(map[string]schema.TableSchema, len(export.Tables)),
		indexByCol:   make(map[contractTableColumn]schema.IndexID),
	}
	for _, table := range export.Tables {
		ts, err := tableSchemaFromExport(table)
		if err != nil {
			return nil, err
		}
		lookup.tablesByID[ts.ID] = ts
		lookup.tablesByName[ts.Name] = ts
		for _, index := range ts.Indexes {
			if len(index.Columns) != 1 {
				continue
			}
			key := contractTableColumn{table: ts.ID, col: types.ColID(index.Columns[0])}
			if _, exists := lookup.indexByCol[key]; !exists {
				lookup.indexByCol[key] = index.ID
			}
		}
	}
	return lookup, nil
}

func tableSchemaFromExport(table schema.TableExport) (schema.TableSchema, error) {
	out := schema.TableSchema{
		ID:         table.ID,
		Name:       table.Name,
		IsEvent:    table.IsEvent,
		ReadPolicy: table.ReadPolicy,
		Columns:    make([]schema.ColumnSchema, len(table.Columns)),
		Indexes:    make([]schema.IndexSchema, len(table.Indexes)),
	}
	if table.SDK != nil {
		out.SDK = *table.SDK
	}
	for i, column := range table.Columns {
		kind, ok := schema.ParseValueKindExportString(column.Type)
		if !ok {
			return schema.TableSchema{}, fmt.Errorf("%w: table %q column %q has type %q", ErrUnsupportedArgumentType, table.Name, column.Name, column.Type)
		}
		index := column.Index
		if index == 0 && i != 0 {
			index = i
		}
		out.Columns[i] = schema.ColumnSchema{
			Index:         index,
			Name:          column.Name,
			Type:          kind,
			Nullable:      column.Nullable,
			AutoIncrement: column.AutoIncrement,
		}
	}
	for i, index := range table.Indexes {
		ordinals := append([]int(nil), index.ColumnOrdinals...)
		if len(ordinals) == 0 {
			ordinals = make([]int, 0, len(index.Columns))
			for _, name := range index.Columns {
				ordinal := -1
				for colIdx, column := range out.Columns {
					if column.Name == name {
						ordinal = colIdx
						break
					}
				}
				if ordinal >= 0 {
					ordinals = append(ordinals, ordinal)
				}
			}
		}
		out.Indexes[i] = schema.NewIndexSchema(index.ID, index.Name, ordinals, index.Unique, index.Primary)
	}
	return out, nil
}

func (l *contractSchemaLookup) Table(id schema.TableID) (*schema.TableSchema, bool) {
	table, ok := l.tablesByID[id]
	if !ok {
		return nil, false
	}
	return cloneContractTableSchema(table), true
}

func (l *contractSchemaLookup) TableByName(name string) (schema.TableID, *schema.TableSchema, bool) {
	table, ok := l.tablesByName[name]
	if !ok {
		return 0, nil, false
	}
	clone := cloneContractTableSchema(table)
	return clone.ID, clone, true
}

func (l *contractSchemaLookup) TableExists(table schema.TableID) bool {
	_, ok := l.tablesByID[table]
	return ok
}

func (l *contractSchemaLookup) TableName(table schema.TableID) string {
	if table, ok := l.tablesByID[table]; ok {
		return table.Name
	}
	return ""
}

func (l *contractSchemaLookup) ColumnExists(table schema.TableID, col types.ColID) bool {
	ts, ok := l.tablesByID[table]
	return ok && int(col) >= 0 && int(col) < len(ts.Columns)
}

func (l *contractSchemaLookup) ColumnType(table schema.TableID, col types.ColID) schema.ValueKind {
	if ts, ok := l.tablesByID[table]; ok && int(col) >= 0 && int(col) < len(ts.Columns) {
		return ts.Columns[col].Type
	}
	return 0
}

func (l *contractSchemaLookup) HasIndex(table schema.TableID, col types.ColID) bool {
	_, ok := l.IndexIDForColumn(table, col)
	return ok
}

func (l *contractSchemaLookup) ColumnCount(table schema.TableID) int {
	if ts, ok := l.tablesByID[table]; ok {
		return len(ts.Columns)
	}
	return 0
}

func (l *contractSchemaLookup) IndexIDForColumn(table schema.TableID, col types.ColID) (schema.IndexID, bool) {
	index, ok := l.indexByCol[contractTableColumn{table: table, col: col}]
	return index, ok
}

func cloneContractTableSchema(table schema.TableSchema) *schema.TableSchema {
	clone := table
	clone.Columns = append([]schema.ColumnSchema(nil), table.Columns...)
	clone.Indexes = append([]schema.IndexSchema(nil), table.Indexes...)
	for i := range clone.Indexes {
		clone.Indexes[i].Columns = append([]int(nil), clone.Indexes[i].Columns...)
	}
	return &clone
}

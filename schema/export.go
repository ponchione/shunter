package schema

// SchemaExport is the JSON-friendly engine schema surface consumed by codegen
// and external tooling.
type SchemaExport struct {
	Version  uint32          `json:"version"`
	Tables   []TableExport   `json:"tables"`
	Reducers []ReducerExport `json:"reducers"`
}

// ProductSchemaExport is the exported shape of one BSATN product row.
type ProductSchemaExport struct {
	Columns []ProductColumnExport `json:"columns"`
}

// ProductColumnExport is one exported product-schema column.
type ProductColumnExport struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable,omitempty"`
}

// TableExport is the exported shape of one table.
type TableExport struct {
	ID         TableID        `json:"id"`
	Name       string         `json:"name"`
	Columns    []ColumnExport `json:"columns"`
	Indexes    []IndexExport  `json:"indexes"`
	ReadPolicy ReadPolicy     `json:"read_policy"`
}

// ColumnExport is the exported shape of one column.
type ColumnExport struct {
	Index         int    `json:"index"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Nullable      bool   `json:"nullable,omitempty"`
	AutoIncrement bool   `json:"auto_increment"`
}

// IndexExport is the exported shape of one index.
type IndexExport struct {
	ID             IndexID  `json:"id"`
	Name           string   `json:"name"`
	Columns        []string `json:"columns"`
	ColumnOrdinals []int    `json:"column_ordinals"`
	Unique         bool     `json:"unique"`
	Primary        bool     `json:"primary"`
}

// ReducerExport is the exported shape of one reducer.
type ReducerExport struct {
	Name      string               `json:"name"`
	Lifecycle bool                 `json:"lifecycle"`
	Args      *ProductSchemaExport `json:"args,omitempty"`
	Result    *ProductSchemaExport `json:"result,omitempty"`
}

// ExportSchema traverses the immutable registry and produces a detached value
// snapshot suitable for JSON serialization and code generation.
func (e *Engine) ExportSchema() *SchemaExport {
	if e == nil || e.registry == nil {
		return &SchemaExport{}
	}
	out := &SchemaExport{Version: e.registry.Version()}
	for _, tableID := range e.registry.Tables() {
		ts, ok := e.registry.Table(tableID)
		if !ok {
			continue
		}
		te := TableExport{ID: ts.ID, Name: ts.Name, ReadPolicy: normalizeReadPolicy(ts.ReadPolicy)}
		te.Columns = make([]ColumnExport, len(ts.Columns))
		for i, col := range ts.Columns {
			te.Columns[i] = ColumnExport{
				Index:         col.Index,
				Name:          col.Name,
				Type:          ValueKindExportString(col.Type),
				Nullable:      col.Nullable,
				AutoIncrement: col.AutoIncrement,
			}
		}
		te.Indexes = make([]IndexExport, len(ts.Indexes))
		for i, idx := range ts.Indexes {
			ie := IndexExport{
				ID:             idx.ID,
				Name:           idx.Name,
				ColumnOrdinals: append([]int(nil), idx.Columns...),
				Unique:         idx.Unique,
				Primary:        idx.Primary,
			}
			ie.Columns = make([]string, len(idx.Columns))
			for j, colIdx := range idx.Columns {
				if colIdx >= 0 && colIdx < len(ts.Columns) {
					ie.Columns[j] = ts.Columns[colIdx].Name
				}
			}
			te.Indexes[i] = ie
		}
		out.Tables = append(out.Tables, te)
	}
	for _, name := range e.registry.Reducers() {
		out.Reducers = append(out.Reducers, ReducerExport{Name: name, Lifecycle: false})
	}
	if e.registry.OnConnect() != nil {
		out.Reducers = append(out.Reducers, ReducerExport{Name: "OnConnect", Lifecycle: true})
	}
	if e.registry.OnDisconnect() != nil {
		out.Reducers = append(out.Reducers, ReducerExport{Name: "OnDisconnect", Lifecycle: true})
	}
	return out
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractdiff"
	"github.com/ponchione/shunter/contractworkflow"
	"github.com/ponchione/shunter/schema"
)

func runDescribe(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter describe")
	contractPath := fs.String("contract", "", "contract JSON path")
	format := fs.String("format", contractworkflow.FormatText, "output format: text or json")
	section := fs.String("section", describeSectionAll, "section to print: all, tables, reducers, queries, views, or visibility")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "contract", *contractPath); code != 0 {
		return code
	}
	if err := contractworkflow.ValidateFormat(*format); err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	if err := validateDescribeSection(*section); err != nil {
		writeCLIError(stderr, err)
		return 2
	}

	contract, err := readDescribeContract(*contractPath)
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	out, err := formatDescribeContract(contract, *format, *section)
	if err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	if _, err := stdout.Write(out); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	return 0
}

func readDescribeContract(path string) (shunter.ModuleContract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return shunter.ModuleContract{}, fmt.Errorf("read contract %q: %w", path, err)
	}
	var contract shunter.ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return shunter.ModuleContract{}, fmt.Errorf("%w: describe contract: %v", contractdiff.ErrInvalidContractJSON, err)
	}
	if err := shunter.ValidateModuleContract(contract); err != nil {
		return shunter.ModuleContract{}, fmt.Errorf("%w: describe contract: %v", contractdiff.ErrInvalidContractJSON, err)
	}
	return contract, nil
}

const (
	describeSectionAll        = "all"
	describeSectionTables     = "tables"
	describeSectionReducers   = "reducers"
	describeSectionQueries    = "queries"
	describeSectionViews      = "views"
	describeSectionVisibility = "visibility"
)

func validateDescribeSection(section string) error {
	switch normalizeDescribeSection(section) {
	case describeSectionAll, describeSectionTables, describeSectionReducers, describeSectionQueries, describeSectionViews, describeSectionVisibility:
		return nil
	default:
		return fmt.Errorf("unsupported describe section %q", section)
	}
}

func normalizeDescribeSection(section string) string {
	switch strings.ToLower(strings.TrimSpace(section)) {
	case "", describeSectionAll:
		return describeSectionAll
	case describeSectionTables:
		return describeSectionTables
	case describeSectionReducers:
		return describeSectionReducers
	case describeSectionQueries:
		return describeSectionQueries
	case describeSectionViews:
		return describeSectionViews
	case describeSectionVisibility, "visibility_filters":
		return describeSectionVisibility
	default:
		return strings.ToLower(strings.TrimSpace(section))
	}
}

func formatDescribeContract(contract shunter.ModuleContract, format, section string) ([]byte, error) {
	summary := describeContractSummary(contract)
	summary.Section = normalizeDescribeSection(section)
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", contractworkflow.FormatText:
		return []byte(summary.Text()), nil
	case contractworkflow.FormatJSON:
		summary = summary.filteredSection()
		out, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(out, '\n'), nil
	default:
		return nil, fmt.Errorf("%w %q", contractworkflow.ErrUnsupportedFormat, format)
	}
}

type describeSummary struct {
	Module            describeModule             `json:"module"`
	ContractVersion   uint32                     `json:"contract_version"`
	SchemaVersion     uint32                     `json:"schema_version"`
	Section           string                     `json:"section"`
	Counts            describeCounts             `json:"counts"`
	Tables            []describeTable            `json:"tables"`
	Reducers          []describeReducer          `json:"reducers"`
	Queries           []describeDeclaredRead     `json:"queries"`
	Views             []describeDeclaredRead     `json:"views"`
	VisibilityFilters []describeVisibilityFilter `json:"visibility_filters"`
}

type describeModule struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type describeCounts struct {
	Tables            int `json:"tables"`
	Columns           int `json:"columns"`
	Indexes           int `json:"indexes"`
	Reducers          int `json:"reducers"`
	Queries           int `json:"queries"`
	Views             int `json:"views"`
	VisibilityFilters int `json:"visibility_filters"`
}

type describeTable struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	Indexes    []string `json:"indexes"`
	ReadPolicy string   `json:"read_policy"`
}

type describeReducer struct {
	Name      string `json:"name"`
	Lifecycle bool   `json:"lifecycle,omitempty"`
	Args      int    `json:"args,omitempty"`
	Result    int    `json:"result,omitempty"`
}

type describeDeclaredRead struct {
	Name        string `json:"name"`
	SQL         string `json:"sql,omitempty"`
	Executable  bool   `json:"executable"`
	Params      int    `json:"params,omitempty"`
	RowColumns  int    `json:"row_columns,omitempty"`
	ResultKind  string `json:"result_kind,omitempty"`
	ResultTable string `json:"result_table,omitempty"`
}

type describeVisibilityFilter struct {
	Name         string `json:"name"`
	ReturnTable  string `json:"return_table"`
	UsesIdentity bool   `json:"uses_identity,omitempty"`
}

func describeContractSummary(contract shunter.ModuleContract) describeSummary {
	out := describeSummary{
		Module: describeModule{
			Name:    contract.Module.Name,
			Version: contract.Module.Version,
		},
		ContractVersion:   contract.ContractVersion,
		SchemaVersion:     contract.Schema.Version,
		Tables:            make([]describeTable, 0, len(contract.Schema.Tables)),
		Reducers:          make([]describeReducer, 0, len(contract.Schema.Reducers)),
		Queries:           make([]describeDeclaredRead, 0, len(contract.Queries)),
		Views:             make([]describeDeclaredRead, 0, len(contract.Views)),
		VisibilityFilters: make([]describeVisibilityFilter, 0, len(contract.VisibilityFilters)),
	}
	for _, table := range contract.Schema.Tables {
		out.Counts.Columns += len(table.Columns)
		out.Counts.Indexes += len(table.Indexes)
		out.Tables = append(out.Tables, describeTable{
			Name:       table.Name,
			Columns:    describeColumns(table.Columns),
			Indexes:    describeIndexes(table.Indexes),
			ReadPolicy: table.ReadPolicy.Access.String(),
		})
	}
	for _, reducer := range contract.Schema.Reducers {
		out.Reducers = append(out.Reducers, describeReducer{
			Name:      reducer.Name,
			Lifecycle: reducer.Lifecycle,
			Args:      productColumnCount(reducer.Args),
			Result:    productColumnCount(reducer.Result),
		})
	}
	for _, query := range contract.Queries {
		out.Queries = append(out.Queries, describeRead(query.Name, query.SQL, query.Parameters, query.RowSchema, query.ResultShape))
	}
	for _, view := range contract.Views {
		out.Views = append(out.Views, describeRead(view.Name, view.SQL, view.Parameters, view.RowSchema, view.ResultShape))
	}
	for _, filter := range contract.VisibilityFilters {
		out.VisibilityFilters = append(out.VisibilityFilters, describeVisibilityFilter{
			Name:         filter.Name,
			ReturnTable:  filter.ReturnTable,
			UsesIdentity: filter.UsesCallerIdentity,
		})
	}
	out.Counts.Tables = len(out.Tables)
	out.Counts.Reducers = len(out.Reducers)
	out.Counts.Queries = len(out.Queries)
	out.Counts.Views = len(out.Views)
	out.Counts.VisibilityFilters = len(out.VisibilityFilters)
	return out
}

func describeColumns(columns []schema.ColumnExport) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = column.Name + ":" + column.Type
	}
	return out
}

func describeIndexes(indexes []schema.IndexExport) []string {
	out := make([]string, len(indexes))
	for i, index := range indexes {
		out[i] = index.Name
	}
	return out
}

func describeRead(name, sql string, params, rows *shunter.ProductSchema, shape *shunter.ReadResultShape) describeDeclaredRead {
	out := describeDeclaredRead{
		Name:       name,
		SQL:        sql,
		Executable: strings.TrimSpace(sql) != "",
		Params:     productColumnCount(params),
		RowColumns: productColumnCount(rows),
	}
	if shape != nil {
		out.ResultKind = shape.Kind
		out.ResultTable = shape.Table
	}
	return out
}

func productColumnCount(product *shunter.ProductSchema) int {
	if product == nil {
		return 0
	}
	return len(product.Columns)
}

func (s describeSummary) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Module: %s", s.Module.Name)
	if s.Module.Version != "" {
		fmt.Fprintf(&b, " %s", s.Module.Version)
	}
	fmt.Fprintf(&b, "\nContract version: %d\nSchema version: %d\n", s.ContractVersion, s.SchemaVersion)
	if s.Section != "" && s.Section != describeSectionAll {
		fmt.Fprintf(&b, "Section: %s\n", s.Section)
	}
	if s.shouldWriteSection(describeSectionTables) {
		writeDescribeTextSection(&b, "Tables", len(s.Tables), func() {
			for _, table := range s.Tables {
				fmt.Fprintf(&b, "  - %s: %d columns, %d indexes, read %s\n", table.Name, len(table.Columns), len(table.Indexes), table.ReadPolicy)
			}
		})
	}
	if s.shouldWriteSection(describeSectionReducers) {
		writeDescribeTextSection(&b, "Reducers", len(s.Reducers), func() {
			for _, reducer := range s.Reducers {
				fmt.Fprintf(&b, "  - %s", reducer.Name)
				if reducer.Lifecycle {
					b.WriteString(" lifecycle")
				}
				if reducer.Args > 0 {
					fmt.Fprintf(&b, ", args %d", reducer.Args)
				}
				if reducer.Result > 0 {
					fmt.Fprintf(&b, ", result %d", reducer.Result)
				}
				b.WriteByte('\n')
			}
		})
	}
	if s.shouldWriteSection(describeSectionQueries) {
		writeDescribeTextReads(&b, "Queries", s.Queries)
	}
	if s.shouldWriteSection(describeSectionViews) {
		writeDescribeTextReads(&b, "Views", s.Views)
	}
	if s.shouldWriteSection(describeSectionVisibility) {
		writeDescribeTextSection(&b, "Visibility filters", len(s.VisibilityFilters), func() {
			for _, filter := range s.VisibilityFilters {
				fmt.Fprintf(&b, "  - %s: returns %s", filter.Name, filter.ReturnTable)
				if filter.UsesIdentity {
					b.WriteString(", uses caller identity")
				}
				b.WriteByte('\n')
			}
		})
	}
	return b.String()
}

func (s describeSummary) shouldWriteSection(section string) bool {
	return s.Section == "" || s.Section == describeSectionAll || s.Section == section
}

func (s describeSummary) filteredSection() describeSummary {
	switch s.Section {
	case "", describeSectionAll:
		return s
	case describeSectionTables:
		s.Reducers = nil
		s.Queries = nil
		s.Views = nil
		s.VisibilityFilters = nil
	case describeSectionReducers:
		s.Tables = nil
		s.Queries = nil
		s.Views = nil
		s.VisibilityFilters = nil
	case describeSectionQueries:
		s.Tables = nil
		s.Reducers = nil
		s.Views = nil
		s.VisibilityFilters = nil
	case describeSectionViews:
		s.Tables = nil
		s.Reducers = nil
		s.Queries = nil
		s.VisibilityFilters = nil
	case describeSectionVisibility:
		s.Tables = nil
		s.Reducers = nil
		s.Queries = nil
		s.Views = nil
	}
	return s
}

func writeDescribeTextReads(b *strings.Builder, label string, reads []describeDeclaredRead) {
	writeDescribeTextSection(b, label, len(reads), func() {
		for _, read := range reads {
			fmt.Fprintf(b, "  - %s", read.Name)
			if read.Executable {
				b.WriteString(" executable")
			} else {
				b.WriteString(" metadata-only")
			}
			if read.ResultKind != "" {
				fmt.Fprintf(b, ", %s", read.ResultKind)
			}
			if read.ResultTable != "" {
				fmt.Fprintf(b, " %s", read.ResultTable)
			}
			if read.Params > 0 {
				fmt.Fprintf(b, ", params %d", read.Params)
			}
			if read.RowColumns > 0 {
				fmt.Fprintf(b, ", row columns %d", read.RowColumns)
			}
			b.WriteByte('\n')
		}
	})
}

func writeDescribeTextSection(b *strings.Builder, label string, count int, writeRows func()) {
	fmt.Fprintf(b, "%s (%d):\n", label, count)
	writeRows()
}

package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractworkflow"
	"github.com/ponchione/shunter/schema"
)

func runDescribe(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter describe")
	contractPath := fs.String("contract", "", "contract JSON path")
	urlValue := fs.String("url", "", "running Shunter app URL")
	timeout := fs.Duration("timeout", defaultRunningAppTimeout, "bounded running-app diagnostics timeout")
	format := fs.String("format", contractworkflow.FormatText, "output format: text or json")
	section := fs.String("section", describeSectionAll, "section to print: all, tables, reducers, procedures, queries, views, or visibility")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
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
	contract := strings.TrimSpace(*contractPath)
	target := strings.TrimSpace(*urlValue)
	if (contract == "") == (target == "") {
		writeCLIErrorf(stderr, "provide exactly one of --contract or --url\n")
		return 2
	}
	if target != "" {
		if normalizeDescribeSection(*section) != describeSectionAll {
			writeCLIErrorf(stderr, "--section is only supported with --contract\n")
			return 2
		}
		if *timeout <= 0 {
			writeCLIErrorf(stderr, "--timeout must be positive\n")
			return 2
		}
		return runDescribeURL(stdout, stderr, target, *timeout, *format)
	}

	contractData, err := readDescribeContract(contract)
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	out, err := formatDescribeContract(contractData, *format, *section)
	if err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	return writeCLIOutput(stdout, stderr, out)
}

func runDescribeURL(stdout, stderr io.Writer, rawURL string, timeout time.Duration, format string) int {
	target, err := normalizeRunningAppDiagnosticsURL(rawURL, "/debug/shunter/runtime")
	if err != nil {
		writeRunningAppUsageError(stderr, format, runningAppError{
			Command:   "describe",
			TargetURL: rawURL,
			Code:      "invalid_url",
			Message:   err.Error(),
		})
		return 2
	}
	var description shunter.RuntimeDescription
	if err := getRunningAppDiagnosticsJSON(target, timeout, diagnosticsSuccessStatus, &description); err != nil {
		writeRunningAppRuntimeError(stderr, format, runningAppError{
			Command:   "describe",
			TargetURL: target,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 1
	}
	out, err := formatRunningDescribe(description, target, format)
	if err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	return writeCLIOutput(stdout, stderr, out)
}

func readDescribeContract(path string) (shunter.ModuleContract, error) {
	return readContractFile(path, "describe contract")
}

func readContractFile(path, context string) (shunter.ModuleContract, error) {
	return contractworkflow.LoadContractFile(path, context)
}

const (
	describeSectionAll        = "all"
	describeSectionTables     = "tables"
	describeSectionReducers   = "reducers"
	describeSectionProcedures = "procedures"
	describeSectionQueries    = "queries"
	describeSectionViews      = "views"
	describeSectionVisibility = "visibility"
)

func validateDescribeSection(section string) error {
	switch normalizeDescribeSection(section) {
	case describeSectionAll, describeSectionTables, describeSectionReducers, describeSectionProcedures, describeSectionQueries, describeSectionViews, describeSectionVisibility:
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
	case describeSectionProcedures:
		return describeSectionProcedures
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
	return formatTextOrJSON(format, summary.Text, summary.filteredSection())
}

type describeSummary struct {
	Module            describeModule             `json:"module"`
	ContractVersion   uint32                     `json:"contract_version"`
	SchemaVersion     uint32                     `json:"schema_version"`
	Section           string                     `json:"section"`
	Counts            describeCounts             `json:"counts"`
	Tables            []describeTable            `json:"tables"`
	Reducers          []describeReducer          `json:"reducers"`
	Procedures        []describeProcedure        `json:"procedures"`
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
	Procedures        int `json:"procedures"`
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

type describeProcedure struct {
	Name   string `json:"name"`
	Args   int    `json:"args,omitempty"`
	Result int    `json:"result,omitempty"`
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
		Procedures:        make([]describeProcedure, 0, len(contract.Procedures)),
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
	for _, procedure := range contract.Procedures {
		out.Procedures = append(out.Procedures, describeProcedure{
			Name:   procedure.Name,
			Args:   productColumnCount(procedure.Args),
			Result: productColumnCount(procedure.Result),
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
	out.Counts.Procedures = len(out.Procedures)
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

func writeContractSummaryText(b *strings.Builder, describe describeSummary) {
	fmt.Fprintf(b, "Module: %s", describe.Module.Name)
	if describe.Module.Version != "" {
		fmt.Fprintf(b, " %s", describe.Module.Version)
	}
	fmt.Fprintf(b, "\nContract version: %d\nSchema version: %d\n", describe.ContractVersion, describe.SchemaVersion)
	fmt.Fprintf(
		b,
		"Counts: %d tables, %d columns, %d indexes, %d reducers, %d procedures, %d queries, %d views, %d visibility filters\n",
		describe.Counts.Tables,
		describe.Counts.Columns,
		describe.Counts.Indexes,
		describe.Counts.Reducers,
		describe.Counts.Procedures,
		describe.Counts.Queries,
		describe.Counts.Views,
		describe.Counts.VisibilityFilters,
	)
}

type runningDescribeReport struct {
	Status               string                     `json:"status"`
	Scope                string                     `json:"scope"`
	Command              string                     `json:"command"`
	TargetURL            string                     `json:"target_url"`
	RunningServerChecked bool                       `json:"running_server_checked"`
	Runtime              shunter.RuntimeDescription `json:"runtime"`
}

func formatRunningDescribe(description shunter.RuntimeDescription, target, format string) ([]byte, error) {
	status := shunter.ClassifyRuntimeHealth(description.Health)
	report := runningDescribeReport{
		Status:               string(status),
		Scope:                "running_app",
		Command:              "describe",
		TargetURL:            target,
		RunningServerChecked: true,
		Runtime:              description,
	}
	return formatTextOrJSON(format, report.Text, report)
}

func (r runningDescribeReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Status: %s\n", r.Status)
	fmt.Fprintf(&b, "Scope: %s\n", r.Scope)
	fmt.Fprintf(&b, "Command: %s\n", r.Command)
	fmt.Fprintf(&b, "Target: %s\n", r.TargetURL)
	fmt.Fprintf(&b, "Running server checked: %t\n", r.RunningServerChecked)
	fmt.Fprintf(&b, "Module: %s", r.Runtime.Module.Name)
	if r.Runtime.Module.Version != "" {
		fmt.Fprintf(&b, " %s", r.Runtime.Module.Version)
	}
	fmt.Fprintf(&b, "\nRuntime state: %s\n", r.Runtime.Health.State)
	fmt.Fprintf(&b, "Ready: %t\n", r.Runtime.Health.Ready)
	fmt.Fprintf(&b, "Degraded: %t\n", r.Runtime.Health.Degraded)
	fmt.Fprintf(&b, "Queries: %d\n", len(r.Runtime.Module.Queries))
	fmt.Fprintf(&b, "Views: %d\n", len(r.Runtime.Module.Views))
	fmt.Fprintf(&b, "Visibility filters: %d\n", len(r.Runtime.Module.VisibilityFilters))
	return b.String()
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
	if s.shouldWriteSection(describeSectionProcedures) {
		writeDescribeTextSection(&b, "Procedures", len(s.Procedures), func() {
			for _, procedure := range s.Procedures {
				fmt.Fprintf(&b, "  - %s", procedure.Name)
				if procedure.Args > 0 {
					fmt.Fprintf(&b, ", args %d", procedure.Args)
				}
				if procedure.Result > 0 {
					fmt.Fprintf(&b, ", result %d", procedure.Result)
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
		s.Procedures = nil
		s.Queries = nil
		s.Views = nil
		s.VisibilityFilters = nil
	case describeSectionReducers:
		s.Tables = nil
		s.Procedures = nil
		s.Queries = nil
		s.Views = nil
		s.VisibilityFilters = nil
	case describeSectionProcedures:
		s.Tables = nil
		s.Reducers = nil
		s.Queries = nil
		s.Views = nil
		s.VisibilityFilters = nil
	case describeSectionQueries:
		s.Tables = nil
		s.Reducers = nil
		s.Procedures = nil
		s.Views = nil
		s.VisibilityFilters = nil
	case describeSectionViews:
		s.Tables = nil
		s.Reducers = nil
		s.Procedures = nil
		s.Queries = nil
		s.VisibilityFilters = nil
	case describeSectionVisibility:
		s.Tables = nil
		s.Reducers = nil
		s.Procedures = nil
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

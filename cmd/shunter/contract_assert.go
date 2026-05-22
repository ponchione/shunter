package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractworkflow"
)

func runContractAssert(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter contract assert")
	contractPath := fs.String("contract", "", "contract JSON path")
	format := fs.String("format", contractworkflow.FormatText, "output format: text or json")
	module := fs.String("module", "", "expected module name")
	schemaVersion := fs.Int("schema-version", -1, "expected schema version")
	tables := fs.Int("tables", -1, "expected table count")
	columns := fs.Int("columns", -1, "expected column count")
	indexes := fs.Int("indexes", -1, "expected index count")
	reducers := fs.Int("reducers", -1, "expected reducer count")
	queries := fs.Int("queries", -1, "expected query count")
	views := fs.Int("views", -1, "expected view count")
	visibilityFilters := fs.Int("visibility-filters", -1, "expected visibility filter count")
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
	assertions := contractAssertions{
		Module:            strings.TrimSpace(*module),
		SchemaVersion:     *schemaVersion,
		Tables:            *tables,
		Columns:           *columns,
		Indexes:           *indexes,
		Reducers:          *reducers,
		Queries:           *queries,
		Views:             *views,
		VisibilityFilters: *visibilityFilters,
	}
	if err := assertions.validate(); err != nil {
		writeCLIError(stderr, err)
		return 2
	}

	contract, err := readContractFile(*contractPath, "assert contract")
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	report := buildContractAssertReport(contract, assertions)
	out, err := formatContractAssertReport(report, *format)
	if err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	if _, err := stdout.Write(out); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	if report.Status != contractAssertStatusPassed {
		return 1
	}
	return 0
}

const (
	contractAssertStatusPassed = "passed"
	contractAssertStatusFailed = "failed"
)

type contractAssertions struct {
	Module            string
	SchemaVersion     int
	Tables            int
	Columns           int
	Indexes           int
	Reducers          int
	Queries           int
	Views             int
	VisibilityFilters int
}

func (a contractAssertions) validate() error {
	for _, assertion := range a.countAssertions() {
		if assertion.expected < -1 {
			return fmt.Errorf("--%s must be >= 0", assertion.name)
		}
	}
	return nil
}

func (a contractAssertions) countAssertions() []contractAssertExpectedCount {
	return []contractAssertExpectedCount{
		{name: "schema-version", expected: a.SchemaVersion},
		{name: "tables", expected: a.Tables},
		{name: "columns", expected: a.Columns},
		{name: "indexes", expected: a.Indexes},
		{name: "reducers", expected: a.Reducers},
		{name: "queries", expected: a.Queries},
		{name: "views", expected: a.Views},
		{name: "visibility-filters", expected: a.VisibilityFilters},
	}
}

type contractAssertExpectedCount struct {
	name     string
	expected int
}

type contractAssertReport struct {
	Status     string                `json:"status"`
	Scope      string                `json:"scope"`
	Message    string                `json:"message"`
	Module     describeModule        `json:"module"`
	Counts     describeCounts        `json:"counts"`
	Assertions []contractAssertCheck `json:"assertions"`
	Failures   []contractAssertCheck `json:"failures,omitempty"`
}

type contractAssertCheck struct {
	Name     string `json:"name"`
	Expected any    `json:"expected"`
	Actual   any    `json:"actual"`
	Passed   bool   `json:"passed"`
}

func buildContractAssertReport(contract shunter.ModuleContract, assertions contractAssertions) contractAssertReport {
	summary := describeContractSummary(contract)
	report := contractAssertReport{
		Status: contractAssertStatusPassed,
		Scope:  "contract",
		Module: summary.Module,
		Counts: summary.Counts,
	}
	if assertions.Module != "" {
		report.addStringAssertion("module", assertions.Module, summary.Module.Name)
	}
	report.addCountAssertions([]contractAssertCountCheck{
		{Name: "schema-version", Expected: assertions.SchemaVersion, Actual: int(summary.SchemaVersion)},
		{Name: "tables", Expected: assertions.Tables, Actual: summary.Counts.Tables},
		{Name: "columns", Expected: assertions.Columns, Actual: summary.Counts.Columns},
		{Name: "indexes", Expected: assertions.Indexes, Actual: summary.Counts.Indexes},
		{Name: "reducers", Expected: assertions.Reducers, Actual: summary.Counts.Reducers},
		{Name: "queries", Expected: assertions.Queries, Actual: summary.Counts.Queries},
		{Name: "views", Expected: assertions.Views, Actual: summary.Counts.Views},
		{Name: "visibility-filters", Expected: assertions.VisibilityFilters, Actual: summary.Counts.VisibilityFilters},
	})
	if len(report.Failures) > 0 {
		report.Status = contractAssertStatusFailed
		report.Message = fmt.Sprintf("%d contract assertion(s) failed", len(report.Failures))
		return report
	}
	report.Message = fmt.Sprintf("%d contract assertion(s) passed", len(report.Assertions))
	return report
}

func (r *contractAssertReport) addStringAssertion(name, expected, actual string) {
	check := contractAssertCheck{
		Name:     name,
		Expected: expected,
		Actual:   actual,
		Passed:   expected == actual,
	}
	r.Assertions = append(r.Assertions, check)
	if !check.Passed {
		r.Failures = append(r.Failures, check)
	}
}

type contractAssertCountCheck struct {
	Name     string
	Expected int
	Actual   int
}

func (r *contractAssertReport) addCountAssertions(checks []contractAssertCountCheck) {
	for _, check := range checks {
		if check.Expected < 0 {
			continue
		}
		assertion := contractAssertCheck{
			Name:     check.Name,
			Expected: check.Expected,
			Actual:   check.Actual,
			Passed:   check.Expected == check.Actual,
		}
		r.Assertions = append(r.Assertions, assertion)
		if !assertion.Passed {
			r.Failures = append(r.Failures, assertion)
		}
	}
}

func formatContractAssertReport(report contractAssertReport, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", contractworkflow.FormatText:
		return []byte(report.Text()), nil
	case contractworkflow.FormatJSON:
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(out, '\n'), nil
	default:
		return nil, fmt.Errorf("%w %q", contractworkflow.ErrUnsupportedFormat, format)
	}
}

func (r contractAssertReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Status: %s\n", r.Status)
	fmt.Fprintf(&b, "Scope: %s\n", r.Scope)
	fmt.Fprintf(&b, "Message: %s\n", r.Message)
	fmt.Fprintf(&b, "Module: %s", r.Module.Name)
	if r.Module.Version != "" {
		fmt.Fprintf(&b, " %s", r.Module.Version)
	}
	fmt.Fprintf(
		&b,
		"\nCounts: %d tables, %d columns, %d indexes, %d reducers, %d queries, %d views, %d visibility filters\n",
		r.Counts.Tables,
		r.Counts.Columns,
		r.Counts.Indexes,
		r.Counts.Reducers,
		r.Counts.Queries,
		r.Counts.Views,
		r.Counts.VisibilityFilters,
	)
	if len(r.Assertions) == 0 {
		b.WriteString("Assertions: none\n")
		return b.String()
	}
	b.WriteString("Assertions:\n")
	for _, assertion := range r.Assertions {
		marker := "ok"
		if !assertion.Passed {
			marker = "fail"
		}
		fmt.Fprintf(&b, "  - %s: %s", assertion.Name, marker)
		fmt.Fprintf(&b, " expected %v actual %v", assertion.Expected, assertion.Actual)
		b.WriteByte('\n')
	}
	return b.String()
}

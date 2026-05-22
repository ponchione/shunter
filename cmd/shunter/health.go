package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractworkflow"
)

func runHealth(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter health")
	contractPath := fs.String("contract", "", "contract JSON path")
	format := fs.String("format", contractworkflow.FormatText, "output format: text or json")
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

	contract, err := readContractFile(*contractPath, "health contract")
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	out, err := formatHealthContract(contract, *format)
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

func formatHealthContract(contract shunter.ModuleContract, format string) ([]byte, error) {
	describe := describeContractSummary(contract)
	describe.Section = describeSectionAll
	report := healthContractReport{
		Status:               "ok",
		Scope:                "contract",
		RunningServerChecked: false,
		Message:              "local contract artifact is valid; no running server was checked",
		Describe:             describe,
	}
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

type healthContractReport struct {
	Status               string          `json:"status"`
	Scope                string          `json:"scope"`
	RunningServerChecked bool            `json:"running_server_checked"`
	Message              string          `json:"message"`
	Describe             describeSummary `json:"describe"`
}

func (r healthContractReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Status: %s\n", r.Status)
	fmt.Fprintf(&b, "Scope: %s\n", r.Scope)
	fmt.Fprintf(&b, "Running server checked: %t\n", r.RunningServerChecked)
	fmt.Fprintf(&b, "Message: %s\n", r.Message)
	fmt.Fprintf(&b, "Module: %s", r.Describe.Module.Name)
	if r.Describe.Module.Version != "" {
		fmt.Fprintf(&b, " %s", r.Describe.Module.Version)
	}
	fmt.Fprintf(&b, "\nContract version: %d\nSchema version: %d\n", r.Describe.ContractVersion, r.Describe.SchemaVersion)
	fmt.Fprintf(
		&b,
		"Counts: %d tables, %d columns, %d indexes, %d reducers, %d queries, %d views, %d visibility filters\n",
		r.Describe.Counts.Tables,
		r.Describe.Counts.Columns,
		r.Describe.Counts.Indexes,
		r.Describe.Counts.Reducers,
		r.Describe.Counts.Queries,
		r.Describe.Counts.Views,
		r.Describe.Counts.VisibilityFilters,
	)
	return b.String()
}

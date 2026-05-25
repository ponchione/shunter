package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractworkflow"
)

func runContractValidate(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter contract validate")
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

	contract, err := readContractFile(*contractPath, "validate contract")
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	out, err := formatValidateContract(contract, *format)
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

func formatValidateContract(contract shunter.ModuleContract, format string) ([]byte, error) {
	describe := describeContractSummary(contract)
	describe.Section = describeSectionAll
	report := contractValidationReport{
		Status:   "valid",
		Scope:    "contract",
		Message:  "module contract JSON is valid",
		Describe: describe,
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

type contractValidationReport struct {
	Status   string          `json:"status"`
	Scope    string          `json:"scope"`
	Message  string          `json:"message"`
	Describe describeSummary `json:"describe"`
}

func (r contractValidationReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Status: %s\n", r.Status)
	fmt.Fprintf(&b, "Scope: %s\n", r.Scope)
	fmt.Fprintf(&b, "Message: %s\n", r.Message)
	writeContractSummaryText(&b, r.Describe)
	return b.String()
}

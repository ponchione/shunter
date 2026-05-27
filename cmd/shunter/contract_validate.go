package main

import (
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
	return writeCLIOutput(stdout, stderr, out)
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
	return formatTextOrJSON(format, report.Text, report)
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

package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractworkflow"
)

func runHealth(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter health")
	contractPath := fs.String("contract", "", "contract JSON path")
	urlValue := fs.String("url", "", "running Shunter app URL")
	timeout := fs.Duration("timeout", defaultRunningAppTimeout, "bounded running-app diagnostics timeout")
	format := fs.String("format", contractworkflow.FormatText, "output format: text or json")
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
	contract := strings.TrimSpace(*contractPath)
	target := strings.TrimSpace(*urlValue)
	if (contract == "") == (target == "") {
		writeCLIErrorf(stderr, "provide exactly one of --contract or --url\n")
		return 2
	}
	if target != "" {
		if *timeout <= 0 {
			writeCLIErrorf(stderr, "--timeout must be positive\n")
			return 2
		}
		return runHealthURL(stdout, stderr, target, *timeout, *format)
	}

	contractData, err := readContractFile(contract, "health contract")
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	out, err := formatHealthContract(contractData, *format)
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

func runHealthURL(stdout, stderr io.Writer, rawURL string, timeout time.Duration, format string) int {
	target, err := normalizeRunningAppDiagnosticsURL(rawURL, "/healthz")
	if err != nil {
		writeRunningAppUsageError(stderr, format, runningAppError{
			Command:   "health",
			TargetURL: rawURL,
			Code:      "invalid_url",
			Message:   err.Error(),
		})
		return 2
	}
	var inspection shunter.RuntimeHealthInspection
	if err := getRunningAppDiagnosticsJSON(target, timeout, healthDiagnosticsStatus, &inspection); err != nil {
		writeRunningAppRuntimeError(stderr, format, runningAppError{
			Command:   "health",
			TargetURL: target,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 1
	}
	out, err := formatRunningHealth(inspection, target, format)
	if err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	if _, err := stdout.Write(out); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	if inspection.Status == shunter.HealthStatusFailed {
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
	return formatTextOrJSON(format, report.Text, report)
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
	writeContractSummaryText(&b, r.Describe)
	return b.String()
}

type runningHealthReport struct {
	Status               string                          `json:"status"`
	Scope                string                          `json:"scope"`
	Command              string                          `json:"command"`
	TargetURL            string                          `json:"target_url"`
	RunningServerChecked bool                            `json:"running_server_checked"`
	Runtime              shunter.RuntimeHealthInspection `json:"runtime"`
}

func formatRunningHealth(inspection shunter.RuntimeHealthInspection, target, format string) ([]byte, error) {
	report := runningHealthReport{
		Status:               string(inspection.Status),
		Scope:                "running_app",
		Command:              "health",
		TargetURL:            target,
		RunningServerChecked: true,
		Runtime:              inspection,
	}
	return formatTextOrJSON(format, report.Text, report)
}

func (r runningHealthReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Status: %s\n", r.Status)
	fmt.Fprintf(&b, "Scope: %s\n", r.Scope)
	fmt.Fprintf(&b, "Command: %s\n", r.Command)
	fmt.Fprintf(&b, "Target: %s\n", r.TargetURL)
	fmt.Fprintf(&b, "Running server checked: %t\n", r.RunningServerChecked)
	fmt.Fprintf(&b, "Runtime state: %s\n", r.Runtime.Runtime.State)
	fmt.Fprintf(&b, "Ready: %t\n", r.Runtime.Runtime.Ready)
	fmt.Fprintf(&b, "Degraded: %t\n", r.Runtime.Runtime.Degraded)
	fmt.Fprintf(&b, "Protocol ready: %t\n", r.Runtime.Runtime.Protocol.Ready)
	return b.String()
}

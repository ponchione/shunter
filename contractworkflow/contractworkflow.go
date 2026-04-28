package contractworkflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/codegen"
	"github.com/ponchione/shunter/contractdiff"
)

const (
	// FormatText renders human-readable workflow output.
	FormatText = "text"
	// FormatJSON renders deterministic JSON workflow output.
	FormatJSON = "json"
)

var ErrUnsupportedFormat = errors.New("unsupported contract workflow output format")

// CompareFiles diffs two canonical ModuleContract JSON files.
func CompareFiles(previousPath, currentPath string) (contractdiff.Report, error) {
	previousData, err := readRequiredFile("previous contract", previousPath)
	if err != nil {
		return contractdiff.Report{}, err
	}
	currentData, err := readRequiredFile("current contract", currentPath)
	if err != nil {
		return contractdiff.Report{}, err
	}
	return contractdiff.CompareJSON(previousData, currentData)
}

// CheckPolicyFiles runs migration/contract policy checks for two contract JSON files.
func CheckPolicyFiles(previousPath, currentPath string, opts contractdiff.PolicyOptions) (contractdiff.PolicyResult, error) {
	previousData, err := readRequiredFile("previous contract", previousPath)
	if err != nil {
		return contractdiff.PolicyResult{}, err
	}
	currentData, err := readRequiredFile("current contract", currentPath)
	if err != nil {
		return contractdiff.PolicyResult{}, err
	}

	report, err := contractdiff.CompareJSON(previousData, currentData)
	if err != nil {
		return contractdiff.PolicyResult{}, err
	}
	current, err := decodeCurrentContract(currentData)
	if err != nil {
		return contractdiff.PolicyResult{}, err
	}
	return contractdiff.CheckPolicy(report, current, opts), nil
}

// PlanFiles builds a deterministic migration plan from two contract JSON files.
func PlanFiles(previousPath, currentPath string, opts contractdiff.PlanOptions) (contractdiff.MigrationPlan, error) {
	previousData, err := readRequiredFile("previous contract", previousPath)
	if err != nil {
		return contractdiff.MigrationPlan{}, err
	}
	currentData, err := readRequiredFile("current contract", currentPath)
	if err != nil {
		return contractdiff.MigrationPlan{}, err
	}
	return contractdiff.PlanJSON(previousData, currentData, opts)
}

// GenerateFromFile generates client bindings from a canonical ModuleContract JSON file.
func GenerateFromFile(contractPath string, opts codegen.Options) ([]byte, error) {
	data, err := readRequiredFile("contract input", contractPath)
	if err != nil {
		return nil, err
	}
	out, err := codegen.GenerateFromJSON(data, opts)
	if err != nil {
		return nil, fmt.Errorf("generate bindings from %q: %w", contractPath, err)
	}
	return out, nil
}

// GenerateFile generates client bindings from contractPath and writes them to outputPath.
func GenerateFile(contractPath, outputPath string, opts codegen.Options) error {
	out, err := GenerateFromFile(contractPath, opts)
	if err != nil {
		return err
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("generated output path is required")
	}
	if err := writeFile(outputPath, out); err != nil {
		return fmt.Errorf("write generated output %q: %w", outputPath, err)
	}
	return nil
}

// FormatDiff renders a contract diff report in text or JSON form.
func FormatDiff(report contractdiff.Report, format string) ([]byte, error) {
	switch normalizedFormat(format) {
	case FormatText:
		return []byte(report.Text()), nil
	case FormatJSON:
		out := diffOutput{Changes: make([]changeOutput, 0, len(report.Changes))}
		for _, change := range report.Changes {
			out.Changes = append(out.Changes, changeOutput{
				Kind:    string(change.Kind),
				Surface: string(change.Surface),
				Name:    change.Name,
				Detail:  change.Detail,
			})
		}
		return marshalWorkflowJSON(out)
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedFormat, format)
	}
}

// FormatPolicy renders policy warnings in text or JSON form.
func FormatPolicy(result contractdiff.PolicyResult, format string) ([]byte, error) {
	switch normalizedFormat(format) {
	case FormatText:
		if len(result.Warnings) == 0 {
			return []byte("No policy warnings.\n"), nil
		}
		var b strings.Builder
		for _, warning := range result.Warnings {
			fmt.Fprintf(&b, "%s %s %s: %s\n", warning.Code, warning.Surface, warning.Name, warning.Detail)
		}
		return []byte(b.String()), nil
	case FormatJSON:
		out := policyOutput{
			Failed:   result.Failed,
			Warnings: make([]policyWarningOutput, 0, len(result.Warnings)),
		}
		for _, warning := range result.Warnings {
			out.Warnings = append(out.Warnings, policyWarningOutput{
				Code:    string(warning.Code),
				Surface: string(warning.Surface),
				Name:    warning.Name,
				Detail:  warning.Detail,
			})
		}
		return marshalWorkflowJSON(out)
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedFormat, format)
	}
}

// FormatPlan renders a migration plan in text or JSON form.
func FormatPlan(plan contractdiff.MigrationPlan, format string) ([]byte, error) {
	switch normalizedFormat(format) {
	case FormatText:
		return []byte(plan.Text()), nil
	case FormatJSON:
		return plan.MarshalCanonicalJSON()
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedFormat, format)
	}
}

type diffOutput struct {
	Changes []changeOutput `json:"changes"`
}

type changeOutput struct {
	Kind    string `json:"kind"`
	Surface string `json:"surface"`
	Name    string `json:"name"`
	Detail  string `json:"detail"`
}

type policyOutput struct {
	Failed   bool                  `json:"failed"`
	Warnings []policyWarningOutput `json:"warnings"`
}

type policyWarningOutput struct {
	Code    string `json:"code"`
	Surface string `json:"surface"`
	Name    string `json:"name"`
	Detail  string `json:"detail"`
}

func normalizedFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return FormatText
	}
	return format
}

func marshalWorkflowJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decodeCurrentContract(data []byte) (shunter.ModuleContract, error) {
	var contract shunter.ModuleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return shunter.ModuleContract{}, fmt.Errorf("%w: current contract: %v", contractdiff.ErrInvalidContractJSON, err)
	}
	return contract, nil
}

func readRequiredFile(label, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%s path is required", label)
	}
	data, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s %q: %w", label, path, err)
	}
	return data, nil
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o666)
}

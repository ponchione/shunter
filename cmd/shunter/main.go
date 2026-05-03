package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/codegen"
	"github.com/ponchione/shunter/contractdiff"
	"github.com/ponchione/shunter/contractworkflow"
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printRootHelp(stdout)
		return 0
	}
	if isVersionArg(args[0]) {
		printVersion(stdout)
		return 0
	}

	switch args[0] {
	case "version":
		printVersion(stdout)
		return 0
	case "contract":
		return runContract(stdout, stderr, args[1:])
	default:
		writeCLIErrorf(stderr, "unknown command %q\n\n", args[0])
		printRootHelp(stderr)
		return 2
	}
}

func runContract(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printContractHelp(stdout)
		return 0
	}

	switch args[0] {
	case "diff":
		return runContractDiff(stdout, stderr, args[1:])
	case "policy":
		return runContractPolicy(stdout, stderr, args[1:])
	case "plan":
		return runContractPlan(stdout, stderr, args[1:])
	case "codegen":
		return runContractCodegen(stdout, stderr, args[1:])
	default:
		writeCLIErrorf(stderr, "unknown contract command %q\n\n", args[0])
		printContractHelp(stderr)
		return 2
	}
}

func runContractDiff(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter contract diff")
	previousPath := fs.String("previous", "", "previous contract JSON path")
	currentPath := fs.String("current", "", "current contract JSON path")
	format := fs.String("format", contractworkflow.FormatText, "output format: text or json")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "previous", *previousPath); code != 0 {
		return code
	}
	if code := requirePath(stderr, "current", *currentPath); code != 0 {
		return code
	}
	if err := contractworkflow.ValidateFormat(*format); err != nil {
		writeCLIError(stderr, err)
		return 2
	}

	report, err := contractworkflow.CompareFiles(*previousPath, *currentPath)
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	out, err := contractworkflow.FormatDiff(report, *format)
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

func runContractPolicy(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter contract policy")
	previousPath := fs.String("previous", "", "previous contract JSON path")
	currentPath := fs.String("current", "", "current contract JSON path")
	strict := fs.Bool("strict", false, "exit with failure when warnings are present")
	requirePreviousVersion := fs.Bool("require-previous-version", false, "warn when module migration metadata omits previous_version")
	format := fs.String("format", contractworkflow.FormatText, "output format: text or json")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "previous", *previousPath); code != 0 {
		return code
	}
	if code := requirePath(stderr, "current", *currentPath); code != 0 {
		return code
	}
	if err := contractworkflow.ValidateFormat(*format); err != nil {
		writeCLIError(stderr, err)
		return 2
	}

	result, err := contractworkflow.CheckPolicyFiles(*previousPath, *currentPath, contractdiff.PolicyOptions{
		RequirePreviousVersion: *requirePreviousVersion,
		Strict:                 *strict,
	})
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	out, err := contractworkflow.FormatPolicy(result, *format)
	if err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	if _, err := stdout.Write(out); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	if result.Failed {
		return 1
	}
	return 0
}

func runContractPlan(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter contract plan")
	previousPath := fs.String("previous", "", "previous contract JSON path")
	currentPath := fs.String("current", "", "current contract JSON path")
	strict := fs.Bool("strict", false, "exit with failure when policy warnings are present")
	requirePreviousVersion := fs.Bool("require-previous-version", false, "warn when module migration metadata omits previous_version")
	validateContracts := fs.Bool("validate", false, "include read-only contract consistency validation warnings")
	format := fs.String("format", contractworkflow.FormatText, "output format: text or json")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "previous", *previousPath); code != 0 {
		return code
	}
	if code := requirePath(stderr, "current", *currentPath); code != 0 {
		return code
	}
	if err := contractworkflow.ValidateFormat(*format); err != nil {
		writeCLIError(stderr, err)
		return 2
	}

	plan, err := contractworkflow.PlanFiles(*previousPath, *currentPath, contractdiff.PlanOptions{
		Policy: contractdiff.PolicyOptions{
			RequirePreviousVersion: *requirePreviousVersion,
			Strict:                 *strict,
		},
		ValidateContracts: *validateContracts,
	})
	if err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	out, err := contractworkflow.FormatPlan(plan, *format)
	if err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	if _, err := stdout.Write(out); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	if plan.Summary.PolicyFailed {
		return 1
	}
	return 0
}

func runContractCodegen(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter contract codegen")
	contractPath := fs.String("contract", "", "contract JSON path")
	language := fs.String("language", codegen.LanguageTypeScript, "generator target language")
	outputPath := fs.String("out", "", "generated output path")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "contract", *contractPath); code != 0 {
		return code
	}
	if code := requirePath(stderr, "out", *outputPath); code != 0 {
		return code
	}

	if err := contractworkflow.GenerateFile(*contractPath, *outputPath, codegen.Options{Language: *language}); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	writeCLIStatusf(stdout, "wrote %s\n", *outputPath)
	return 0
}

func newFlagSet(stderr io.Writer, name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, true
		}
		return 2, true
	}
	return 0, false
}

func requireNoArgs(stderr io.Writer, fs *flag.FlagSet) int {
	if fs.NArg() == 0 {
		return 0
	}
	writeCLIErrorf(stderr, "%s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
	return 2
}

func requirePath(stderr io.Writer, name, value string) int {
	if value != "" {
		return 0
	}
	writeCLIErrorf(stderr, "--%s is required\n", name)
	return 2
}

func writeCLIError(stderr io.Writer, err error) {
	if err == nil {
		return
	}
	writeCLIErrorf(stderr, "%v\n", err)
}

// writeCLIErrorf and writeCLIStatusf are user-facing command output helpers.
// They use injected writers only; runtime diagnostics must go through
// ObservabilityConfig.Logger and runtimeObservability instead.
func writeCLIErrorf(stderr io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(stderr, format, args...)
}

func writeCLIStatusf(stdout io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(stdout, format, args...)
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func isVersionArg(arg string) bool {
	return arg == "-version" || arg == "--version"
}

func printVersion(w io.Writer) {
	info := shunter.CurrentBuildInfo()
	fmt.Fprintf(w, "shunter %s\n", info.Version)
	if info.Commit != "" {
		fmt.Fprintf(w, "commit %s\n", info.Commit)
	}
	if info.Date != "" {
		fmt.Fprintf(w, "date %s\n", info.Date)
	}
	if info.Dirty {
		fmt.Fprintln(w, "dirty true")
	}
	if info.GoVersion != "" {
		fmt.Fprintf(w, "go %s\n", info.GoVersion)
	}
}

func printRootHelp(w io.Writer) {
	fmt.Fprint(w, `Shunter contract artifact workflows.

Usage:
  shunter version
  shunter contract diff --previous old.json --current current.json [--format text|json]
  shunter contract policy --previous old.json --current current.json [--strict] [--require-previous-version] [--format text|json]
  shunter contract plan --previous old.json --current current.json [--strict] [--require-previous-version] [--validate] [--format text|json]
  shunter contract codegen --contract shunter.contract.json --language typescript --out client.ts

This generic CLI operates only on existing ModuleContract JSON files. It does
not export an app module contract; export from an app-owned binary with
Runtime.ExportContractJSON so the binary links the module. No dynamic module loading is provided.
`)
}

func printContractHelp(w io.Writer) {
	fmt.Fprint(w, `Usage:
  shunter contract diff --previous old.json --current current.json [--format text|json]
  shunter contract policy --previous old.json --current current.json [--strict] [--require-previous-version] [--format text|json]
  shunter contract plan --previous old.json --current current.json [--strict] [--require-previous-version] [--validate] [--format text|json]
  shunter contract codegen --contract shunter.contract.json --language typescript --out client.ts

Contract commands operate on canonical ModuleContract JSON files only.
Export app module contracts from an app-owned binary with Runtime.ExportContractJSON.
`)
}

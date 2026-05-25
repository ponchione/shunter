package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/examples/hosted-chat/internal/app"
)

const (
	formatText = "text"
	formatJSON = "json"
)

func main() {
	os.Exit(run(context.Background(), os.Stdout, os.Stderr, os.Args[1:]))
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printHelp(stdout)
		return 0
	}
	switch args[0] {
	case "preflight":
		return runPreflight(stdout, stderr, args[1:])
	case "migrate":
		return runMigrate(ctx, stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printHelp(stderr)
		return 2
	}
}

func runPreflight(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("hosted-chat-maintain preflight", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data-dir", "", "offline hosted-chat DataDir to inspect")
	format := fs.String("format", formatText, "output format: text or json")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	cfg := maintenanceConfig(*dataDir)
	report, err := shunter.CheckDataDirCompatibilityReport(app.Module(), cfg)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if err := writePreflightReport(stdout, report, *format); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	if !report.Compatible {
		return 1
	}
	return 0
}

func runMigrate(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("hosted-chat-maintain migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data-dir", "", "offline hosted-chat DataDir to migrate")
	format := fs.String("format", formatText, "output format: text or json")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := shunter.RunModuleDataDirMigrations(ctx, app.Module(), maintenanceConfig(*dataDir))
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if err := writeMigrationResult(stdout, result, *format); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	return 0
}

func maintenanceConfig(dataDir string) shunter.Config {
	cfg := shunter.ConfigFromEnv()
	if strings.TrimSpace(dataDir) != "" {
		cfg.DataDir = dataDir
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		cfg.DataDir = "./data/hosted-chat"
	}
	return cfg
}

func writePreflightReport(w io.Writer, report shunter.DataDirCompatibilityReport, format string) error {
	switch strings.TrimSpace(format) {
	case "", formatText:
		fmt.Fprintf(w, "status: %s\n", report.Status)
		fmt.Fprintf(w, "compatible: %t\n", report.Compatible)
		fmt.Fprintf(w, "data_dir: %s\n", report.DataDir)
		fmt.Fprintf(w, "requires_backup: %t\n", report.RequiresBackup)
		fmt.Fprintf(w, "requires_offline_hook: %t\n", report.RequiresOfflineHook)
		if report.BlockingError != "" {
			fmt.Fprintf(w, "blocking_error: %s\n", report.BlockingError)
		}
		return nil
	case formatJSON:
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", data)
		return err
	default:
		return fmt.Errorf("format must be text or json")
	}
}

func writeMigrationResult(w io.Writer, result shunter.MigrationRunResult, format string) error {
	switch strings.TrimSpace(format) {
	case "", formatText:
		fmt.Fprintf(w, "status: migrated\n")
		fmt.Fprintf(w, "data_dir: %s\n", result.DataDir)
		fmt.Fprintf(w, "recovered_tx_id: %d\n", result.RecoveredTxID)
		fmt.Fprintf(w, "durable_tx_id: %d\n", result.DurableTxID)
		fmt.Fprintf(w, "hooks: %d\n", len(result.Hooks))
		return nil
	case formatJSON:
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", data)
		return err
	default:
		return fmt.Errorf("format must be text or json")
	}
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
	fmt.Fprintf(stderr, "%s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
	return 2
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `Hosted Chat maintenance commands.

Usage:
  hosted-chat-maintain preflight [--data-dir ./data/hosted-chat] [--format text|json]
  hosted-chat-maintain migrate [--data-dir ./data/hosted-chat] [--format text|json]

These commands are app-owned: they link the hosted-chat module directly so
DataDir compatibility checks and migration hooks use the same declarations as
the hosted server.
`)
}

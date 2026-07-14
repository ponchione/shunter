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
	case "prepare-backup":
		return runPrepareBackup(ctx, stdout, stderr, args[1:])
	case "migrate":
		return runMigrate(ctx, stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printHelp(stderr)
		return 2
	}
}

type backupPreparationResult struct {
	Status        string `json:"status"`
	DataDir       string `json:"data_dir"`
	RecoveredTxID uint64 `json:"recovered_tx_id"`
	SnapshotTxID  uint64 `json:"snapshot_tx_id"`
}

func runPrepareBackup(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("hosted-chat-maintain prepare-backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data-dir", "", "offline hosted-chat DataDir to snapshot and compact")
	format := fs.String("format", formatText, "output format: text or json")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if err := validateOutputFormat(*format); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := prepareBackup(ctx, app.Module(), maintenanceConfig(*dataDir))
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if err := writeBackupPreparationResult(stdout, result, *format); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	return 0
}

func prepareBackup(ctx context.Context, mod *shunter.Module, cfg shunter.Config) (backupPreparationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return backupPreparationResult{}, err
	}
	if err := requireExistingOfflineDataDir(cfg.DataDir); err != nil {
		return backupPreparationResult{}, err
	}
	rt, err := shunter.Build(mod, cfg)
	if err != nil {
		return backupPreparationResult{}, fmt.Errorf("build maintenance runtime: %w", err)
	}
	closeWith := func(operationErr error) error {
		return errors.Join(operationErr, rt.Close())
	}
	recoveredTxID := rt.Health().Recovery.RecoveredTxID
	snapshotTxID, err := rt.CreateSnapshot()
	if err != nil {
		return backupPreparationResult{}, closeWith(fmt.Errorf("create maintenance snapshot: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return backupPreparationResult{}, closeWith(fmt.Errorf("prepare maintenance snapshot: %w", err))
	}
	if err := rt.WaitUntilDurable(ctx, snapshotTxID); err != nil {
		return backupPreparationResult{}, closeWith(fmt.Errorf("wait for snapshot horizon durability: %w", err))
	}
	if err := rt.CompactCommitLog(snapshotTxID); err != nil {
		return backupPreparationResult{}, closeWith(fmt.Errorf("compact snapshot-covered commit log: %w", err))
	}
	if err := rt.Close(); err != nil {
		return backupPreparationResult{}, fmt.Errorf("close maintenance runtime: %w", err)
	}
	return backupPreparationResult{
		Status:        "prepared",
		DataDir:       cfg.DataDir,
		RecoveredTxID: uint64(recoveredTxID),
		SnapshotTxID:  uint64(snapshotTxID),
	}, nil
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
	if err := validateOutputFormat(*format); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	cfg := maintenanceConfig(*dataDir)
	report, err := shunter.CheckDataDirCompatibilityReport(app.Module(), cfg)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if err := writePreflightReport(stdout, report, *format); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
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
	if err := validateOutputFormat(*format); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
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
		return 1
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

func requireExistingOfflineDataDir(dataDir string) error {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return fmt.Errorf("offline data dir path is required")
	}
	info, err := os.Lstat(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("offline data dir %s does not exist", dataDir)
	}
	if err != nil {
		return fmt.Errorf("inspect offline data dir %s: %w", dataDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("offline data dir %s is a symlink", dataDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("offline data dir %s is not a directory", dataDir)
	}
	return nil
}

func validateOutputFormat(format string) error {
	switch strings.TrimSpace(format) {
	case "", formatText, formatJSON:
		return nil
	default:
		return fmt.Errorf("format must be text or json")
	}
}

func writePreflightReport(w io.Writer, report shunter.DataDirCompatibilityReport, format string) error {
	switch strings.TrimSpace(format) {
	case "", formatText:
		var out strings.Builder
		fmt.Fprintf(&out, "status: %s\n", report.Status)
		fmt.Fprintf(&out, "compatible: %t\n", report.Compatible)
		fmt.Fprintf(&out, "data_dir: %s\n", report.DataDir)
		fmt.Fprintf(&out, "requires_backup: %t\n", report.RequiresBackup)
		fmt.Fprintf(&out, "requires_offline_hook: %t\n", report.RequiresOfflineHook)
		if report.BlockingError != "" {
			fmt.Fprintf(&out, "blocking_error: %s\n", report.BlockingError)
		}
		_, err := io.WriteString(w, out.String())
		return err
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
		var out strings.Builder
		fmt.Fprintf(&out, "status: migrated\n")
		fmt.Fprintf(&out, "data_dir: %s\n", result.DataDir)
		fmt.Fprintf(&out, "recovered_tx_id: %d\n", result.RecoveredTxID)
		fmt.Fprintf(&out, "durable_tx_id: %d\n", result.DurableTxID)
		fmt.Fprintf(&out, "hooks: %d\n", len(result.Hooks))
		_, err := io.WriteString(w, out.String())
		return err
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

func writeBackupPreparationResult(w io.Writer, result backupPreparationResult, format string) error {
	switch strings.TrimSpace(format) {
	case "", formatText:
		var out strings.Builder
		fmt.Fprintf(&out, "status: %s\n", result.Status)
		fmt.Fprintf(&out, "data_dir: %s\n", result.DataDir)
		fmt.Fprintf(&out, "recovered_tx_id: %d\n", result.RecoveredTxID)
		fmt.Fprintf(&out, "snapshot_tx_id: %d\n", result.SnapshotTxID)
		_, err := io.WriteString(w, out.String())
		return err
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
  hosted-chat-maintain prepare-backup [--data-dir ./data/hosted-chat] [--format text|json]
  hosted-chat-maintain migrate [--data-dir ./data/hosted-chat] [--format text|json]

These commands are app-owned: they link the hosted-chat module directly so
DataDir compatibility checks and migration hooks use the same declarations as
the hosted server. prepare-backup requires an existing offline DataDir, recovers
it without starting runtime services or startup hooks, creates a snapshot,
compacts only the covered log prefix, and closes before reporting success.
`)
}

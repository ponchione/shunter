package main

import (
	"io"
	"strings"

	shunter "github.com/ponchione/shunter"
)

func runBackup(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter backup")
	dataDir := fs.String("data-dir", "", "offline runtime DataDir to copy")
	outputPath := fs.String("out", "", "backup output directory; must not already exist")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "data-dir", *dataDir); code != 0 {
		return code
	}
	if code := requirePath(stderr, "out", *outputPath); code != 0 {
		return code
	}

	if err := shunter.BackupDataDir(*dataDir, *outputPath); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	writeCLIStatusf(stdout, "backed up %s to %s\n", strings.TrimSpace(*dataDir), strings.TrimSpace(*outputPath))
	return 0
}

func runRestore(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter restore")
	backupPath := fs.String("backup", "", "backup directory to restore")
	dataDir := fs.String("data-dir", "", "restore destination DataDir; must not contain existing state")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := requireNoArgs(stderr, fs); code != 0 {
		return code
	}
	if code := requirePath(stderr, "backup", *backupPath); code != 0 {
		return code
	}
	if code := requirePath(stderr, "data-dir", *dataDir); code != 0 {
		return code
	}

	if err := shunter.RestoreDataDir(*backupPath, *dataDir); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	writeCLIStatusf(stdout, "restored %s to %s\n", strings.TrimSpace(*backupPath), strings.TrimSpace(*dataDir))
	return 0
}

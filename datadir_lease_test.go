package shunter

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	dataDirLeaseChildEnv = "SHUNTER_DATA_DIR_LEASE_CHILD"
	dataDirLeasePathEnv  = "SHUNTER_DATA_DIR_LEASE_PATH"
)

func TestBuildRejectsCanonicalDataDirAliasesUntilOwnerCloses(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	owner, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build owner: %v", err)
	}

	lexicalAlias := filepath.Join(root, "nested", "..", "data", ".")
	_, err = Build(validChatModule(), Config{DataDir: lexicalAlias})
	requireDataDirInUse(t, err, dataDir)

	link := filepath.Join(root, "data-link")
	if err := os.Symlink(dataDir, link); err == nil {
		_, err = Build(validChatModule(), Config{DataDir: link})
		requireDataDirInUse(t, err, dataDir)
	}

	if err := owner.Close(); err != nil {
		t.Fatalf("Close owner: %v", err)
	}
	reopened, err := Build(validChatModule(), Config{DataDir: lexicalAlias})
	if err != nil {
		t.Fatalf("Build after owner close: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close reopened runtime: %v", err)
	}
}

func TestFailedBuildReleasesDataDirOwnership(t *testing.T) {
	dataDir := t.TempDir()
	initial, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build initial runtime: %v", err)
	}
	if err := initial.Close(); err != nil {
		t.Fatalf("Close initial runtime: %v", err)
	}

	other := NewModule("other").SchemaVersion(1).TableDef(messagesTableDef())
	if _, err := Build(other, Config{DataDir: dataDir}); err == nil {
		t.Fatal("Build with mismatched module returned nil")
	}

	reopened, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build after failed Build: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close reopened runtime: %v", err)
	}
}

func TestFailedStartRetainsDataDirOwnershipUntilClose(t *testing.T) {
	dataDir := t.TempDir()
	owner, err := Build(validChatModule(), Config{
		DataDir:        dataDir,
		EnableProtocol: true,
		AuthMode:       AuthModeStrict,
	})
	if err != nil {
		t.Fatalf("Build owner: %v", err)
	}
	if err := owner.Start(context.Background()); err == nil {
		t.Fatal("Start without strict auth signing key returned nil")
	}

	_, err = Build(validChatModule(), Config{DataDir: dataDir})
	requireDataDirInUse(t, err, dataDir)
	if err := owner.Close(); err != nil {
		t.Fatalf("Close failed-start owner: %v", err)
	}

	reopened, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build after failed-start owner close: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close reopened runtime: %v", err)
	}
}

func TestDataDirOwnershipAllowsRecoveryAndAppendAfterClose(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Build(dataDirBackupTestModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build first runtime: %v", err)
	}
	if err := first.Start(context.Background()); err != nil {
		t.Fatalf("Start first runtime: %v", err)
	}
	firstResult := commitLeaseTestMessage(t, first, "alpha")
	if firstResult.TxID != 1 {
		t.Fatalf("first tx ID = %d, want 1", firstResult.TxID)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close first runtime: %v", err)
	}

	second, err := Build(dataDirBackupTestModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build second runtime: %v", err)
	}
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("Start second runtime: %v", err)
	}
	secondResult := commitLeaseTestMessage(t, second, "bravo")
	if secondResult.TxID != 2 {
		t.Fatalf("second tx ID = %d, want 2", secondResult.TxID)
	}
	assertDataDirRestoredMessageBodies(t, second, []string{"alpha", "bravo"})
	if err := second.Close(); err != nil {
		t.Fatalf("Close second runtime: %v", err)
	}
}

func TestOfflineDataDirOperationsRejectLiveOwner(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(root, "backup")
	seed, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build seed runtime: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close seed runtime: %v", err)
	}
	if err := BackupDataDir(dataDir, backupDir); err != nil {
		t.Fatalf("BackupDataDir seed: %v", err)
	}

	owner, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build owner: %v", err)
	}
	defer owner.Close()

	requireDataDirInUse(t, BackupDataDir(dataDir, filepath.Join(root, "blocked-backup")), dataDir)
	requireDataDirInUse(t, RestoreDataDir(backupDir, dataDir), dataDir)
	requireDataDirInUse(t, CheckDataDirCompatibility(validChatModule(), Config{DataDir: dataDir}), dataDir)
	_, err = CheckDataDirCompatibilityReport(validChatModule(), Config{DataDir: dataDir})
	requireDataDirInUse(t, err, dataDir)
	_, err = RunDataDirMigrations(context.Background(), validChatModule(), Config{DataDir: dataDir}, func(context.Context, *MigrationContext) error {
		return nil
	})
	requireDataDirInUse(t, err, dataDir)
}

func TestSubprocessDataDirOwnerBlocksOperationsAndCrashReleasesLock(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(root, "backup")
	seed, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build seed runtime: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close seed runtime: %v", err)
	}
	if err := BackupDataDir(dataDir, backupDir); err != nil {
		t.Fatalf("BackupDataDir seed: %v", err)
	}

	child := startDataDirLeaseChild(t, dataDir)
	_, err = Build(validChatModule(), Config{DataDir: dataDir})
	requireDataDirInUse(t, err, dataDir)
	requireDataDirInUse(t, RestoreDataDir(backupDir, dataDir), dataDir)
	_, err = RunDataDirMigrations(context.Background(), validChatModule(), Config{DataDir: dataDir}, func(context.Context, *MigrationContext) error {
		return nil
	})
	requireDataDirInUse(t, err, dataDir)

	if err := child.Process.Kill(); err != nil {
		t.Fatalf("kill owner subprocess: %v", err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("killed owner subprocess exited successfully")
	}

	reopened, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build after owner subprocess crash: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close reopened runtime: %v", err)
	}
}

func TestDataDirLeaseSubprocessOwner(t *testing.T) {
	if os.Getenv(dataDirLeaseChildEnv) != "1" {
		return
	}
	dataDir := os.Getenv(dataDirLeasePathEnv)
	owner, err := Build(validChatModule(), Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build subprocess owner: %v", err)
	}
	defer owner.Close()
	if _, err := fmt.Fprintln(os.Stdout, "READY"); err != nil {
		t.Fatalf("signal subprocess readiness: %v", err)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func commitLeaseTestMessage(t *testing.T, rt *Runtime, body string) ReducerResult {
	t.Helper()
	result, err := rt.CallReducer(context.Background(), "insert_message", []byte(body))
	if err != nil {
		t.Fatalf("CallReducer(%q): %v", body, err)
	}
	if result.Status != StatusCommitted {
		t.Fatalf("CallReducer(%q) result = %+v, want committed", body, result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.WaitUntilDurable(ctx, result.TxID); err != nil {
		t.Fatalf("WaitUntilDurable(%d): %v", result.TxID, err)
	}
	return result
}

func requireDataDirInUse(t *testing.T, err error, dataDir string) {
	t.Helper()
	if !errors.Is(err, ErrDataDirInUse) {
		t.Fatalf("error = %v, want ErrDataDirInUse", err)
	}
	canonical, resolveErr := resolvePathForContainment(dataDir)
	if resolveErr != nil {
		t.Fatalf("resolve canonical data dir: %v", resolveErr)
	}
	if !strings.Contains(err.Error(), canonical) {
		t.Fatalf("error = %q, want canonical path %q", err, canonical)
	}
}

func startDataDirLeaseChild(t *testing.T, dataDir string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDataDirLeaseSubprocessOwner$")
	cmd.Env = append(os.Environ(),
		dataDirLeaseChildEnv+"=1",
		dataDirLeasePathEnv+"="+dataDir,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("owner subprocess stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("owner subprocess stdout: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start owner subprocess: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "READY" {
				ready <- nil
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ready <- err
			return
		}
		ready <- fmt.Errorf("owner subprocess exited before readiness: %s", stderr.String())
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for owner subprocess: %s", stderr.String())
	}
	return cmd
}

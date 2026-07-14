package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractworkflow"
	"github.com/ponchione/shunter/protocol"
)

var errCLIOutput = errors.New("injected CLI output failure")

type failingCLIWriter struct{}

func (failingCLIWriter) Write([]byte) (int, error) {
	return 0, errCLIOutput
}

func TestRunningAppSuccessWritersPropagateOutputErrors(t *testing.T) {
	contract := cliContractFixture()
	identity := protocol.IdentityToken{}
	writers := []struct {
		name  string
		write func(io.Writer, string) error
	}{
		{
			name: "call",
			write: func(w io.Writer, format string) error {
				return writeCallSuccess(w, format, contract, "ws://app.test/subscribe", identity, protocol.TransactionUpdate{
					Status:      protocol.StatusCommitted{},
					ReducerCall: protocol.ReducerCallInfo{ReducerName: "send_message"},
				})
			},
		},
		{
			name: "procedure",
			write: func(w io.Writer, format string) error {
				return writeProcedureSuccess(w, format, contract, "ws://app.test/subscribe", identity, protocol.ProcedureResponse{}, "maintain")
			},
		},
		{
			name: "query",
			write: func(w io.Writer, format string) error {
				return writeQueryRowsSuccess(w, format, contract, "ws://app.test/subscribe", identity, 0, "history", "Query", contractworkflow.JSONQueryRows{})
			},
		},
	}
	for _, writer := range writers {
		for _, format := range []string{contractworkflow.FormatText, contractworkflow.FormatJSON} {
			t.Run(writer.name+"/"+format, func(t *testing.T) {
				err := writer.write(failingCLIWriter{}, format)
				if !errors.Is(err, errCLIOutput) {
					t.Fatalf("write error = %v, want %v", err, errCLIOutput)
				}
			})
		}
	}
}

func TestVersionOutputErrorReturnsFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(failingCLIWriter{}, &stderr, []string{"version"}); code != 1 {
		t.Fatalf("version exit code = %d, stderr = %s, want 1", code, stderr.String())
	}
	assertContains(t, stderr.String(), errCLIOutput.Error())
}

func TestStatusOutputErrorsReturnFailure(t *testing.T) {
	t.Run("backup", func(t *testing.T) {
		root := t.TempDir()
		dataDir := filepath.Join(root, "data")
		if err := os.Mkdir(dataDir, 0o755); err != nil {
			t.Fatalf("create data dir: %v", err)
		}
		writeCLIBytes(t, dataDir, "segment", []byte("data"))
		var stderr bytes.Buffer
		code := run(failingCLIWriter{}, &stderr, []string{"backup", "--data-dir", dataDir, "--out", filepath.Join(root, "backup")})
		if code != 1 {
			t.Fatalf("backup exit code = %d, stderr = %s, want 1", code, stderr.String())
		}
		assertContains(t, stderr.String(), errCLIOutput.Error())
	})

	t.Run("restore", func(t *testing.T) {
		root := t.TempDir()
		dataDir := filepath.Join(root, "data")
		if err := os.Mkdir(dataDir, 0o755); err != nil {
			t.Fatalf("create data dir: %v", err)
		}
		writeCLIBytes(t, dataDir, "segment", []byte("data"))
		backupDir := filepath.Join(root, "backup")
		if err := shunter.BackupDataDir(dataDir, backupDir); err != nil {
			t.Fatalf("create backup: %v", err)
		}
		var stderr bytes.Buffer
		code := run(failingCLIWriter{}, &stderr, []string{"restore", "--backup", backupDir, "--data-dir", filepath.Join(root, "restored")})
		if code != 1 {
			t.Fatalf("restore exit code = %d, stderr = %s, want 1", code, stderr.String())
		}
		assertContains(t, stderr.String(), errCLIOutput.Error())
	})

	t.Run("codegen", func(t *testing.T) {
		root := t.TempDir()
		contractPath := writeCLIContract(t, root, "contract.json", cliContractFixture())
		var stderr bytes.Buffer
		code := run(failingCLIWriter{}, &stderr, []string{"contract", "codegen", "--contract", contractPath, "--out", filepath.Join(root, "client.ts")})
		if code != 1 {
			t.Fatalf("codegen exit code = %d, stderr = %s, want 1", code, stderr.String())
		}
		assertContains(t, stderr.String(), errCLIOutput.Error())
	})
}

package shunter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExternalModuleCompilesWithoutWebSocketReplace(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("get repo root: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(`module external.test/shunterconsumer

go 1.25.5

require github.com/ponchione/shunter v0.0.0

replace github.com/ponchione/shunter => `+repoRoot+`
`), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"context"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/types"
)

var _ shunter.Value = types.NewUint64(1)
var _ shunter.ProductValue = shunter.ProductValue{types.NewUint64(1)}
var _ shunter.RowID
var _ shunter.TxID
var _ shunter.ReducerDB
var _ shunter.IndexBound = shunter.Inclusive(types.NewUint64(1))
var _ func(*shunter.Runtime, context.Context, types.TxID) error = (*shunter.Runtime).WaitUntilDurable

func main() {}
`), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	runExternalModuleGo(t, dir, "mod", "tidy")
	runExternalModuleGo(t, dir, "test", "./...")
}

func externalModuleGoEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOWORK=") || strings.HasPrefix(entry, "GOFLAGS=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOWORK=off", "GOFLAGS=")
}

func runExternalModuleGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = externalModuleGoEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("external module go %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

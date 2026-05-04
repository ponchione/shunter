package shunter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/schema"
)

func TestBuildBootstrapsCommittedStateForModuleTables(t *testing.T) {
	dir := t.TempDir()

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if rt.state == nil {
		t.Fatal("runtime state is nil")
	}
	if rt.registry == nil {
		t.Fatal("runtime registry is nil")
	}
	tid, _, ok := rt.registry.TableByName("messages")
	if !ok {
		t.Fatal("messages table missing from runtime registry")
	}
	if _, ok := rt.state.Table(tid); !ok {
		t.Fatal("messages table was not registered in committed state")
	}
	if rt.dataDir != dir {
		t.Fatalf("runtime dataDir = %q, want %q", rt.dataDir, dir)
	}
	if rt.resumePlan.NextTxID == 0 && rt.recoveredTxID != 0 {
		t.Fatalf("recovered tx = %d with zero next tx", rt.recoveredTxID)
	}
}

func TestBuildReopensExistingBootstrappedState(t *testing.T) {
	dir := t.TempDir()

	first, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("first Build returned error: %v", err)
	}
	if first.state == nil {
		t.Fatal("first runtime state is nil")
	}

	second, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("second Build returned error: %v", err)
	}
	if second.state == nil {
		t.Fatal("second runtime state is nil")
	}
	if second.registry == nil {
		t.Fatal("second runtime registry is nil")
	}
	if second.registry.Version() != 1 {
		t.Fatalf("registry version = %d, want 1", second.registry.Version())
	}
	tid, _, ok := second.registry.TableByName("messages")
	if !ok {
		t.Fatal("messages table missing from reopened registry")
	}
	if _, ok := second.state.Table(tid); !ok {
		t.Fatal("messages table missing from reopened committed state")
	}
}

func TestBuildWithBlankDataDirNormalizesToRuntimeDefault(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)

	rt, err := Build(validChatModule(), Config{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if rt.dataDir != defaultDataDir {
		t.Fatalf("runtime dataDir = %q, want %q", rt.dataDir, defaultDataDir)
	}
	if rt.Config().DataDir != "" {
		t.Fatalf("public Config().DataDir = %q, want blank original value", rt.Config().DataDir)
	}
	info, err := os.Stat(defaultDataDir)
	if err != nil {
		t.Fatalf("default data dir was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("default data dir path is not a directory")
	}
}

func TestBuildCreatesReducerRegistryFromModule(t *testing.T) {
	reduce := func(_ *schema.ReducerContext, _ []byte) ([]byte, error) { return nil, nil }
	onConnect := func(_ *schema.ReducerContext) error { return nil }
	onDisconnect := func(_ *schema.ReducerContext) error { return nil }

	mod := validChatModule().
		Reducer("send_message", reduce).
		OnConnect(onConnect).
		OnDisconnect(onDisconnect)

	rt, err := Build(mod, Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if rt.reducers == nil {
		t.Fatal("runtime reducer registry is nil")
	}
	if !rt.reducers.IsFrozen() {
		t.Fatal("runtime reducer registry is not frozen")
	}
	if _, ok := rt.reducers.Lookup("send_message"); !ok {
		t.Fatal("send_message reducer missing")
	}
	if _, ok := rt.reducers.LookupLifecycle(executor.LifecycleOnConnect); !ok {
		t.Fatal("on-connect lifecycle reducer missing")
	}
	if _, ok := rt.reducers.LookupLifecycle(executor.LifecycleOnDisconnect); !ok {
		t.Fatal("on-disconnect lifecycle reducer missing")
	}
}

func TestCheckDataDirCompatibilityAcceptsMissingAndMatchingDataDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	mod := validChatModule()
	if err := CheckDataDirCompatibility(mod, Config{DataDir: path}); err != nil {
		t.Fatalf("CheckDataDirCompatibility missing DataDir returned error: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight stat = %v, want missing DataDir left uncreated", err)
	}
	if _, err := Build(mod, Config{DataDir: path}); err != nil {
		t.Fatalf("Build after preflight returned error: %v", err)
	}

	dir := t.TempDir()
	if _, err := Build(validChatModule(), Config{DataDir: dir}); err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}
	if err := CheckDataDirCompatibility(validChatModule(), Config{DataDir: dir}); err != nil {
		t.Fatalf("CheckDataDirCompatibility matching DataDir returned error: %v", err)
	}
}

func TestCheckDataDirCompatibilityReportsSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	if _, err := Build(validChatModule(), Config{DataDir: dir}); err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}
	mismatch := messagesTableDef()
	mismatch.Columns[1].Name = "text"

	err := CheckDataDirCompatibility(NewModule("chat").SchemaVersion(1).TableDef(mismatch), Config{DataDir: dir})
	if err == nil {
		t.Fatal("CheckDataDirCompatibility returned nil, want schema mismatch")
	}
	var schemaErr *commitlog.SchemaMismatchError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("CheckDataDirCompatibility error = %v, want SchemaMismatchError", err)
	}
	assertErrorContains(t, err, "check data dir compatibility")
	assertErrorContains(t, err, "name mismatch")
	assertErrorContains(t, err, "body")
	assertErrorContains(t, err, "text")
}

func validChatModule() *Module {
	return NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef())
}

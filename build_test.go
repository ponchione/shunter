package shunter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
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

func TestBuildWritesDataDirMetadata(t *testing.T) {
	oldVersion := Version
	oldCommit := Commit
	oldDate := Date
	Version = "v9.8.7"
	Commit = "abc123"
	Date = "2026-05-03T12:34:56Z"
	defer func() {
		Version = oldVersion
		Commit = oldCommit
		Date = oldDate
	}()

	dir := t.TempDir()
	if _, err := Build(validChatModule().Version("v1.2.3"), Config{DataDir: dir}); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	metadata, ok, err := readDataDirMetadata(dir)
	if err != nil {
		t.Fatalf("read data dir metadata: %v", err)
	}
	if !ok {
		t.Fatal("data dir metadata missing")
	}
	if metadata.FormatVersion != dataDirMetadataFormatVersion {
		t.Fatalf("metadata format_version = %d, want %d", metadata.FormatVersion, dataDirMetadataFormatVersion)
	}
	if metadata.ContractVersion != ModuleContractVersion {
		t.Fatalf("metadata contract_version = %d, want %d", metadata.ContractVersion, ModuleContractVersion)
	}
	if metadata.Shunter.Version != "v9.8.7" || metadata.Shunter.Commit != "abc123" || metadata.Shunter.Date != "2026-05-03T12:34:56Z" {
		t.Fatalf("metadata Shunter version = %+v, want linker-style build info", metadata.Shunter)
	}
	if metadata.Module.Name != "chat" || metadata.Module.Version != "v1.2.3" || metadata.Module.SchemaVersion != 1 {
		t.Fatalf("metadata module version = %+v, want chat v1.2.3 schema 1", metadata.Module)
	}
}

func TestBuildSyncsDataDirMetadataParentAfterAtomicRename(t *testing.T) {
	dir := t.TempDir()
	originalSyncDir := syncDataDirMetadataDir
	var synced []string
	syncDataDirMetadataDir = func(path string) error {
		synced = append(synced, path)
		if path != dir {
			t.Fatalf("metadata parent sync path = %q, want %q", path, dir)
		}
		if _, ok, err := readDataDirMetadata(dir); err != nil || !ok {
			t.Fatalf("metadata not published before parent sync: ok=%v err=%v", ok, err)
		}
		return nil
	}
	defer func() { syncDataDirMetadataDir = originalSyncDir }()

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if len(synced) != 1 {
		t.Fatalf("metadata parent sync calls = %v, want one call", synced)
	}
}

func TestBuildUpdatesDataDirModuleVersionMetadataWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	if _, err := Build(validChatModule().Version("v1.0.0"), Config{DataDir: dir}); err != nil {
		t.Fatalf("first Build returned error: %v", err)
	}
	if _, err := Build(validChatModule().Version("v1.1.0"), Config{DataDir: dir}); err != nil {
		t.Fatalf("second Build with updated module version returned error: %v", err)
	}

	metadata, ok, err := readDataDirMetadata(dir)
	if err != nil {
		t.Fatalf("read data dir metadata: %v", err)
	}
	if !ok {
		t.Fatal("data dir metadata missing")
	}
	if metadata.Module.Version != "v1.1.0" {
		t.Fatalf("metadata module version = %q, want updated app module version", metadata.Module.Version)
	}
}

func TestDataDirMetadataRejectsDifferentModuleName(t *testing.T) {
	dir := t.TempDir()
	if _, err := Build(validChatModule(), Config{DataDir: dir}); err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}
	other := NewModule("other").SchemaVersion(1).TableDef(messagesTableDef())

	err := CheckDataDirCompatibility(other, Config{DataDir: dir})
	if err == nil {
		t.Fatal("CheckDataDirCompatibility returned nil, want metadata mismatch")
	}
	assertErrorContains(t, err, "data dir metadata module name")
	assertErrorContains(t, err, "chat")
	assertErrorContains(t, err, "other")

	_, err = Build(other, Config{DataDir: dir})
	if err == nil {
		t.Fatal("Build returned nil, want metadata mismatch")
	}
	assertErrorContains(t, err, "data dir metadata module name")
	assertErrorContains(t, err, "chat")
	assertErrorContains(t, err, "other")
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
	if rt.buildConfig.OneOffQueryMaxRows != protocol.DefaultSQLQueryMaxRows {
		t.Fatalf("OneOffQueryMaxRows = %d, want %d", rt.buildConfig.OneOffQueryMaxRows, protocol.DefaultSQLQueryMaxRows)
	}
	if rt.buildConfig.OneOffQueryMaxBytes != protocol.DefaultSQLQueryMaxBytes {
		t.Fatalf("OneOffQueryMaxBytes = %d, want %d", rt.buildConfig.OneOffQueryMaxBytes, protocol.DefaultSQLQueryMaxBytes)
	}
	if rt.buildConfig.SubscriptionInitialRowLimit != defaultSubscriptionInitialRows {
		t.Fatalf("SubscriptionInitialRowLimit = %d, want %d", rt.buildConfig.SubscriptionInitialRowLimit, defaultSubscriptionInitialRows)
	}
	if rt.buildConfig.SubscriptionSnapshotMaxBytes != defaultSubscriptionSnapshotBytes ||
		rt.buildConfig.SubscriptionMaxQueriesPerSet != protocol.DefaultSubscriptionMaxQueriesPerSet ||
		rt.buildConfig.SubscriptionMaxActiveSetsPerConnection != defaultSubscriptionActiveSets ||
		rt.buildConfig.SubscriptionMaxActiveSubscriptionsPerConnection != defaultSubscriptionActiveSubscriptions {
		t.Fatalf("subscription defaults = %+v", rt.buildConfig)
	}
}

func TestBuildRejectsNegativeResourceLimits(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "one-off rows",
			cfg:  Config{DataDir: t.TempDir(), OneOffQueryMaxRows: -1},
			want: "one-off query max rows must not be negative",
		},
		{
			name: "one-off bytes",
			cfg:  Config{DataDir: t.TempDir(), OneOffQueryMaxBytes: -1},
			want: "one-off query max bytes must not be negative",
		},
		{
			name: "subscription initial rows",
			cfg:  Config{DataDir: t.TempDir(), SubscriptionInitialRowLimit: -1},
			want: "subscription initial row limit must not be negative",
		},
		{
			name: "subscription snapshot bytes",
			cfg:  Config{DataDir: t.TempDir(), SubscriptionSnapshotMaxBytes: -1},
			want: "subscription snapshot max bytes must not be negative",
		},
		{
			name: "subscription queries per set",
			cfg:  Config{DataDir: t.TempDir(), SubscriptionMaxQueriesPerSet: -1},
			want: "subscription max queries per set must not be negative",
		},
		{
			name: "subscription active sets",
			cfg:  Config{DataDir: t.TempDir(), SubscriptionMaxActiveSetsPerConnection: -1},
			want: "subscription max active sets per connection must not be negative",
		},
		{
			name: "subscription active queries",
			cfg:  Config{DataDir: t.TempDir(), SubscriptionMaxActiveSubscriptionsPerConnection: -1},
			want: "subscription max active subscriptions per connection must not be negative",
		},
		{
			name: "relations",
			cfg:  Config{DataDir: t.TempDir(), SubscriptionMaxMultiJoinRelations: -1},
			want: "subscription max multi-join relations must not be negative",
		},
		{
			name: "rows per relation",
			cfg:  Config{DataDir: t.TempDir(), SubscriptionMaxMultiJoinRowsPerRelation: -1},
			want: "subscription max multi-join rows per relation must not be negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(validChatModule(), tt.cfg)
			if err == nil {
				t.Fatal("Build returned nil error, want negative resource limit rejection")
			}
			assertErrorContains(t, err, tt.want)
		})
	}
}

func TestBuildRejectsSubscriptionQueryLimitAboveDecoderCeiling(t *testing.T) {
	_, err := Build(validChatModule(), Config{
		DataDir:                      t.TempDir(),
		SubscriptionMaxQueriesPerSet: int(protocol.MaxSubscribeMultiQueriesHard) + 1,
	})
	if err == nil {
		t.Fatal("Build returned nil error, want decoder-ceiling rejection")
	}
	assertErrorContains(t, err, "exceeds decoder hard limit")
}

func TestStartConfiguresSubscriptionLimits(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir:                                         t.TempDir(),
		SubscriptionInitialRowLimit:                     128,
		SubscriptionSnapshotMaxBytes:                    1_024,
		SubscriptionMaxQueriesPerSet:                    3,
		SubscriptionMaxActiveSetsPerConnection:          4,
		SubscriptionMaxActiveSubscriptionsPerConnection: 5,
		SubscriptionMaxMultiJoinRelations:               4,
		SubscriptionMaxMultiJoinRowsPerRelation:         256,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if rt.subscriptions == nil {
		t.Fatal("subscriptions manager is nil after Start")
	}
	if rt.subscriptions.InitialRowLimit != 128 {
		t.Fatalf("InitialRowLimit = %d, want 128", rt.subscriptions.InitialRowLimit)
	}
	if rt.subscriptions.SnapshotByteLimit != 1_024 || rt.subscriptions.MaxQueriesPerSet != 3 ||
		rt.subscriptions.MaxActiveSetsPerConnection != 4 || rt.subscriptions.MaxActiveSubscriptionsPerConnection != 5 {
		t.Fatalf("subscription quota wiring = %+v", rt.subscriptions)
	}
	if rt.subscriptions.MaxMultiJoinRelations != 4 {
		t.Fatalf("MaxMultiJoinRelations = %d, want 4", rt.subscriptions.MaxMultiJoinRelations)
	}
	if rt.subscriptions.MaxMultiJoinRowsPerRelation != 256 {
		t.Fatalf("MaxMultiJoinRowsPerRelation = %d, want 256", rt.subscriptions.MaxMultiJoinRowsPerRelation)
	}
}

func TestBuildCreatesMissingDataDirWithPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are POSIX-specific")
	}

	dir := filepath.Join(t.TempDir(), "state")
	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat data dir: %v", err)
	}
	if got := info.Mode().Perm(); got != dataDirMode {
		t.Fatalf("data dir mode = %#o, want %#o", got, dataDirMode)
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

func TestCheckDataDirCompatibilityAcceptsEmptyDataDirWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	if err := CheckDataDirCompatibility(validChatModule(), Config{DataDir: dir}); err != nil {
		t.Fatalf("CheckDataDirCompatibility empty DataDir returned error: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read empty DataDir after preflight: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty DataDir entries after preflight = %#v, want none", entries)
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

func TestCheckDataDirCompatibilityAllowsSafeAdditiveTableAndIndex(t *testing.T) {
	dir := t.TempDir()
	if _, err := Build(validChatModule(), Config{DataDir: dir}); err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}
	messages := messagesTableDef()
	messages.Indexes = []schema.IndexDefinition{{Name: "body_idx", Columns: []string{"body"}}}
	audit := schema.TableDefinition{
		Name: "audit_events",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "body", Type: types.KindString},
		},
	}
	next := NewModule("chat").SchemaVersion(2).TableDef(messages).TableDef(audit)

	report, err := CheckDataDirCompatibilityReport(next, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("CheckDataDirCompatibilityReport returned error: %v", err)
	}
	if !report.Compatible || report.Status != DataDirCompatibilityAdditive {
		t.Fatalf("compatibility report = %#v, want additive compatible", report)
	}
	if !report.RequiresBackup || report.RequiresOfflineHook {
		t.Fatalf("backup/offline flags = %t/%t, want backup without required hook", report.RequiresBackup, report.RequiresOfflineHook)
	}
	if len(report.Schema.Changes) != 3 {
		t.Fatalf("schema changes = %#v, want version, index, and table", report.Schema.Changes)
	}
	if err := CheckDataDirCompatibility(next, Config{DataDir: dir}); err != nil {
		t.Fatalf("CheckDataDirCompatibility additive returned error: %v", err)
	}
	rt, err := Build(next, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build additive returned error: %v", err)
	}
	defer rt.Close()

	auditID, _, ok := rt.registry.TableByName("audit_events")
	if !ok {
		t.Fatal("audit_events table missing after additive build")
	}
	sysClientsID, _, ok := rt.registry.TableByName("sys_clients")
	if !ok || sysClientsID != 1 {
		t.Fatalf("sys_clients table id = %d ok=%t, want preserved snapshot id 1", sysClientsID, ok)
	}
	if auditID <= sysClientsID {
		t.Fatalf("audit_events id = %d, want assigned after existing snapshot tables", auditID)
	}
	exported := rt.ExportSchema()
	if exportedSysClientsID, ok := exportedTableID(exported, "sys_clients"); !ok || exportedSysClientsID != sysClientsID {
		t.Fatalf("ExportSchema sys_clients id = %d ok=%t, want reconciled id %d", exportedSysClientsID, ok, sysClientsID)
	}
	if exportedAuditID, ok := exportedTableID(exported, "audit_events"); !ok || exportedAuditID != auditID {
		t.Fatalf("ExportSchema audit_events id = %d ok=%t, want reconciled id %d", exportedAuditID, ok, auditID)
	}
}

func TestCheckDataDirCompatibilityBlocksLogOnlySchemaVersionDrift(t *testing.T) {
	dir := t.TempDir()
	initial, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}
	t.Cleanup(func() { _ = initial.Close() })
	messageID, _, ok := initial.registry.TableByName("messages")
	if !ok {
		t.Fatal("messages table missing")
	}
	tx := store.NewTransaction(initial.state, initial.registry)
	if _, err := tx.Insert(messageID, types.ProductValue{types.NewUint64(0), types.NewString("hello")}); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	tx.Seal()
	changeset, err := store.Commit(initial.state, tx)
	if err != nil {
		t.Fatalf("commit message: %v", err)
	}
	changeset.TxID = 1
	initial.state.SetCommittedTxID(1)
	options := commitlog.DefaultCommitLogOptions()
	durability, err := commitlog.NewDurabilityWorkerWithResumePlan(dir, initial.resumePlan, options)
	if err != nil {
		t.Fatalf("open durability worker: %v", err)
	}
	durability.EnqueueCommitted(1, changeset)
	wait := durability.WaitUntilDurable(1)
	select {
	case got := <-wait:
		if got != 1 {
			t.Fatalf("durable tx = %d, want 1", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for durable tx")
	}
	if _, err := durability.Close(); err != nil {
		t.Fatalf("close durability worker: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "0")); err != nil {
		t.Fatalf("remove bootstrap snapshot: %v", err)
	}

	audit := messagesTableDef()
	audit.Name = "audit_events"
	next := NewModule("chat").SchemaVersion(2).TableDef(audit).TableDef(messagesTableDef())
	report, err := CheckDataDirCompatibilityReport(next, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("CheckDataDirCompatibilityReport returned error: %v", err)
	}
	if report.Compatible || report.Status != DataDirCompatibilityBlocked {
		t.Fatalf("compatibility report = %#v, want blocked", report)
	}
	if report.Schema.Status != schema.SchemaCompatibilityBlocked || len(report.Schema.Issues) == 0 {
		t.Fatalf("schema report = %#v, want blocked issue", report.Schema)
	}
	if !strings.Contains(report.BlockingError, "no selected snapshot") {
		t.Fatalf("blocking error = %q, want no selected snapshot detail", report.BlockingError)
	}
	if _, err := Build(next, Config{DataDir: dir}); err == nil {
		t.Fatal("Build with log-only schema drift returned nil, want blocked")
	} else if !strings.Contains(err.Error(), "no selected snapshot") {
		t.Fatalf("Build error = %v, want no selected snapshot detail", err)
	}
}

func TestCheckDataDirCompatibilityReportBlocksRowShapeChanges(t *testing.T) {
	dir := t.TempDir()
	if _, err := Build(validChatModule(), Config{DataDir: dir}); err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}
	mismatch := messagesTableDef()
	mismatch.Columns = append(mismatch.Columns, schema.ColumnDefinition{Name: "extra", Type: types.KindString, Nullable: true})
	next := NewModule("chat").SchemaVersion(2).TableDef(mismatch)

	report, err := CheckDataDirCompatibilityReport(next, Config{DataDir: dir})
	if err != nil {
		t.Fatalf("CheckDataDirCompatibilityReport returned error: %v", err)
	}
	if report.Compatible || report.Status != DataDirCompatibilityBlocked {
		t.Fatalf("compatibility report = %#v, want blocked", report)
	}
	if !report.RequiresBackup || !report.RequiresOfflineHook {
		t.Fatalf("backup/offline flags = %t/%t, want both required", report.RequiresBackup, report.RequiresOfflineHook)
	}
	if report.BlockingError == "" {
		t.Fatal("blocked report missing blocking error")
	}
	if !strings.Contains(report.BlockingError, "row-shape changes require an app-owned migration") {
		t.Fatalf("blocking error = %q, want row-shape migration detail", report.BlockingError)
	}
	if report.Schema.Status != schema.SchemaCompatibilityBlocked || len(report.Schema.Issues) == 0 {
		t.Fatalf("schema report = %#v, want blocked issues", report.Schema)
	}
}

func exportedTableID(exported *schema.SchemaExport, name string) (schema.TableID, bool) {
	if exported == nil {
		return 0, false
	}
	for _, table := range exported.Tables {
		if table.Name == name {
			return table.ID, true
		}
	}
	return 0, false
}

func validChatModule() *Module {
	return NewModule("chat").
		SchemaVersion(1).
		TableDef(messagesTableDef())
}

package shunter

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

const (
	defaultDataDir                         = "./shunter-data"
	dataDirMode                            = 0o700
	defaultExecutorQueueCapacity           = 256
	defaultDurabilityQueueCapacity         = 256
	defaultProcedureResultBytes            = 64 << 20
	defaultSubscriptionInitialRows         = 100_000
	defaultSubscriptionSnapshotBytes       = 64 << 20
	defaultSubscriptionActiveSets          = 128
	defaultSubscriptionActiveSubscriptions = 1_024
)

type runtimeBuildPreview struct {
	normalized        Config
	dataDir           string
	schemaOpts        schema.EngineOptions
	registry          schema.SchemaRegistry
	visibilityFilters []VisibilityFilterDescription
}

type DataDirCompatibilityStatus string

const (
	DataDirCompatibilityCompatible DataDirCompatibilityStatus = "compatible"
	DataDirCompatibilityFresh      DataDirCompatibilityStatus = "fresh"
	DataDirCompatibilityAdditive   DataDirCompatibilityStatus = "additive"
	DataDirCompatibilityBlocked    DataDirCompatibilityStatus = "blocked"
)

type DataDirCompatibilityReport struct {
	Compatible          bool                             `json:"compatible"`
	Status              DataDirCompatibilityStatus       `json:"status"`
	DataDir             string                           `json:"data_dir"`
	RequiresBackup      bool                             `json:"requires_backup"`
	RequiresOfflineHook bool                             `json:"requires_offline_hook"`
	Schema              schema.SchemaCompatibilityReport `json:"schema"`
	BlockingError       string                           `json:"blocking_error,omitempty"`
}

func previewRuntimeBuild(mod *Module, cfg Config) (runtimeBuildPreview, error) {
	if mod == nil {
		return runtimeBuildPreview{}, fmt.Errorf("module must not be nil")
	}
	if strings.TrimSpace(mod.name) == "" {
		return runtimeBuildPreview{}, fmt.Errorf("module name must not be empty")
	}

	normalized, dataDir, err := normalizeConfig(cfg)
	if err != nil {
		return runtimeBuildPreview{}, err
	}
	if err := validateModuleDeclarations(mod); err != nil {
		return runtimeBuildPreview{}, err
	}

	schemaOpts := schema.EngineOptions{
		DataDir:                 dataDir,
		ExecutorQueueCapacity:   normalized.ExecutorQueueCapacity,
		DurabilityQueueCapacity: normalized.DurabilityQueueCapacity,
		EnableProtocol:          normalized.EnableProtocol,
	}
	preview, err := mod.builder.BuildPreview(schemaOpts)
	if err != nil {
		return runtimeBuildPreview{}, fmt.Errorf("build hosted runtime schema: %w", err)
	}
	previewRegistry := preview.Registry()
	if err := validateModuleDeclarationSQL(mod, previewRegistry); err != nil {
		return runtimeBuildPreview{}, err
	}
	visibilityFilters, err := validateModuleVisibilityFilters(mod, previewRegistry)
	if err != nil {
		return runtimeBuildPreview{}, err
	}
	if err := validateModuleTableMigrations(mod, previewRegistry); err != nil {
		return runtimeBuildPreview{}, err
	}
	if err := validateModuleMetadata(mod, previewRegistry); err != nil {
		return runtimeBuildPreview{}, err
	}

	return runtimeBuildPreview{
		normalized:        normalized,
		dataDir:           dataDir,
		schemaOpts:        schemaOpts,
		registry:          previewRegistry,
		visibilityFilters: visibilityFilters,
	}, nil
}

func normalizeConfig(cfg Config) (Config, string, error) {
	if cfg.ExecutorQueueCapacity < 0 {
		return Config{}, "", fmt.Errorf("executor queue capacity must not be negative")
	}
	if cfg.DurabilityQueueCapacity < 0 {
		return Config{}, "", fmt.Errorf("durability queue capacity must not be negative")
	}
	if cfg.OneOffQueryMaxRows < 0 {
		return Config{}, "", fmt.Errorf("one-off query max rows must not be negative")
	}
	if cfg.OneOffQueryMaxBytes < 0 {
		return Config{}, "", fmt.Errorf("one-off query max bytes must not be negative")
	}
	if cfg.OneOffQueryMaxWork < 0 {
		return Config{}, "", fmt.Errorf("one-off query max work must not be negative")
	}
	if cfg.ProcedureResultMaxBytes < 0 {
		return Config{}, "", fmt.Errorf("procedure result max bytes must not be negative")
	}
	if cfg.SubscriptionInitialRowLimit < 0 {
		return Config{}, "", fmt.Errorf("subscription initial row limit must not be negative")
	}
	if cfg.SubscriptionSnapshotMaxBytes < 0 {
		return Config{}, "", fmt.Errorf("subscription snapshot max bytes must not be negative")
	}
	if cfg.SubscriptionMaxQueriesPerSet < 0 {
		return Config{}, "", fmt.Errorf("subscription max queries per set must not be negative")
	}
	if uint64(cfg.SubscriptionMaxQueriesPerSet) > uint64(protocol.MaxSubscribeMultiQueriesHard) {
		return Config{}, "", fmt.Errorf("subscription max queries per set %d exceeds decoder hard limit %d", cfg.SubscriptionMaxQueriesPerSet, protocol.MaxSubscribeMultiQueriesHard)
	}
	if cfg.SubscriptionMaxActiveSetsPerConnection < 0 {
		return Config{}, "", fmt.Errorf("subscription max active sets per connection must not be negative")
	}
	if cfg.SubscriptionMaxActiveSubscriptionsPerConnection < 0 {
		return Config{}, "", fmt.Errorf("subscription max active subscriptions per connection must not be negative")
	}
	if cfg.SubscriptionMaxMultiJoinRelations < 0 {
		return Config{}, "", fmt.Errorf("subscription max multi-join relations must not be negative")
	}
	if cfg.SubscriptionMaxMultiJoinRowsPerRelation < 0 {
		return Config{}, "", fmt.Errorf("subscription max multi-join rows per relation must not be negative")
	}
	if cfg.SubscriptionMaxMultiJoinWork < 0 {
		return Config{}, "", fmt.Errorf("subscription max multi-join work must not be negative")
	}
	if cfg.AuthMode != AuthModeDev && cfg.AuthMode != AuthModeStrict {
		return Config{}, "", fmt.Errorf("auth mode is invalid")
	}
	observability, err := normalizeObservabilityConfig(cfg.Observability)
	if err != nil {
		return Config{}, "", err
	}

	normalized := copyConfig(cfg)
	normalized.Observability = observability
	dataDir := strings.TrimSpace(cfg.DataDir)
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	if normalized.ExecutorQueueCapacity == 0 {
		normalized.ExecutorQueueCapacity = defaultExecutorQueueCapacity
	}
	if normalized.DurabilityQueueCapacity == 0 {
		normalized.DurabilityQueueCapacity = defaultDurabilityQueueCapacity
	}
	if normalized.OneOffQueryMaxRows == 0 {
		normalized.OneOffQueryMaxRows = protocol.DefaultSQLQueryMaxRows
	}
	if normalized.OneOffQueryMaxBytes == 0 {
		normalized.OneOffQueryMaxBytes = protocol.DefaultSQLQueryMaxBytes
	}
	if normalized.OneOffQueryMaxWork == 0 {
		normalized.OneOffQueryMaxWork = protocol.DefaultSQLQueryMaxWork
	}
	if normalized.ProcedureResultMaxBytes == 0 {
		normalized.ProcedureResultMaxBytes = defaultProcedureResultBytes
	}
	if normalized.SubscriptionInitialRowLimit == 0 {
		normalized.SubscriptionInitialRowLimit = defaultSubscriptionInitialRows
	}
	if normalized.SubscriptionSnapshotMaxBytes == 0 {
		normalized.SubscriptionSnapshotMaxBytes = defaultSubscriptionSnapshotBytes
	}
	if normalized.SubscriptionMaxQueriesPerSet == 0 {
		normalized.SubscriptionMaxQueriesPerSet = protocol.DefaultSubscriptionMaxQueriesPerSet
	}
	if normalized.SubscriptionMaxActiveSetsPerConnection == 0 {
		normalized.SubscriptionMaxActiveSetsPerConnection = defaultSubscriptionActiveSets
	}
	if normalized.SubscriptionMaxActiveSubscriptionsPerConnection == 0 {
		normalized.SubscriptionMaxActiveSubscriptionsPerConnection = defaultSubscriptionActiveSubscriptions
	}
	if normalized.SubscriptionMaxMultiJoinRelations == 0 {
		normalized.SubscriptionMaxMultiJoinRelations = subscription.DefaultMultiJoinMaxRelations
	}
	if normalized.SubscriptionMaxMultiJoinRowsPerRelation == 0 {
		normalized.SubscriptionMaxMultiJoinRowsPerRelation = subscription.DefaultMultiJoinMaxRowsPerRelation
	}
	if normalized.SubscriptionMaxMultiJoinWork == 0 {
		normalized.SubscriptionMaxMultiJoinWork = subscription.DefaultMultiJoinMaxWork
	}
	return normalized, dataDir, nil
}

func copyConfig(cfg Config) Config {
	out := cfg
	out.AuthSigningKey = append([]byte(nil), cfg.AuthSigningKey...)
	out.AuthVerificationKeys = copyAuthVerificationKeys(cfg.AuthVerificationKeys)
	out.AuthOIDCIssuers = copyAuthOIDCIssuers(cfg.AuthOIDCIssuers)
	out.AuthOIDCDiscoveryIssuers = copyAuthOIDCDiscoveryIssuers(cfg.AuthOIDCDiscoveryIssuers)
	out.AuthIssuers = append([]string(nil), cfg.AuthIssuers...)
	out.AuthAudiences = append([]string(nil), cfg.AuthAudiences...)
	out.AuthExtraClaims = append([]string(nil), cfg.AuthExtraClaims...)
	return out
}

func copyAuthVerificationKeys(in []AuthVerificationKey) []AuthVerificationKey {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	for i := range out {
		out[i].Key = slices.Clone(out[i].Key)
	}
	return out
}

func copyAuthOIDCIssuers(in []AuthOIDCIssuer) []AuthOIDCIssuer {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	for i := range out {
		out[i].Algorithms = slices.Clone(out[i].Algorithms)
	}
	return out
}

func copyAuthOIDCDiscoveryIssuers(in []AuthOIDCDiscoveryIssuer) []AuthOIDCDiscoveryIssuer {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	for i := range out {
		out[i].Algorithms = slices.Clone(out[i].Algorithms)
	}
	return out
}

// CheckDataDirCompatibility validates that mod can recover cfg.DataDir without
// starting runtime services or bootstrapping missing state. A missing or empty
// DataDir is compatible because Build can create a fresh runtime state there.
func CheckDataDirCompatibility(mod *Module, cfg Config) error {
	preview, err := previewRuntimeBuild(mod, cfg)
	if err != nil {
		return err
	}
	info, err := os.Stat(preview.dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect data dir %s: %w", preview.dataDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data dir %s is not a directory", preview.dataDir)
	}
	if err := validateDataDirMetadata(preview.dataDir, mod, preview.registry); err != nil {
		return fmt.Errorf("check data dir compatibility: %w", err)
	}

	_, _, _, recoveryReport, err := commitlog.OpenAndRecoverWithReport(preview.dataDir, preview.registry)
	if errors.Is(err, commitlog.ErrNoData) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check data dir compatibility: %w", err)
	}
	if _, err := logOnlyRecoverySchemaCompatibilityReport(preview.dataDir, preview.registry, recoveryReport); err != nil {
		return fmt.Errorf("check data dir compatibility: %w", err)
	}
	return nil
}

// CheckDataDirCompatibilityReport validates cfg.DataDir and returns a
// machine-readable hosted-app migration preflight report. It does not create or
// mutate the DataDir. Safe additive schema changes currently include
// schema-version-only drift, added tables, and appended non-unique/non-primary
// indexes; row-shape changes and destructive table/index changes remain
// blocked until app-owned migration hooks rewrite or validate persisted rows.
func CheckDataDirCompatibilityReport(mod *Module, cfg Config) (DataDirCompatibilityReport, error) {
	preview, err := previewRuntimeBuild(mod, cfg)
	if err != nil {
		return DataDirCompatibilityReport{}, err
	}
	report := DataDirCompatibilityReport{
		Compatible: true,
		Status:     DataDirCompatibilityFresh,
		DataDir:    preview.dataDir,
		Schema: schema.SchemaCompatibilityReport{
			Compatible:        true,
			Status:            schema.SchemaCompatibilityCompatible,
			RegisteredVersion: preview.registry.Version(),
		},
	}
	info, err := os.Stat(preview.dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return blockedDataDirCompatibilityReport(report, fmt.Errorf("inspect data dir %s: %w", preview.dataDir, err)), nil
	}
	if !info.IsDir() {
		return blockedDataDirCompatibilityReport(report, fmt.Errorf("data dir %s is not a directory", preview.dataDir)), nil
	}
	if err := validateDataDirMetadata(preview.dataDir, mod, preview.registry); err != nil {
		return blockedDataDirCompatibilityReport(report, fmt.Errorf("check data dir compatibility: %w", err)), nil
	}

	_, _, _, recoveryReport, err := commitlog.OpenAndRecoverWithReport(preview.dataDir, preview.registry)
	if errors.Is(err, commitlog.ErrNoData) {
		return report, nil
	}
	if err != nil {
		applySchemaMismatchReport(&report, err)
		return blockedDataDirCompatibilityReport(report, fmt.Errorf("check data dir compatibility: %w", err)), nil
	}
	if schemaReport, err := logOnlyRecoverySchemaCompatibilityReport(preview.dataDir, preview.registry, recoveryReport); err != nil {
		report.Schema = schemaReport
		return blockedDataDirCompatibilityReport(report, fmt.Errorf("check data dir compatibility: %w", err)), nil
	}
	report.Schema = recoveryReport.SchemaCompatibility
	if report.Schema.Status == "" {
		report.Schema = schema.SchemaCompatibilityReport{
			Compatible:        true,
			Status:            schema.SchemaCompatibilityCompatible,
			RegisteredVersion: preview.registry.Version(),
		}
	}
	report.Compatible = report.Schema.Compatible
	switch report.Schema.Status {
	case schema.SchemaCompatibilityAdditive:
		report.Status = DataDirCompatibilityAdditive
		report.RequiresBackup = true
	case schema.SchemaCompatibilityBlocked:
		report.Status = DataDirCompatibilityBlocked
		report.RequiresBackup = true
		report.RequiresOfflineHook = true
	default:
		report.Status = DataDirCompatibilityCompatible
	}
	return report, nil
}

func blockedDataDirCompatibilityReport(report DataDirCompatibilityReport, err error) DataDirCompatibilityReport {
	report.Compatible = false
	report.Status = DataDirCompatibilityBlocked
	report.RequiresBackup = true
	report.RequiresOfflineHook = true
	if err != nil {
		report.BlockingError = err.Error()
	}
	report.Schema.Compatible = false
	report.Schema.Status = schema.SchemaCompatibilityBlocked
	return report
}

func applySchemaMismatchReport(report *DataDirCompatibilityReport, err error) {
	if report == nil || err == nil {
		return
	}
	var schemaErr *commitlog.SchemaMismatchError
	if errors.As(err, &schemaErr) && schemaErr.Report.Status != "" {
		report.Schema = schemaErr.Report
	}
}

func logOnlyRecoverySchemaCompatibilityReport(dataDir string, reg schema.SchemaRegistry, recoveryReport commitlog.RecoveryReport) (schema.SchemaCompatibilityReport, error) {
	if !recoveryReport.HasDurableLog || recoveryReport.HasSelectedSnapshot {
		return schema.SchemaCompatibilityReport{}, nil
	}
	registeredVersion := uint32(0)
	if reg != nil {
		registeredVersion = reg.Version()
	}
	metadata, ok, err := readDataDirMetadata(dataDir)
	if err != nil {
		detail := fmt.Sprintf("data dir has durable commit log but no selected snapshot, and metadata could not be read: %v", err)
		return blockedLogOnlySchemaReport(registeredVersion, 0, detail), errors.New(detail)
	}
	if !ok {
		detail := "data dir has durable commit log but no selected snapshot or schema metadata; a selected snapshot is required before schema-version changes can be checked safely"
		return blockedLogOnlySchemaReport(registeredVersion, 0, detail), errors.New(detail)
	}
	if metadata.Module.SchemaVersion == registeredVersion {
		return schema.SchemaCompatibilityReport{}, nil
	}
	detail := fmt.Sprintf("data dir has durable commit log but no selected snapshot; metadata schema_version=%d, registered=%d; create or restore a snapshot before additive schema-version changes", metadata.Module.SchemaVersion, registeredVersion)
	return blockedLogOnlySchemaReport(registeredVersion, metadata.Module.SchemaVersion, detail), errors.New(detail)
}

func blockedLogOnlySchemaReport(registeredVersion, persistedVersion uint32, detail string) schema.SchemaCompatibilityReport {
	return schema.SchemaCompatibilityReport{
		Compatible:        false,
		Status:            schema.SchemaCompatibilityBlocked,
		RegisteredVersion: registeredVersion,
		SnapshotVersion:   persistedVersion,
		Issues: []schema.SchemaCompatibilityIssue{{
			Kind:   schema.SchemaCompatibilityIssueMissingRegistry,
			Detail: detail,
		}},
	}
}

func openOrBootstrapState(dataDir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, commitlog.RecoveryResumePlan, commitlog.RecoveryReport, schema.SchemaRegistry, error) {
	if err := os.MkdirAll(dataDir, dataDirMode); err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, commitlog.RecoveryReport{}, reg, fmt.Errorf("mkdir data dir: %w", err)
	}

	committed, recoveredTxID, plan, report, recoveryRegistry, err := commitlog.OpenAndRecoverWithRegistryReport(dataDir, reg)
	if err == nil {
		if _, err := logOnlyRecoverySchemaCompatibilityReport(dataDir, reg, report); err != nil {
			return nil, 0, commitlog.RecoveryResumePlan{}, report, recoveryRegistry, err
		}
		return committed, recoveredTxID, plan, report, recoveryRegistry, nil
	}
	if !errors.Is(err, commitlog.ErrNoData) {
		return nil, 0, commitlog.RecoveryResumePlan{}, report, recoveryRegistry, err
	}

	fresh := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		tableSchema, ok := reg.Table(tid)
		if !ok {
			return nil, 0, commitlog.RecoveryResumePlan{}, commitlog.RecoveryReport{}, reg, fmt.Errorf("registry missing table %d", tid)
		}
		fresh.RegisterTable(tid, store.NewTable(tableSchema))
	}
	if err := commitlog.NewSnapshotWriter(dataDir, reg).CreateSnapshot(fresh, 0); err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, commitlog.RecoveryReport{}, reg, fmt.Errorf("initial snapshot: %w", err)
	}
	committed, recoveredTxID, plan, _, recoveryRegistry, err = commitlog.OpenAndRecoverWithRegistryReport(dataDir, reg)
	if err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, commitlog.RecoveryReport{}, recoveryRegistry, err
	}
	report = commitlog.RecoveryReport{
		RecoveredTxID: recoveredTxID,
		ResumePlan:    plan,
	}
	return committed, recoveredTxID, plan, report, recoveryRegistry, nil
}

func buildExecutorReducerRegistry(reg schema.SchemaRegistry, declarations []ReducerDeclaration) (*executor.ReducerRegistry, error) {
	reducers := executor.NewReducerRegistry()
	permissionsByName := make(map[string]PermissionMetadata, len(declarations))
	for _, decl := range declarations {
		permissionsByName[decl.Name] = copyPermissionMetadata(decl.Permissions)
	}
	for _, name := range reg.Reducers() {
		handler, ok := reg.Reducer(name)
		if !ok {
			return nil, fmt.Errorf("schema registry missing reducer %q", name)
		}
		if err := reducers.Register(executor.RegisteredReducer{
			Name:                name,
			Handler:             handler,
			RequiredPermissions: copySlice(permissionsByName[name].Required),
		}); err != nil {
			return nil, err
		}
	}
	if handler := reg.OnConnect(); handler != nil {
		if err := reducers.Register(executor.RegisteredReducer{
			Name:      "OnConnect",
			Handler:   lifecycleReducerHandler(handler),
			Lifecycle: executor.LifecycleOnConnect,
		}); err != nil {
			return nil, err
		}
	}
	if handler := reg.OnDisconnect(); handler != nil {
		if err := reducers.Register(executor.RegisteredReducer{
			Name:      "OnDisconnect",
			Handler:   lifecycleReducerHandler(handler),
			Lifecycle: executor.LifecycleOnDisconnect,
		}); err != nil {
			return nil, err
		}
	}
	reducers.Freeze()
	return reducers, nil
}

func lifecycleReducerHandler(handler func(*schema.ReducerContext) error) types.ReducerHandler {
	return func(ctx *types.ReducerContext, _ []byte) ([]byte, error) {
		return nil, handler(ctx)
	}
}

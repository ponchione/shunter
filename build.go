package shunter

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

const (
	defaultDataDir                 = "./shunter-data"
	defaultExecutorQueueCapacity   = 256
	defaultDurabilityQueueCapacity = 256
)

type runtimeBuildPreview struct {
	normalized        Config
	dataDir           string
	schemaOpts        schema.EngineOptions
	registry          schema.SchemaRegistry
	visibilityFilters []VisibilityFilterDescription
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
	return normalized, dataDir, nil
}

func copyConfig(cfg Config) Config {
	out := cfg
	out.AuthSigningKey = append([]byte(nil), cfg.AuthSigningKey...)
	out.AuthIssuers = append([]string(nil), cfg.AuthIssuers...)
	out.AuthAudiences = append([]string(nil), cfg.AuthAudiences...)
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

	_, _, _, _, err = commitlog.OpenAndRecoverWithReport(preview.dataDir, preview.registry)
	if errors.Is(err, commitlog.ErrNoData) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check data dir compatibility: %w", err)
	}
	return nil
}

func openOrBootstrapState(dataDir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, commitlog.RecoveryResumePlan, commitlog.RecoveryReport, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, commitlog.RecoveryReport{}, fmt.Errorf("mkdir data dir: %w", err)
	}

	committed, recoveredTxID, plan, report, err := commitlog.OpenAndRecoverWithReport(dataDir, reg)
	if err == nil {
		return committed, recoveredTxID, plan, report, nil
	}
	if !errors.Is(err, commitlog.ErrNoData) {
		return nil, 0, commitlog.RecoveryResumePlan{}, report, err
	}

	fresh := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		tableSchema, ok := reg.Table(tid)
		if !ok {
			return nil, 0, commitlog.RecoveryResumePlan{}, commitlog.RecoveryReport{}, fmt.Errorf("registry missing table %d", tid)
		}
		fresh.RegisterTable(tid, store.NewTable(tableSchema))
	}
	if err := commitlog.NewSnapshotWriter(dataDir, reg).CreateSnapshot(fresh, 0); err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, commitlog.RecoveryReport{}, fmt.Errorf("initial snapshot: %w", err)
	}
	committed, recoveredTxID, plan, _, err = commitlog.OpenAndRecoverWithReport(dataDir, reg)
	if err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, commitlog.RecoveryReport{}, err
	}
	report = commitlog.RecoveryReport{
		RecoveredTxID: recoveredTxID,
		ResumePlan:    plan,
	}
	return committed, recoveredTxID, plan, report, nil
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
			RequiredPermissions: copyStringSlice(permissionsByName[name].Required),
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

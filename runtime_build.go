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

	normalized := copyConfig(cfg)
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
	out.AuthAudiences = append([]string(nil), cfg.AuthAudiences...)
	return out
}

func openOrBootstrapState(dataDir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, commitlog.RecoveryResumePlan, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, fmt.Errorf("mkdir data dir: %w", err)
	}

	committed, recoveredTxID, plan, err := commitlog.OpenAndRecoverDetailed(dataDir, reg)
	if err == nil {
		return committed, recoveredTxID, plan, nil
	}
	if !errors.Is(err, commitlog.ErrNoData) {
		return nil, 0, commitlog.RecoveryResumePlan{}, err
	}

	fresh := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		tableSchema, ok := reg.Table(tid)
		if !ok {
			return nil, 0, commitlog.RecoveryResumePlan{}, fmt.Errorf("registry missing table %d", tid)
		}
		fresh.RegisterTable(tid, store.NewTable(tableSchema))
	}
	if err := commitlog.NewSnapshotWriter(dataDir, reg).CreateSnapshot(fresh, 0); err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, fmt.Errorf("initial snapshot: %w", err)
	}
	return commitlog.OpenAndRecoverDetailed(dataDir, reg)
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

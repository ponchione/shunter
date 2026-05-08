package shunter

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// MigrationHook is an app-owned startup migration callback. The hook receives
// a callback-scoped transaction; Shunter commits and persists that transaction
// only if the hook returns nil. Hooks should be idempotent because a failed
// later startup step or process restart may run them again.
type MigrationHook func(context.Context, *MigrationContext) error

// MigrationContext exposes runtime-owned state to a MigrationHook.
type MigrationContext struct {
	moduleName    string
	moduleVersion string
	schema        schema.SchemaRegistry
	committedTxID types.TxID
	tx            *store.Transaction
}

// MigrationRunResult summarizes an explicit DataDir migration run.
type MigrationRunResult struct {
	DataDir       string
	RecoveredTxID types.TxID
	DurableTxID   types.TxID
	Hooks         []MigrationHookResult
}

// MigrationHookResult reports the outcome for one hook supplied to
// RunDataDirMigrations or one startup hook registered on Module.
type MigrationHookResult struct {
	// Index is the zero-based position of the hook in the supplied hook list.
	Index   int
	TxID    types.TxID
	Changed bool
}

// ModuleName returns the module name for the runtime being started.
func (c *MigrationContext) ModuleName() string {
	if c == nil {
		return ""
	}
	return c.moduleName
}

// ModuleVersion returns the app-owned module version string.
func (c *MigrationContext) ModuleVersion() string {
	if c == nil {
		return ""
	}
	return c.moduleVersion
}

// Schema returns the immutable schema registry for the runtime being started.
func (c *MigrationContext) Schema() schema.SchemaRegistry {
	if c == nil {
		return nil
	}
	return c.schema
}

// CommittedTxID returns the committed state horizon visible to this hook.
func (c *MigrationContext) CommittedTxID() types.TxID {
	if c == nil {
		return 0
	}
	return c.committedTxID
}

// Transaction returns the callback-scoped migration transaction. It must not be
// retained after the hook returns.
func (c *MigrationContext) Transaction() *store.Transaction {
	if c == nil {
		return nil
	}
	return c.tx
}

func copyMigrationHooks(in []MigrationHook) []MigrationHook {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
}

// RunDataDirMigrations runs app-owned migration hooks against cfg.DataDir
// without starting normal runtime services. It is intended for app-owned
// binaries that link the module directly; callers must stop any runtime that
// owns the DataDir before calling it.
func RunDataDirMigrations(ctx context.Context, mod *Module, cfg Config, hooks ...MigrationHook) (MigrationRunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return MigrationRunResult{}, err
	}
	preview, err := previewRuntimeBuild(mod, cfg)
	if err != nil {
		return MigrationRunResult{}, err
	}
	result := MigrationRunResult{DataDir: preview.dataDir}
	if len(hooks) == 0 {
		return result, nil
	}

	state, recoveredTxID, resumePlan, _, err := openOrBootstrapState(preview.dataDir, preview.registry)
	if err != nil {
		return MigrationRunResult{}, fmt.Errorf("run data dir migrations state: %w", err)
	}
	result.RecoveredTxID = recoveredTxID
	result.DurableTxID = recoveredTxID

	options := commitlog.DefaultCommitLogOptions()
	options.ChannelCapacity = preview.normalized.DurabilityQueueCapacity
	durability, err := commitlog.NewDurabilityWorkerWithResumePlan(preview.dataDir, resumePlan, options)
	if err != nil {
		return MigrationRunResult{}, fmt.Errorf("run data dir migrations durability: %w", err)
	}
	defer func() {
		if durability != nil {
			_, _ = durability.Close()
		}
	}()

	exec := migrationExecutor{
		moduleName:    mod.name,
		moduleVersion: mod.version,
		registry:      preview.registry,
		state:         state,
		durability:    durability,
		currentTxID:   recoveredTxID,
		durableTxID:   recoveredTxID,
	}
	results, err := exec.runHooks(ctx, hooks)
	if err != nil {
		_, closeErr := durability.Close()
		durability = nil
		return MigrationRunResult{}, errors.Join(err, closeErr)
	}
	finalDurableTxID, closeErr := durability.Close()
	durability = nil
	if closeErr != nil {
		return MigrationRunResult{}, closeErr
	}
	result.Hooks = results
	result.DurableTxID = types.TxID(finalDurableTxID)
	if result.DurableTxID == 0 {
		result.DurableTxID = exec.durableTxID
	}
	return result, nil
}

// RunModuleDataDirMigrations runs hooks registered on mod with
// Module.MigrationHook against cfg.DataDir without starting normal runtime
// services. It is a convenience wrapper for app-owned offline migration
// binaries that want to reuse the same hook declarations as Runtime.Start.
func RunModuleDataDirMigrations(ctx context.Context, mod *Module, cfg Config) (MigrationRunResult, error) {
	if mod == nil {
		return RunDataDirMigrations(ctx, mod, cfg)
	}
	hooks := copyMigrationHooks(mod.migrationHooks)
	return RunDataDirMigrations(ctx, mod, cfg, hooks...)
}

func (r *Runtime) runMigrationHooks(ctx context.Context, durability *commitlog.DurabilityWorker) error {
	if len(r.module.migrationHooks) == 0 {
		return nil
	}
	if durability == nil {
		return fmt.Errorf("migration hooks require durability worker")
	}
	exec := migrationExecutor{
		moduleName:    r.module.name,
		moduleVersion: r.module.version,
		registry:      r.registry,
		state:         r.state,
		durability:    durability,
		currentTxID:   r.recoveredTxID,
		durableTxID:   r.durableTxID,
	}
	if _, err := exec.runHooks(ctx, r.module.migrationHooks); err != nil {
		return err
	}
	r.recoveredTxID = exec.currentTxID
	r.durableTxID = exec.durableTxID
	return nil
}

type migrationExecutor struct {
	moduleName    string
	moduleVersion string
	registry      schema.SchemaRegistry
	state         *store.CommittedState
	durability    *commitlog.DurabilityWorker
	currentTxID   types.TxID
	durableTxID   types.TxID
}

func (e *migrationExecutor) runHooks(ctx context.Context, hooks []MigrationHook) ([]MigrationHookResult, error) {
	var results []MigrationHookResult
	for i, hook := range hooks {
		if hook == nil {
			continue
		}
		result, err := e.runHook(ctx, i, hook)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (e *migrationExecutor) runHook(ctx context.Context, hookIndex int, hook MigrationHook) (MigrationHookResult, error) {
	result := MigrationHookResult{Index: hookIndex}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	tx := store.NewTransaction(e.state, e.registry)
	mctx := &MigrationContext{
		moduleName:    e.moduleName,
		moduleVersion: e.moduleVersion,
		schema:        e.registry,
		committedTxID: e.currentTxID,
		tx:            tx,
	}
	if err := hook(ctx, mctx); err != nil {
		store.Rollback(tx)
		return result, fmt.Errorf("migration hook %d: %w", hookIndex+1, err)
	}
	if err := ctx.Err(); err != nil {
		store.Rollback(tx)
		return result, err
	}
	if migrationTransactionEmpty(tx) {
		store.Rollback(tx)
		return result, nil
	}
	txID, err := nextMigrationTxID(e.currentTxID)
	if err != nil {
		store.Rollback(tx)
		return result, fmt.Errorf("migration hook %d: %w", hookIndex+1, err)
	}
	tx.Seal()
	changeset, err := store.Commit(e.state, tx)
	if err != nil {
		store.Rollback(tx)
		return result, fmt.Errorf("migration hook %d commit: %w", hookIndex+1, err)
	}
	if changeset.IsEmpty() {
		return result, nil
	}
	changeset.TxID = txID
	e.state.SetCommittedTxID(txID)
	e.state.RecordMemoryUsage()
	if err := persistMigrationChangeset(ctx, e.durability, txID, changeset); err != nil {
		return result, fmt.Errorf("migration hook %d durability: %w", hookIndex+1, err)
	}
	e.currentTxID = txID
	e.durableTxID = txID
	result.TxID = txID
	result.Changed = true
	return result, nil
}

func migrationTransactionEmpty(tx *store.Transaction) bool {
	txState := tx.TxState()
	if txState == nil {
		return false
	}
	for _, rows := range txState.AllInserts() {
		if len(rows) != 0 {
			return false
		}
	}
	for _, rows := range txState.AllDeletes() {
		if len(rows) != 0 {
			return false
		}
	}
	return true
}

func nextMigrationTxID(current types.TxID) (types.TxID, error) {
	if current == types.TxID(^uint64(0)) {
		return 0, fmt.Errorf("migration tx id exhausted")
	}
	return current + 1, nil
}

func persistMigrationChangeset(ctx context.Context, durability *commitlog.DurabilityWorker, txID types.TxID, changeset *store.Changeset) (err error) {
	if err := durability.FatalError(); err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("enqueue committed migration tx %d: %v", txID, r)
		}
	}()
	durability.EnqueueCommitted(uint64(txID), changeset)
	wait := durability.WaitUntilDurable(txID)
	if wait == nil {
		return nil
	}
	select {
	case durable, ok := <-wait:
		if !ok {
			if err := durability.FatalError(); err != nil {
				return err
			}
			return fmt.Errorf("durability worker closed before migration tx %d became durable", txID)
		}
		if durable != txID {
			return fmt.Errorf("durability confirmed tx %d while waiting for migration tx %d", durable, txID)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return durability.FatalError()
}

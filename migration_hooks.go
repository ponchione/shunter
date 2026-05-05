package shunter

import (
	"context"
	"fmt"

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
	out := make([]MigrationHook, len(in))
	copy(out, in)
	return out
}

func (r *Runtime) runMigrationHooks(ctx context.Context, durability *commitlog.DurabilityWorker) error {
	if len(r.module.migrationHooks) == 0 {
		return nil
	}
	if durability == nil {
		return fmt.Errorf("migration hooks require durability worker")
	}
	for i, hook := range r.module.migrationHooks {
		if hook == nil {
			continue
		}
		if err := r.runMigrationHook(ctx, durability, i, hook); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) runMigrationHook(ctx context.Context, durability *commitlog.DurabilityWorker, hookIndex int, hook MigrationHook) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx := store.NewTransaction(r.state, r.registry)
	mctx := &MigrationContext{
		moduleName:    r.module.name,
		moduleVersion: r.module.version,
		schema:        r.registry,
		committedTxID: r.recoveredTxID,
		tx:            tx,
	}
	if err := hook(ctx, mctx); err != nil {
		store.Rollback(tx)
		return fmt.Errorf("migration hook %d: %w", hookIndex+1, err)
	}
	if err := ctx.Err(); err != nil {
		store.Rollback(tx)
		return err
	}
	if migrationTransactionEmpty(tx) {
		store.Rollback(tx)
		return nil
	}
	txID, err := nextMigrationTxID(r.recoveredTxID)
	if err != nil {
		store.Rollback(tx)
		return fmt.Errorf("migration hook %d: %w", hookIndex+1, err)
	}
	tx.Seal()
	changeset, err := store.Commit(r.state, tx)
	if err != nil {
		store.Rollback(tx)
		return fmt.Errorf("migration hook %d commit: %w", hookIndex+1, err)
	}
	if changeset.IsEmpty() {
		return nil
	}
	changeset.TxID = txID
	r.state.SetCommittedTxID(txID)
	r.state.RecordMemoryUsage()
	if err := persistMigrationChangeset(ctx, durability, txID, changeset); err != nil {
		return fmt.Errorf("migration hook %d durability: %w", hookIndex+1, err)
	}
	r.recoveredTxID = txID
	r.durableTxID = txID
	return nil
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

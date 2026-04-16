package executor

import (
	"bytes"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// handleOnConnect runs the OnConnect pipeline (SPEC-003 §10.3, Story 7.2):
//
//  1. begin transaction
//  2. insert the sys_clients row
//  3. run the OnConnect lifecycle reducer if registered
//  4. on reducer error/panic: roll back the entire transaction, reject
//  5. on success: commit + post-commit pipeline
//
// Connection is rejected (status != StatusCommitted) on any failure.
func (e *Executor) handleOnConnect(cmd OnConnectCmd) {
	sysID, ok := e.sysClientsTableID()
	if !ok {
		respondLifecycle(cmd.ResponseCh, StatusFailedInternal, 0, fmt.Errorf("sys_clients table missing"))
		return
	}

	tx := store.NewTransaction(e.committed, e.schemaReg)
	row := types.ProductValue{
		types.NewBytes(cmd.ConnID[:]),
		types.NewBytes(cmd.Identity[:]),
		types.NewInt64(time.Now().UnixNano()),
	}
	if _, err := tx.Insert(sysID, row); err != nil {
		store.Rollback(tx)
		respondLifecycle(cmd.ResponseCh, StatusFailedInternal, 0, fmt.Errorf("sys_clients insert: %w", err))
		return
	}

	if rr, hasReducer := e.registry.LookupLifecycle(LifecycleOnConnect); hasReducer {
		status, err := e.runLifecycleReducer(rr, tx, cmd.ConnID, cmd.Identity)
		if status != StatusCommitted {
			store.Rollback(tx)
			respondLifecycle(cmd.ResponseCh, status, 0, err)
			return
		}
	}

	changeset, err := store.Commit(e.committed, tx)
	if err != nil {
		store.Rollback(tx)
		respondLifecycle(cmd.ResponseCh, StatusFailedInternal, 0, fmt.Errorf("commit: %w", err))
		return
	}
	txID := types.TxID(e.nextTxID)
	e.nextTxID++
	changeset.TxID = txID

	e.postCommit(txID, changeset, nil, cmd.ResponseCh)
}

// handleOnDisconnect runs the OnDisconnect pipeline (SPEC-003 §10.4, Story 7.3):
//
//  1. begin transaction
//  2. run the OnDisconnect lifecycle reducer if registered
//  3. on reducer error/panic: roll back reducer writes, log, and run a
//     separate cleanup transaction that only deletes the sys_clients row
//  4. otherwise: delete the sys_clients row in the same transaction
//  5. commit + post-commit pipeline
//
// The sys_clients row is always removed (disconnect cannot be vetoed).
func (e *Executor) handleOnDisconnect(cmd OnDisconnectCmd) {
	sysID, ok := e.sysClientsTableID()
	if !ok {
		respondLifecycle(cmd.ResponseCh, StatusFailedInternal, 0, fmt.Errorf("sys_clients table missing"))
		return
	}

	tx := store.NewTransaction(e.committed, e.schemaReg)
	reducerStatus := StatusCommitted
	var reducerErr error
	if rr, hasReducer := e.registry.LookupLifecycle(LifecycleOnDisconnect); hasReducer {
		reducerStatus, reducerErr = e.runLifecycleReducer(rr, tx, cmd.ConnID, cmd.Identity)
	}

	if reducerStatus != StatusCommitted {
		log.Printf("executor: OnDisconnect reducer failed for conn=%x: %v", cmd.ConnID[:], reducerErr)
		store.Rollback(tx)
		// Fresh cleanup transaction that only removes the sys_clients row.
		tx = store.NewTransaction(e.committed, e.schemaReg)
	}

	if err := deleteSysClientsRow(tx, sysID, cmd.ConnID); err != nil {
		store.Rollback(tx)
		respondLifecycle(cmd.ResponseCh, StatusFailedInternal, 0, fmt.Errorf("sys_clients delete: %w", err))
		return
	}

	changeset, err := store.Commit(e.committed, tx)
	if err != nil {
		store.Rollback(tx)
		respondLifecycle(cmd.ResponseCh, StatusFailedInternal, 0, fmt.Errorf("commit: %w", err))
		return
	}
	txID := types.TxID(e.nextTxID)
	e.nextTxID++
	changeset.TxID = txID

	// Even when the reducer failed, the cleanup commit still runs the
	// post-commit pipeline so subscribers see the sys_clients delete.
	e.postCommit(txID, changeset, nil, cmd.ResponseCh)
}

// runLifecycleReducer invokes a lifecycle reducer with panic recovery and
// returns the outcome status plus the underlying error (if any). The caller
// decides how to integrate the result into the surrounding pipeline.
func (e *Executor) runLifecycleReducer(
	rr *RegisteredReducer,
	tx *store.Transaction,
	conn types.ConnectionID,
	identity types.Identity,
) (ReducerStatus, error) {
	caller := types.CallerContext{
		Identity:     identity,
		ConnectionID: conn,
		Timestamp:    time.Now().UTC(),
	}
	rctx := &types.ReducerContext{
		ReducerName: rr.Name,
		Caller:      caller,
		DB:          &reducerDBAdapter{tx: tx},
		Scheduler:   e.newSchedulerHandle(tx),
	}

	var reducerErr error
	var panicked any
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = r
				log.Printf("executor: lifecycle reducer %q panic: %v\n%s", rr.Name, r, debug.Stack())
			}
		}()
		_, reducerErr = rr.Handler(rctx, nil)
	}()

	switch {
	case panicked != nil:
		return StatusFailedPanic, fmt.Errorf("%v: %w", panicked, ErrReducerPanic)
	case reducerErr != nil:
		return StatusFailedUser, reducerErr
	default:
		return StatusCommitted, nil
	}
}

// deleteSysClientsRow removes the row for this connection if present. It is
// not an error for the row to be absent — the disconnect path must tolerate
// the case where OnConnect never successfully inserted (e.g. auth failure
// followed by a belt-and-suspenders disconnect from the protocol layer).
func deleteSysClientsRow(tx *store.Transaction, sysID schema.TableID, conn types.ConnectionID) error {
	var targetRowID types.RowID
	found := false
	for rowID, row := range tx.ScanTable(sysID) {
		if int(SysClientsColConnectionID) >= len(row) {
			continue
		}
		if !bytes.Equal(row[SysClientsColConnectionID].AsBytes(), conn[:]) {
			continue
		}
		targetRowID = rowID
		found = true
		break
	}
	if found {
		return tx.Delete(sysID, targetRowID)
	}
	return nil
}

// sysClientsTableID resolves the sys_clients TableID via the executor's
// schema registry. It is resolved per-call rather than cached because the
// executor may be constructed without a call through schema.Build in unit
// tests; a false return indicates a harness error.
func (e *Executor) sysClientsTableID() (schema.TableID, bool) {
	ts, ok := SysClientsTable(e.schemaReg)
	if !ok {
		return 0, false
	}
	return ts.ID, true
}

// respondLifecycle delivers a lifecycle-command response on the optional
// ResponseCh. Lifecycle commands may be fire-and-forget (nil channel).
func respondLifecycle(ch chan<- ReducerResponse, status ReducerStatus, txID types.TxID, err error) {
	if ch == nil {
		return
	}
	ch <- ReducerResponse{Status: status, Error: err, TxID: txID}
}

# TD-013: Rollback Guard — Make Rolled-Back Transactions Unusable

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After `Rollback(tx)`, all subsequent mutation (Insert/Delete/Update) and commit operations return `ErrTransactionRolledBack`. Read operations (GetRow/ScanTable) return zero values. Transaction state is nil'd for defense-in-depth.

**Architecture:** Add `ErrTransactionRolledBack` sentinel, a `checkUsable()` guard method on `Transaction`, call it at entry of every Transaction method + `Commit()`. `Rollback()` also nils `tx.tx` so unguarded access panics.

**Tech Stack:** Go, existing store package error conventions

---

## File Map

- Modify: `store/errors.go` — add `ErrTransactionRolledBack`
- Modify: `store/transaction.go` — add `checkUsable()`, guard all methods
- Modify: `store/commit.go` — guard `Commit()`, nil tx state in `Rollback()`
- Modify: `store/audit_regression_test.go` — regression tests for post-rollback usage
- Modify: `TECH-DEBT.md` — mark TD-013 resolved

---

### Task 1: Write failing tests for post-rollback operations

**Files:**
- Modify: `store/audit_regression_test.go`

Tests: post-rollback Insert, Delete, Update, Commit all return `ErrTransactionRolledBack`. Post-rollback GetRow returns nil/false. Post-rollback ScanTable yields nothing.

### Task 2: Add sentinel error + guard method

**Files:**
- Modify: `store/errors.go`
- Modify: `store/transaction.go`

Add `ErrTransactionRolledBack` to error var block. Add `checkUsable()` returning that error when `rolledBack` is true.

### Task 3: Guard all Transaction methods + Commit/Rollback

**Files:**
- Modify: `store/transaction.go` — guard Insert, Delete, Update, GetRow, ScanTable
- Modify: `store/commit.go` — guard Commit, nil tx state in Rollback

### Task 4: Run full suite + mark TD-013 resolved + commit

Run all tests, update TECH-DEBT.md, commit.

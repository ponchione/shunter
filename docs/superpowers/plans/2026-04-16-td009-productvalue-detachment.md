# TD-009: ProductValue Insert/Read Detachment Fix

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detach stored rows from caller-owned `ProductValue` slices so post-insert and post-read mutation cannot corrupt table or transaction state.

**Architecture:** `ProductValue.Copy()` already exists in `types/product_value.go`. The fix adds `.Copy()` calls at two storage boundaries: `Table.InsertRow` (committed state) and `TxState.AddInsert` (transaction buffer). Read paths (`GetRow`, `DeleteRow`, `Scan`) also return copies to prevent callers from mutating stored data through returned references.

**Tech Stack:** Go, existing `types.ProductValue.Copy()`

---

## File Map

- Modify: `store/table.go` — copy on insert, copy on read/delete/scan
- Modify: `store/tx_state.go` — copy on AddInsert
- Modify: `store/audit_regression_test.go` — add detachment regression tests
- Modify: `TECH-DEBT.md` — mark TD-009 resolved

---

### Task 1: Write failing tests for insert-side mutation

**Files:**
- Modify: `store/audit_regression_test.go`

- [ ] **Step 1: Write failing test — table insert detachment**

```go
func TestTableInsertDetachesFromCaller(t *testing.T) {
	tbl := NewTable(pkSchema())
	id := tbl.AllocRowID()
	row := mkRow(1, "alice")
	if err := tbl.InsertRow(id, row); err != nil {
		t.Fatal(err)
	}

	// Mutate caller's slice after insert.
	row[1] = types.NewString("mutated")

	got, ok := tbl.GetRow(id)
	if !ok {
		t.Fatal("row should exist")
	}
	if got[1].AsString() != "alice" {
		t.Fatalf("stored row mutated by caller: got %q, want %q", got[1].AsString(), "alice")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `rtk go test ./store -run TestTableInsertDetachesFromCaller -v`
Expected: FAIL — `stored row mutated by caller: got "mutated", want "alice"`

---

### Task 2: Fix insert-side detachment in Table.InsertRow

**Files:**
- Modify: `store/table.go:72`

- [ ] **Step 3: Copy row before storing**

In `store/table.go`, change line 72 from:
```go
	t.rows[id] = row
```
to:
```go
	t.rows[id] = row.Copy()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `rtk go test ./store -run TestTableInsertDetachesFromCaller -v`
Expected: PASS

---

### Task 3: Write failing test for read-side mutation via GetRow

**Files:**
- Modify: `store/audit_regression_test.go`

- [ ] **Step 5: Write failing test — GetRow returns detached copy**

```go
func TestTableGetRowReturnsDetachedCopy(t *testing.T) {
	tbl := NewTable(pkSchema())
	id := tbl.AllocRowID()
	if err := tbl.InsertRow(id, mkRow(1, "alice")); err != nil {
		t.Fatal(err)
	}

	// Mutate row returned by GetRow.
	got, _ := tbl.GetRow(id)
	got[1] = types.NewString("mutated-via-getrow")

	// Subsequent read should be unaffected.
	got2, _ := tbl.GetRow(id)
	if got2[1].AsString() != "alice" {
		t.Fatalf("stored row mutated via GetRow: got %q, want %q", got2[1].AsString(), "alice")
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `rtk go test ./store -run TestTableGetRowReturnsDetachedCopy -v`
Expected: FAIL — `stored row mutated via GetRow: got "mutated-via-getrow", want "alice"`

---

### Task 4: Fix read-side detachment in GetRow, DeleteRow, Scan

**Files:**
- Modify: `store/table.go:103-117`

- [ ] **Step 7: Copy in GetRow**

Change `GetRow` from:
```go
func (t *Table) GetRow(id types.RowID) (types.ProductValue, bool) {
	row, ok := t.rows[id]
	return row, ok
}
```
to:
```go
func (t *Table) GetRow(id types.RowID) (types.ProductValue, bool) {
	row, ok := t.rows[id]
	if !ok {
		return nil, false
	}
	return row.Copy(), true
}
```

- [ ] **Step 8: Copy in DeleteRow return**

In `DeleteRow`, change line 98 from:
```go
	return row, true
```
to:
```go
	return row.Copy(), true
```

Note: the row is removed from the map, but the changeset in `Commit` holds the return value. Copying prevents aliasing between the changeset and any caller that also held the internal reference.

- [ ] **Step 9: Copy in Scan yield**

Change `Scan` from:
```go
func (t *Table) Scan() iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for id, row := range t.rows {
			if !yield(id, row) {
				return
			}
		}
	}
}
```
to:
```go
func (t *Table) Scan() iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for id, row := range t.rows {
			if !yield(id, row.Copy()) {
				return
			}
		}
	}
}
```

- [ ] **Step 10: Run GetRow detachment test**

Run: `rtk go test ./store -run TestTableGetRowReturnsDetachedCopy -v`
Expected: PASS

---

### Task 5: Fix TxState.AddInsert detachment

**Files:**
- Modify: `store/tx_state.go:29`
- Modify: `store/audit_regression_test.go`

- [ ] **Step 11: Write failing test — tx insert detachment**

```go
func TestTxStateAddInsertDetachesFromCaller(t *testing.T) {
	tx := NewTxState()
	row := mkRow(1, "alice")
	tx.AddInsert(0, 1, row)

	// Mutate caller's slice.
	row[1] = types.NewString("mutated")

	stored := tx.Inserts(0)[1]
	if stored[1].AsString() != "alice" {
		t.Fatalf("tx insert mutated by caller: got %q, want %q", stored[1].AsString(), "alice")
	}
}
```

- [ ] **Step 12: Run test to verify it fails**

Run: `rtk go test ./store -run TestTxStateAddInsertDetachesFromCaller -v`
Expected: FAIL

- [ ] **Step 13: Copy in AddInsert**

In `store/tx_state.go`, change line 29 from:
```go
	m[id] = row
```
to:
```go
	m[id] = row.Copy()
```

- [ ] **Step 14: Run test to verify it passes**

Run: `rtk go test ./store -run TestTxStateAddInsertDetachesFromCaller -v`
Expected: PASS

---

### Task 6: Run full test suite and commit

- [ ] **Step 15: Run all store tests**

Run: `rtk go test ./store -v`
Expected: All pass

- [ ] **Step 16: Run broad test suite**

Run: `rtk go test ./types ./bsatn ./schema ./store ./executor ./commitlog`
Expected: All pass

- [ ] **Step 17: Mark TD-009 resolved in TECH-DEBT.md**

Change `Status: open` to `Status: resolved` for TD-009 (around line 1262).

- [ ] **Step 18: Commit**

```bash
rtk git add store/table.go store/tx_state.go store/audit_regression_test.go TECH-DEBT.md
rtk git commit -m "fix(store): detach ProductValue on insert/read to prevent caller mutation (TD-009)"
```

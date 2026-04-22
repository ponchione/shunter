package store

import (
	"testing"

	"github.com/ponchione/shunter/schema"
)

// TestStoreErrColumnNotFoundIsSchemaSentinel pins that store.ErrColumnNotFound
// is the same value as schema.ErrColumnNotFound (SPEC-001 §9 re-export of
// SPEC-006 §13), so errors.Is across the schema/store boundary matches.
func TestStoreErrColumnNotFoundIsSchemaSentinel(t *testing.T) {
	if ErrColumnNotFound != schema.ErrColumnNotFound {
		t.Fatal("store.ErrColumnNotFound must equal schema.ErrColumnNotFound")
	}
}

package store

import (
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Changeset captures the net-effect mutations of a committed transaction.
type Changeset struct {
	TxID   types.TxID
	Tables map[schema.TableID]*TableChangeset
}

// TableChangeset holds per-table net-effect inserts and deletes.
type TableChangeset struct {
	TableID   schema.TableID
	TableName string
	Inserts   []types.ProductValue
	Deletes   []types.ProductValue
}

// IsEmpty returns true if the changeset has no mutations.
func (cs *Changeset) IsEmpty() bool {
	for _, tc := range cs.Tables {
		if len(tc.Inserts) > 0 || len(tc.Deletes) > 0 {
			return false
		}
	}
	return true
}

// TableChanges returns the changeset for a specific table, or nil.
func (cs *Changeset) TableChanges(id schema.TableID) *TableChangeset {
	return cs.Tables[id]
}

package store

import (
	"iter"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// Index wraps a BTreeIndex with schema metadata.
type Index struct {
	schema *schema.IndexSchema
	btree  *BTreeIndex
}

// NewIndex creates an Index from its schema.
func NewIndex(is *schema.IndexSchema) *Index {
	return &Index{
		schema: is,
		btree:  NewBTreeIndex(),
	}
}

// ExtractKey builds an IndexKey from a row using the schema's column indices.
func (idx *Index) ExtractKey(row types.ProductValue) IndexKey {
	return ExtractKey(row, idx.schema.Columns)
}

// Seek returns RowIDs matching the exact key.
func (idx *Index) Seek(key IndexKey) []types.RowID {
	return idx.btree.Seek(key)
}

// SeekBounds returns RowIDs for keys between low and high per Bound
// semantics (SPEC-001 §4.4 / §4.6). Thin wrapper over BTreeIndex.SeekBounds.
func (idx *Index) SeekBounds(low, high Bound) iter.Seq[types.RowID] {
	return idx.btree.SeekBounds(low, high)
}

// BTree returns the underlying BTreeIndex for direct range access.
func (idx *Index) BTree() *BTreeIndex { return idx.btree }

// Schema returns the index's schema.
func (idx *Index) Schema() *schema.IndexSchema { return idx.schema }

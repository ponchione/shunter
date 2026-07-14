package executor

import (
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

const systemTablePrimaryKeyIndexID schema.IndexID = 0

func systemTableRowByPrimaryKey(
	tx *store.Transaction,
	tableID schema.TableID,
	key types.Value,
) (types.RowID, types.ProductValue, bool) {
	for rowID, row := range tx.SeekIndex(tableID, systemTablePrimaryKeyIndexID, key) {
		return rowID, row, true
	}
	return 0, nil, false
}

func deleteSystemTableRowByPrimaryKey(
	tx *store.Transaction,
	tableID schema.TableID,
	key types.Value,
) (bool, error) {
	rowID, _, found := systemTableRowByPrimaryKey(tx, tableID, key)
	if !found {
		return false, nil
	}
	if err := tx.Delete(tableID, rowID); err != nil {
		return false, err
	}
	return true, nil
}

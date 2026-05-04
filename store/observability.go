package store

const (
	StoreReadKindTableScan  = "table_scan"
	StoreReadKindIndexScan  = "index_scan"
	StoreReadKindIndexSeek  = "index_seek"
	StoreReadKindIndexRange = "index_range"
)

// Observer receives runtime-scoped store observations. Nil means no-op.
type Observer interface {
	LogStoreSnapshotLeaked(reason string)
	RecordStoreReadRows(kind string, rows uint64)
}

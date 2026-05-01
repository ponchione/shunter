package store

// Observer receives runtime-scoped store observations. Nil means no-op.
type Observer interface {
	LogStoreSnapshotLeaked(reason string)
}

package store

func mapContainsKey[K comparable, V any](m map[K]V, k K) bool {
	if m == nil {
		return false
	}
	_, ok := m[k]
	return ok
}

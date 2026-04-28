package subscription

func mapKeys[K comparable, V any](m map[K]V) []K {
	if len(m) == 0 {
		return []K{}
	}
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

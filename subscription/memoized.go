package subscription

// EncodingMemo is a per-delivery-batch cache for lazily memoized wire-format
// artifacts. Subscription owns the lifecycle; protocol adapters own the stored
// values and keys.
type EncodingMemo struct {
	values map[string]any
}

// NewEncodingMemo creates an empty per-batch encoding cache.
func NewEncodingMemo() *EncodingMemo {
	return &EncodingMemo{values: make(map[string]any)}
}

// Get returns a cached value for key.
func (m *EncodingMemo) Get(key string) (any, bool) {
	if m == nil || m.values == nil {
		return nil, false
	}
	v, ok := m.values[key]
	return v, ok
}

// Put stores a cached value for key.
func (m *EncodingMemo) Put(key string, value any) {
	if m == nil {
		return
	}
	if m.values == nil {
		m.values = make(map[string]any)
	}
	m.values[key] = value
}

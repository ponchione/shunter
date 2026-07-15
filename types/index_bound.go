package types

// IndexBound represents one endpoint of an indexed range read.
//
// Values holds a tuple endpoint for a composite index. When Values is empty,
// Value is the single endpoint part. Value is retained so existing scalar
// bounds and struct literals continue to work. Unbounded ignores both fields.
type IndexBound struct {
	Values    ProductValue
	Value     Value
	Inclusive bool
	Unbounded bool
}

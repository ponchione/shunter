package types

// IndexBound represents one endpoint of an indexed range read.
type IndexBound struct {
	Value     Value
	Inclusive bool
	Unbounded bool
}

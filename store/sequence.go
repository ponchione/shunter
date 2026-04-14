package store

import "sync"

// Sequence provides monotonically increasing auto-increment values.
type Sequence struct {
	mu   sync.Mutex
	next uint64
}

// NewSequence creates a sequence starting at 1.
func NewSequence() *Sequence {
	return &Sequence{next: 1}
}

// Next returns the current value and increments.
func (s *Sequence) Next() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.next
	s.next++
	return v
}

// Peek returns the current value without incrementing.
func (s *Sequence) Peek() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next
}

// Reset sets the next value (for recovery).
func (s *Sequence) Reset(v uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next = v
}

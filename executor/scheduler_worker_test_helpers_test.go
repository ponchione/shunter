package executor

import (
	"context"

	"github.com/ponchione/shunter/types"
)

// scan keeps tests focused on one synchronous worker iteration.
func (s *Scheduler) scan() {
	_, _ = s.scanAndTrackMaxWithContext(context.Background())
}

func (s *Scheduler) isInFlight(row types.ProductValue) bool {
	return s.isInFlightKey(scheduledFireKeyForRow(row))
}

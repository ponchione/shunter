package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestCallerOutcomeShape pins the Phase 1.5 forward-declared shape that
// the protocol-side adapter consumes when assembling the heavy
// `TransactionUpdate` envelope. See `docs/parity-phase1.5-outcome-model.md`.
func TestCallerOutcomeShape(t *testing.T) {
	outcome := CallerOutcome{
		Kind:      CallerOutcomeFailed,
		RequestID: 7,
		Error:     "boom",
	}
	if outcome.Kind != CallerOutcomeFailed || outcome.RequestID != 7 || outcome.Error != "boom" {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
}

// TestPostCommitMetaCarriesCallerOutcome pins that the executor's
// post-commit handoff struct still carries `CallerOutcome` after the
// Phase 1.5 rename from `CallerResult`.
func TestPostCommitMetaCarriesCallerOutcome(t *testing.T) {
	caller := types.ConnectionID{1}
	meta := PostCommitMeta{
		CallerConnID:  &caller,
		CallerOutcome: &CallerOutcome{Kind: CallerOutcomeCommitted, RequestID: 1},
	}
	if meta.CallerOutcome == nil || meta.CallerOutcome.RequestID != 1 {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

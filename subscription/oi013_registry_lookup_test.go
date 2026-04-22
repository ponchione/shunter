package subscription_test

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
)

// Compile-time pin: the canonical schema registry should satisfy the
// subscription-side SchemaLookup contract directly, with no embedder adapter.
var _ subscription.SchemaLookup = schema.SchemaRegistry(nil)

func TestSchemaRegistrySatisfiesSubscriptionSchemaLookup(t *testing.T) {}

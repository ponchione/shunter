package subscription

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/schema"
)

// TestSubscriptionIndexResolverIsSchemaAlias pins that subscription.IndexResolver
// is a type alias of schema.IndexResolver, so there is only one canonical
// declaration (SPEC-006 §7). A concrete resolver implementation satisfies
// both names interchangeably.
func TestSubscriptionIndexResolverIsSchemaAlias(t *testing.T) {
	var impl schema.IndexResolver = stubResolver{}
	var _ IndexResolver = impl
	var _ schema.IndexResolver = IndexResolver(impl)
}

type stubResolver struct{}

func (stubResolver) IndexIDForColumn(schema.TableID, ColID) (schema.IndexID, bool) {
	return 0, false
}

// TestSubscriptionErrColumnNotFoundIsSchemaSentinel pins that
// subscription.ErrColumnNotFound is the same value as schema.ErrColumnNotFound
// so errors.Is matches across package boundaries (SPEC-006 §13).
func TestSubscriptionErrColumnNotFoundIsSchemaSentinel(t *testing.T) {
	if ErrColumnNotFound != schema.ErrColumnNotFound {
		t.Fatal("subscription.ErrColumnNotFound must equal schema.ErrColumnNotFound")
	}
	wrapped := testWrap(ErrColumnNotFound)
	if !errors.Is(wrapped, schema.ErrColumnNotFound) {
		t.Fatal("errors.Is(wrapped, schema.ErrColumnNotFound) must be true")
	}
}

type wrapError struct{ inner error }

func (w wrapError) Error() string { return "wrap: " + w.inner.Error() }
func (w wrapError) Unwrap() error { return w.inner }

func testWrap(err error) error { return wrapError{inner: err} }

package queryplan

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/query/sql"
)

func TestBuildCapturesDerivedMetadata(t *testing.T) {
	plan, err := Build("SELECT * FROM users WHERE TRUE AND id = :sender ORDER BY id LIMIT 2 OFFSET 1", Options{
		AllowLimit:   true,
		AllowOrderBy: true,
		AllowOffset:  true,
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if plan.Statement.Table != "users" {
		t.Fatalf("Statement.Table = %q, want users", plan.Statement.Table)
	}
	if len(plan.OrderBy) != 1 || plan.OrderBy[0].Column != "id" {
		t.Fatalf("OrderBy = %+v, want id term", plan.OrderBy)
	}
	cmp, ok := plan.NormalizedPredicate.(sql.ComparisonPredicate)
	if !ok {
		t.Fatalf("NormalizedPredicate = %T, want ComparisonPredicate", plan.NormalizedPredicate)
	}
	if cmp.Filter.Literal.Kind != sql.LitSender {
		t.Fatalf("NormalizedPredicate literal kind = %v, want LitSender", cmp.Filter.Literal.Kind)
	}
	if !plan.UsesCallerIdentity {
		t.Fatal("UsesCallerIdentity = false, want true")
	}
}

func TestBuildFeatureGatesReturnUnsupportedFeature(t *testing.T) {
	cases := []struct {
		name  string
		query string
		opts  Options
	}{
		{
			name:  "limit",
			query: "SELECT * FROM users LIMIT 1",
			opts:  Options{},
		},
		{
			name:  "order_by",
			query: "SELECT * FROM users ORDER BY id",
			opts:  Options{AllowLimit: true, AllowOffset: true},
		},
		{
			name:  "offset",
			query: "SELECT * FROM users OFFSET 1",
			opts:  Options{AllowLimit: true, AllowOrderBy: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build(tc.query, tc.opts)
			if err == nil {
				t.Fatal("Build error = nil, want UnsupportedFeatureError")
			}
			var featureErr sql.UnsupportedFeatureError
			if !errors.As(err, &featureErr) {
				t.Fatalf("Build error = %T (%v), want UnsupportedFeatureError", err, err)
			}
			if featureErr.SQL != tc.query {
				t.Fatalf("UnsupportedFeatureError.SQL = %q, want %q", featureErr.SQL, tc.query)
			}
		})
	}
}

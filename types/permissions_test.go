package types

import "testing"

func TestMissingRequiredPermission(t *testing.T) {
	cases := []struct {
		name            string
		caller          CallerContext
		required        []string
		expectedMissing string
		expectedDenied  bool
	}{
		{
			name:           "allow-all",
			caller:         CallerContext{AllowAllPermissions: true},
			required:       []string{"messages:send"},
			expectedDenied: false,
		},
		{
			name:           "all-present",
			caller:         CallerContext{Permissions: []string{"messages:read", "messages:send"}},
			required:       []string{"messages:send", "messages:read"},
			expectedDenied: false,
		},
		{
			name:           "all-present-reordered-grants",
			caller:         CallerContext{Permissions: []string{"messages:audit", "messages:send", "messages:read"}},
			required:       []string{"messages:send", "messages:read"},
			expectedDenied: false,
		},
		{
			name:           "all-present-duplicate-grants",
			caller:         CallerContext{Permissions: []string{"messages:read", "messages:read", "messages:send", "messages:audit"}},
			required:       []string{"messages:send", "messages:read"},
			expectedDenied: false,
		},
		{
			name:           "ignores-empty-required",
			caller:         CallerContext{Permissions: []string{"messages:send"}},
			required:       []string{"", "messages:send", ""},
			expectedDenied: false,
		},
		{
			name:           "all-present-duplicate-required",
			caller:         CallerContext{Permissions: []string{"messages:read", "messages:send", "messages:audit"}},
			required:       []string{"messages:send", "messages:read", "messages:send"},
			expectedDenied: false,
		},
		{
			name:            "first-missing",
			caller:          CallerContext{Permissions: []string{"messages:send"}},
			required:        []string{"messages:read", "messages:admin"},
			expectedMissing: "messages:read",
			expectedDenied:  true,
		},
		{
			name:            "first-missing-with-extra-grant",
			caller:          CallerContext{Permissions: []string{"messages:send", "messages:audit"}},
			required:        []string{"messages:read", "messages:admin"},
			expectedMissing: "messages:read",
			expectedDenied:  true,
		},
		{
			name:            "first-missing-with-empty-required",
			caller:          CallerContext{Permissions: []string{"messages:send"}},
			required:        []string{"", "messages:read", "messages:admin"},
			expectedMissing: "messages:read",
			expectedDenied:  true,
		},
		{
			name:            "first-missing-duplicate-required",
			caller:          CallerContext{Permissions: []string{"messages:send"}},
			required:        []string{"messages:read", "messages:admin", "messages:read"},
			expectedMissing: "messages:read",
			expectedDenied:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			missing, denied := MissingRequiredPermission(tc.caller, tc.required)
			if missing != tc.expectedMissing || denied != tc.expectedDenied {
				t.Fatalf("MissingRequiredPermission(%v, %v) = (%q, %v), want (%q, %v)",
					tc.caller.Permissions, tc.required, missing, denied, tc.expectedMissing, tc.expectedDenied)
			}
		})
	}
}

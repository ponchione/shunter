package types

import "testing"

func TestMissingRequiredPermissionAllowAllBypassesRequired(t *testing.T) {
	caller := CallerContext{AllowAllPermissions: true}

	missing, denied := MissingRequiredPermission(caller, []string{"messages:send"})
	if denied {
		t.Fatalf("denied = true, want false; missing = %q", missing)
	}
	if missing != "" {
		t.Fatalf("missing = %q, want empty", missing)
	}
}

func TestMissingRequiredPermissionAllPresentSucceeds(t *testing.T) {
	caller := CallerContext{Permissions: []string{"messages:read", "messages:send"}}

	missing, denied := MissingRequiredPermission(caller, []string{"messages:send", "messages:read"})
	if denied {
		t.Fatalf("denied = true, want false; missing = %q", missing)
	}
	if missing != "" {
		t.Fatalf("missing = %q, want empty", missing)
	}
}

func TestMissingRequiredPermissionReportsFirstMissingRequired(t *testing.T) {
	caller := CallerContext{Permissions: []string{"messages:send"}}

	missing, denied := MissingRequiredPermission(caller, []string{"messages:read", "messages:admin"})
	if !denied {
		t.Fatal("denied = false, want true")
	}
	if missing != "messages:read" {
		t.Fatalf("missing = %q, want messages:read", missing)
	}
}

func TestMissingRequiredPermissionIgnoresEmptyRequiredStrings(t *testing.T) {
	caller := CallerContext{Permissions: []string{"messages:send"}}

	missing, denied := MissingRequiredPermission(caller, []string{"", "messages:send", ""})
	if denied {
		t.Fatalf("denied = true, want false; missing = %q", missing)
	}
	if missing != "" {
		t.Fatalf("missing = %q, want empty", missing)
	}
}

func TestMissingRequiredPermissionGrantSetMetamorphic(t *testing.T) {
	const seed = uint64(0x9e7715510)
	cases := []struct {
		name            string
		caller          CallerContext
		required        []string
		variantCallers  []CallerContext
		variantRequired [][]string
		expectedMissing string
		expectedDenied  bool
	}{
		{
			name:     "all-present",
			caller:   CallerContext{Permissions: []string{"messages:read", "messages:send", "messages:audit"}},
			required: []string{"messages:send", "messages:read"},
			variantCallers: []CallerContext{
				{Permissions: []string{"messages:audit", "messages:send", "messages:read"}},
				{Permissions: []string{"messages:read", "messages:read", "messages:send", "messages:audit"}},
			},
			variantRequired: [][]string{
				{"", "messages:send", "", "messages:read"},
				{"messages:send", "messages:read", "messages:send"},
			},
			expectedMissing: "",
			expectedDenied:  false,
		},
		{
			name:     "first-missing-stable",
			caller:   CallerContext{Permissions: []string{"messages:send"}},
			required: []string{"messages:read", "messages:admin"},
			variantCallers: []CallerContext{
				{Permissions: []string{"messages:send", "messages:audit"}},
				{Permissions: []string{"messages:audit", "messages:send", "messages:send"}},
			},
			variantRequired: [][]string{
				{"", "messages:read", "messages:admin"},
				{"messages:read", "messages:admin", "messages:read"},
			},
			expectedMissing: "messages:read",
			expectedDenied:  true,
		},
	}

	for caseIndex, tc := range cases {
		assertMissingRequiredPermissionResult(t, seed, caseIndex*10, tc.name, tc.caller, tc.required, tc.expectedMissing, tc.expectedDenied)
		for variantIndex, caller := range tc.variantCallers {
			assertMissingRequiredPermissionResult(t, seed, caseIndex*10+variantIndex+1, tc.name, caller, tc.required, tc.expectedMissing, tc.expectedDenied)
		}
		for variantIndex, required := range tc.variantRequired {
			assertMissingRequiredPermissionResult(t, seed, caseIndex*10+len(tc.variantCallers)+variantIndex+1, tc.name, tc.caller, required, tc.expectedMissing, tc.expectedDenied)
		}
	}
}

func assertMissingRequiredPermissionResult(t *testing.T, seed uint64, opIndex int, caseName string, caller CallerContext, required []string, expectedMissing string, expectedDenied bool) {
	t.Helper()
	missing, denied := MissingRequiredPermission(caller, required)
	if missing != expectedMissing || denied != expectedDenied {
		t.Fatalf("seed=%#x op_index=%d runtime_config=case=%s operation=MissingRequiredPermission caller_permissions=%v required=%v observed=(missing=%q,denied=%v) expected=(missing=%q,denied=%v)",
			seed, opIndex, caseName, caller.Permissions, required, missing, denied, expectedMissing, expectedDenied)
	}
}

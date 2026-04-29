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

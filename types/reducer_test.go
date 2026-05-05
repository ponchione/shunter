package types

import "testing"

func TestCallerContextCopyDetachesPrincipalAndPermissions(t *testing.T) {
	caller := CallerContext{
		Principal: AuthPrincipal{
			Issuer:      "issuer",
			Subject:     "alice",
			Audience:    []string{"shunter-api"},
			Permissions: []string{"principal:permission"},
		},
		Permissions: []string{"messages:send"},
	}

	copied := caller.Copy()
	copied.Principal.Audience[0] = "mutated"
	copied.Principal.Permissions[0] = "mutated"
	copied.Permissions[0] = "mutated"

	if caller.Principal.Audience[0] != "shunter-api" {
		t.Fatalf("Principal.Audience aliases copy: caller=%+v copied=%+v", caller, copied)
	}
	if caller.Principal.Permissions[0] != "principal:permission" {
		t.Fatalf("Principal.Permissions aliases copy: caller=%+v copied=%+v", caller, copied)
	}
	if caller.Permissions[0] != "messages:send" {
		t.Fatalf("Permissions aliases copy: caller=%+v copied=%+v", caller, copied)
	}
}

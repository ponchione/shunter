package types

import (
	"encoding/json"
	"testing"
)

func TestAuthClaimsCopyAndGet(t *testing.T) {
	var zero AuthClaims
	if copied := zero.Copy(); copied.Values != nil {
		t.Fatalf("zero Copy Values = %#v, want nil", copied.Values)
	}
	if got, ok := zero.Get("email"); ok || got != nil {
		t.Fatalf("zero Get = %q, %v; want nil, false", got, ok)
	}

	claims := AuthClaims{Values: map[string]json.RawMessage{
		"email": []byte(`"alice@example.com"`),
		"role":  []byte(`"authenticated"`),
	}}
	copied := claims.Copy()
	copied.Values["email"][1] = 'A'
	copied.Values["role"] = []byte(`"mutated"`)
	if string(claims.Values["email"]) != `"alice@example.com"` {
		t.Fatalf("Copy aliased raw message bytes: claims=%s copied=%s", claims.Values["email"], copied.Values["email"])
	}
	if string(claims.Values["role"]) != `"authenticated"` {
		t.Fatalf("Copy aliased map entry: claims=%s copied=%s", claims.Values["role"], copied.Values["role"])
	}

	got, ok := claims.Get("email")
	if !ok || string(got) != `"alice@example.com"` {
		t.Fatalf("Get(email) = %q, %v; want copied email claim", got, ok)
	}
	got[1] = 'A'
	if string(claims.Values["email"]) != `"alice@example.com"` {
		t.Fatalf("Get returned aliased raw message: claims=%s got=%s", claims.Values["email"], got)
	}
	if got, ok := claims.Get("missing"); ok || got != nil {
		t.Fatalf("Get(missing) = %q, %v; want nil, false", got, ok)
	}
}

func TestCallerContextCopyDetachesPrincipalAndPermissions(t *testing.T) {
	caller := CallerContext{
		Principal: AuthPrincipal{
			Issuer:      "issuer",
			Subject:     "alice",
			Audience:    []string{"shunter-api"},
			Permissions: []string{"principal:permission"},
			Claims: AuthClaims{Values: map[string]json.RawMessage{
				"email": []byte(`"alice@example.com"`),
			}},
		},
		Permissions: []string{"messages:send"},
	}

	copied := caller.Copy()
	copied.Principal.Audience[0] = "mutated"
	copied.Principal.Permissions[0] = "mutated"
	copied.Principal.Claims.Values["email"][1] = 'A'
	copied.Permissions[0] = "mutated"

	if caller.Principal.Audience[0] != "shunter-api" {
		t.Fatalf("Principal.Audience aliases copy: caller=%+v copied=%+v", caller, copied)
	}
	if caller.Principal.Permissions[0] != "principal:permission" {
		t.Fatalf("Principal.Permissions aliases copy: caller=%+v copied=%+v", caller, copied)
	}
	if string(caller.Principal.Claims.Values["email"]) != `"alice@example.com"` {
		t.Fatalf("Principal.Claims aliases copy: caller=%+v copied=%+v", caller, copied)
	}
	if caller.Permissions[0] != "messages:send" {
		t.Fatalf("Permissions aliases copy: caller=%+v copied=%+v", caller, copied)
	}
}

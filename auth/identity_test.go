package auth

import "testing"

func TestDeriveIdentityDeterministic(t *testing.T) {
	a := DeriveIdentity("https://issuer.example", "alice")
	b := DeriveIdentity("https://issuer.example", "alice")
	if a != b {
		t.Errorf("same (iss,sub) must yield same Identity: a=%x b=%x", a, b)
	}
}

func TestDeriveIdentityDifferentSubjectDifferent(t *testing.T) {
	a := DeriveIdentity("issuer", "alice")
	b := DeriveIdentity("issuer", "bob")
	if a == b {
		t.Error("different subjects must yield different Identities")
	}
}

func TestDeriveIdentityDifferentIssuerDifferent(t *testing.T) {
	a := DeriveIdentity("issuerA", "sub")
	b := DeriveIdentity("issuerB", "sub")
	if a == b {
		t.Error("different issuers must yield different Identities")
	}
}

// Length-prefix rule: ("a", "b") must NOT collide with ("ab", "").
func TestDeriveIdentityBoundaryDisambiguation(t *testing.T) {
	a := DeriveIdentity("a", "b")
	b := DeriveIdentity("ab", "")
	if a == b {
		t.Error("issuer/subject boundary must be unambiguous; ('a','b') collided with ('ab','')")
	}

	// Also ("ab", "c") vs ("a", "bc").
	c := DeriveIdentity("ab", "c")
	d := DeriveIdentity("a", "bc")
	if c == d {
		t.Error("'(ab,c)' collided with '(a,bc)'")
	}
}

func TestDeriveIdentityNotZero(t *testing.T) {
	id := DeriveIdentity("issuer", "subject")
	if id.IsZero() {
		t.Error("real Identity should not equal the zero Identity")
	}
}

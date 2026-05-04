package protocol

import "testing"

func TestProtocolVersionPolicyV1(t *testing.T) {
	if MinSupportedProtocolVersion != ProtocolVersionV1 {
		t.Fatalf("MinSupportedProtocolVersion = %s, want v1", MinSupportedProtocolVersion)
	}
	if CurrentProtocolVersion != ProtocolVersionV1 {
		t.Fatalf("CurrentProtocolVersion = %s, want v1", CurrentProtocolVersion)
	}

	versions := SupportedProtocolVersions()
	if len(versions) != 1 || versions[0] != ProtocolVersionV1 {
		t.Fatalf("SupportedProtocolVersions = %#v, want [v1]", versions)
	}
	versions[0] = ProtocolVersionUnknown
	if got := SupportedProtocolVersions()[0]; got != ProtocolVersionV1 {
		t.Fatalf("SupportedProtocolVersions returned mutable storage, got %s", got)
	}

	subprotocols := SupportedSubprotocols()
	if len(subprotocols) != 1 || subprotocols[0] != SubprotocolV1 {
		t.Fatalf("SupportedSubprotocols = %#v, want [%q]", subprotocols, SubprotocolV1)
	}

	token, ok := SubprotocolForVersion(ProtocolVersionV1)
	if !ok || token != SubprotocolV1 {
		t.Fatalf("SubprotocolForVersion(v1) = %q, %v; want %q, true", token, ok, SubprotocolV1)
	}
	version, ok := ProtocolVersionForSubprotocol(SubprotocolV1)
	if !ok || version != ProtocolVersionV1 {
		t.Fatalf("ProtocolVersionForSubprotocol(%q) = %s, %v; want v1, true", SubprotocolV1, version, ok)
	}

	if _, ok := SubprotocolForVersion(ProtocolVersion(2)); ok {
		t.Fatal("SubprotocolForVersion(v2) reported supported")
	}
	if _, ok := ProtocolVersionForSubprotocol("v2.bsatn.shunter"); ok {
		t.Fatal("ProtocolVersionForSubprotocol(v2 token) reported supported")
	}
}

func TestProtocolVersionString(t *testing.T) {
	cases := []struct {
		version ProtocolVersion
		want    string
	}{
		{ProtocolVersionUnknown, "unknown"},
		{ProtocolVersionV1, "v1"},
		{ProtocolVersion(12), "v12"},
	}
	for _, tc := range cases {
		if got := tc.version.String(); got != tc.want {
			t.Fatalf("ProtocolVersion(%d).String() = %q, want %q", tc.version, got, tc.want)
		}
	}
}

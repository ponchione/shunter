package protocol

import "testing"

func TestProtocolVersionPolicy(t *testing.T) {
	if MinSupportedProtocolVersion != ProtocolVersionV1 {
		t.Fatalf("MinSupportedProtocolVersion = %s, want v1", MinSupportedProtocolVersion)
	}
	if CurrentProtocolVersion != ProtocolVersionV2 {
		t.Fatalf("CurrentProtocolVersion = %s, want v2", CurrentProtocolVersion)
	}

	versions := SupportedProtocolVersions()
	if len(versions) != 2 || versions[0] != ProtocolVersionV2 || versions[1] != ProtocolVersionV1 {
		t.Fatalf("SupportedProtocolVersions = %#v, want [v2 v1]", versions)
	}
	versions[0] = ProtocolVersionUnknown
	if got := SupportedProtocolVersions()[0]; got != ProtocolVersionV2 {
		t.Fatalf("SupportedProtocolVersions returned mutable storage, got %s", got)
	}

	subprotocols := SupportedSubprotocols()
	if len(subprotocols) != 2 || subprotocols[0] != SubprotocolV2 || subprotocols[1] != SubprotocolV1 {
		t.Fatalf("SupportedSubprotocols = %#v, want [%q %q]", subprotocols, SubprotocolV2, SubprotocolV1)
	}

	token, ok := SubprotocolForVersion(ProtocolVersionV1)
	if !ok || token != SubprotocolV1 {
		t.Fatalf("SubprotocolForVersion(v1) = %q, %v; want %q, true", token, ok, SubprotocolV1)
	}
	version, ok := ProtocolVersionForSubprotocol(SubprotocolV1)
	if !ok || version != ProtocolVersionV1 {
		t.Fatalf("ProtocolVersionForSubprotocol(%q) = %s, %v; want v1, true", SubprotocolV1, version, ok)
	}

	token, ok = SubprotocolForVersion(ProtocolVersionV2)
	if !ok || token != SubprotocolV2 {
		t.Fatalf("SubprotocolForVersion(v2) = %q, %v; want %q, true", token, ok, SubprotocolV2)
	}
	version, ok = ProtocolVersionForSubprotocol(SubprotocolV2)
	if !ok || version != ProtocolVersionV2 {
		t.Fatalf("ProtocolVersionForSubprotocol(%q) = %s, %v; want v2, true", SubprotocolV2, version, ok)
	}
}

func TestProtocolVersionString(t *testing.T) {
	cases := []struct {
		version ProtocolVersion
		want    string
	}{
		{ProtocolVersionUnknown, "unknown"},
		{ProtocolVersionV1, "v1"},
		{ProtocolVersionV2, "v2"},
		{ProtocolVersion(12), "v12"},
	}
	for _, tc := range cases {
		if got := tc.version.String(); got != tc.want {
			t.Fatalf("ProtocolVersion(%d).String() = %q, want %q", tc.version, got, tc.want)
		}
	}
}

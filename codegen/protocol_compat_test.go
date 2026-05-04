package codegen

import (
	"fmt"
	"testing"

	"github.com/ponchione/shunter/protocol"
)

func TestGeneratedTypeScriptPinsRuntimeProtocolMetadata(t *testing.T) {
	out, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	defaultSubprotocol, ok := protocol.SubprotocolForVersion(protocol.CurrentProtocolVersion)
	if !ok {
		t.Fatalf("current protocol version %s has no subprotocol", protocol.CurrentProtocolVersion)
	}

	assertContains(t, ts, `export const shunterProtocol = {`)
	assertContains(t, ts, fmt.Sprintf("minSupportedVersion: %d,", protocol.MinSupportedProtocolVersion))
	assertContains(t, ts, fmt.Sprintf("currentVersion: %d,", protocol.CurrentProtocolVersion))
	assertContains(t, ts, fmt.Sprintf("defaultSubprotocol: %q,", defaultSubprotocol))
	assertContains(t, ts, fmt.Sprintf("supportedSubprotocols: %s,", typeScriptStringArray(protocol.SupportedSubprotocols())))
	assertContains(t, ts, `export type ShunterSubprotocol = (typeof shunterProtocol.supportedSubprotocols)[number];`)
}

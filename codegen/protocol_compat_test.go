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

	assertContains(t, ts, `import type {`)
	assertContains(t, ts, `GeneratedContractMetadata as ShunterGeneratedContractMetadata,`)
	assertContains(t, ts, `ProtocolMetadata as ShunterProtocolMetadata,`)
	assertContains(t, ts, `ReducerCaller as ShunterReducerCaller,`)
	assertContains(t, ts, `} from "@shunter/client";`)

	defaultSubprotocol, ok := protocol.SubprotocolForVersion(protocol.CurrentProtocolVersion)
	if !ok {
		t.Fatalf("current protocol version %s has no subprotocol", protocol.CurrentProtocolVersion)
	}

	assertContains(t, ts, `export const shunterProtocol = {`)
	assertContains(t, ts, fmt.Sprintf("minSupportedVersion: %d,", protocol.MinSupportedProtocolVersion))
	assertContains(t, ts, fmt.Sprintf("currentVersion: %d,", protocol.CurrentProtocolVersion))
	assertContains(t, ts, fmt.Sprintf("defaultSubprotocol: %q,", defaultSubprotocol))
	assertContains(t, ts, fmt.Sprintf("supportedSubprotocols: %s,", typeScriptStringArray(protocol.SupportedSubprotocols())))
	assertContains(t, ts, `} as const satisfies ShunterProtocolMetadata;`)
	assertContains(t, ts, `export type ShunterSubprotocol = (typeof shunterProtocol.supportedSubprotocols)[number];`)
	assertContains(t, ts, `export const shunterContract = {`)
	assertContains(t, ts, `contractFormat: "shunter.module_contract",`)
	assertContains(t, ts, `contractVersion: 1,`)
	assertContains(t, ts, `moduleName: "chat",`)
	assertContains(t, ts, `moduleVersion: "v1.2.3",`)
	assertContains(t, ts, `protocol: shunterProtocol,`)
	assertContains(t, ts, `} as const satisfies ShunterGeneratedContractMetadata<typeof shunterProtocol>;`)
}

func TestGeneratedTypeScriptRuntimeImportOverride(t *testing.T) {
	out, err := Generate(contractFixture(), Options{
		Language:                LanguageTypeScript,
		TypeScriptRuntimeImport: "@app/shunter-runtime",
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `} from "@app/shunter-runtime";`)
	assertNotContains(t, ts, `} from "@shunter/client";`)
}

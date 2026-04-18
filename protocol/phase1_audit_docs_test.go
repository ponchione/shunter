package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhase1AuditDocsCurrentStatusDoesNotClaimClosedProtocolGapsAreStillLive(t *testing.T) {
	content := readRepoFile(t, "..", "docs", "current-status.md")
	if strings.Contains(content, "- forked subprotocol namespace") {
		t.Fatal("docs/current-status.md still claims a forked subprotocol namespace under current protocol differences")
	}
	if strings.Contains(content, "- compression-tag behavior differs") {
		t.Fatal("docs/current-status.md still claims compression-tag behavior differs after Phase 1 closure")
	}
}

func TestPhase1AuditDocsSpec005CloseCodePolicyMentions1002(t *testing.T) {
	content := readRepoFile(t, "..", "docs", "decomposition", "005-protocol", "SPEC-005-protocol.md")
	want := "- **Close-code policy:** Shunter's documented close behavior includes `1000`, `1002`, `1008`, and `1011`"
	if !strings.Contains(content, want) {
		t.Fatalf("SPEC-005 close-code divergence bullet missing updated 1002 wording: want substring %q", want)
	}
}

func TestPhase1AuditDocsSavedPlanUsesLiveSubprotocolParitySelector(t *testing.T) {
	content := readRepoFile(t, "..", ".hermes", "plans", "2026-04-18_073534-phase1-wire-level-parity.md")
	stale := "rtk go test ./protocol -run TestPhase1ParitySubprotocol -v"
	if strings.Contains(content, stale) {
		t.Fatalf("saved Phase 1 plan still uses stale selector %q", stale)
	}
	want := "rtk go test ./protocol -run 'TestPhase1ParityReferenceSubprotocol|TestPhase1ParityLegacyShunterSubprotocolStillAccepted|TestPhase1ParityReferenceSubprotocolPreferred' -v"
	if !strings.Contains(content, want) {
		t.Fatalf("saved Phase 1 plan missing live subprotocol parity selector %q", want)
	}
}

func readRepoFile(t *testing.T, elems ...string) string {
	t.Helper()
	path := filepath.Join(elems...)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

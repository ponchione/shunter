package shunter

import "testing"

func TestNewModuleStoresName(t *testing.T) {
	mod := NewModule("chat")
	if mod == nil {
		t.Fatal("NewModule returned nil")
	}
	if got := mod.Name(); got != "chat" {
		t.Fatalf("Name() = %q, want %q", got, "chat")
	}
}

func TestModuleVersionIsChainableAndPreservesValue(t *testing.T) {
	mod := NewModule("chat")
	if got := mod.Version("v0.1.0"); got != mod {
		t.Fatal("Version did not return the receiver")
	}
	if got := mod.VersionString(); got != "v0.1.0" {
		t.Fatalf("VersionString() = %q, want %q", got, "v0.1.0")
	}
}

func TestModuleMetadataDefensivelyCopiesInput(t *testing.T) {
	input := map[string]string{
		"owner": "team-a",
		"env":   "dev",
	}
	mod := NewModule("chat").Metadata(input)

	input["owner"] = "team-b"
	input["added"] = "after-call"

	got := mod.MetadataMap()
	if got["owner"] != "team-a" {
		t.Fatalf("metadata owner = %q, want %q", got["owner"], "team-a")
	}
	if _, ok := got["added"]; ok {
		t.Fatal("metadata included key added after Metadata call")
	}
}

func TestModuleMetadataMapDefensivelyCopiesOutput(t *testing.T) {
	mod := NewModule("chat").Metadata(map[string]string{"owner": "team-a"})

	first := mod.MetadataMap()
	first["owner"] = "team-b"
	first["added"] = "after-read"

	second := mod.MetadataMap()
	if second["owner"] != "team-a" {
		t.Fatalf("metadata owner = %q, want %q", second["owner"], "team-a")
	}
	if _, ok := second["added"]; ok {
		t.Fatal("MetadataMap returned aliased map; later read included caller mutation")
	}
}

func TestModuleMetadataNilClearsMetadata(t *testing.T) {
	mod := NewModule("chat").Metadata(map[string]string{"owner": "team-a"})
	mod.Metadata(nil)

	got := mod.MetadataMap()
	if len(got) != 0 {
		t.Fatalf("MetadataMap length after Metadata(nil) = %d, want 0", len(got))
	}
}

func TestBlankModuleNameIsConstructible(t *testing.T) {
	mod := NewModule("   ")
	if mod == nil {
		t.Fatal("NewModule returned nil for blank name")
	}
	if got := mod.Name(); got != "   " {
		t.Fatalf("Name() = %q, want original blank string", got)
	}
}

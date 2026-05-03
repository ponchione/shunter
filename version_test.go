package shunter

import (
	"regexp"
	"runtime/debug"
	"testing"
)

func TestVersionFileUsesVPrefixedSemVer(t *testing.T) {
	pattern := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	if !pattern.MatchString(versionFromFile) {
		t.Fatalf("VERSION = %q, want v-prefixed SemVer", versionFromFile)
	}
}

func TestCurrentBuildInfoHasDefaultVersion(t *testing.T) {
	oldVersion := Version
	Version = ""
	defer func() {
		Version = oldVersion
	}()

	info := CurrentBuildInfo()
	if info.ModulePath != modulePath {
		t.Fatalf("ModulePath = %q, want %q", info.ModulePath, modulePath)
	}
	if info.Version == "" || info.Version == "(devel)" {
		t.Fatalf("Version = %q, want concrete default version", info.Version)
	}
	if info.GoVersion == "" {
		t.Fatal("GoVersion is empty")
	}
}

func TestCurrentBuildInfoUsesVersionFileByDefault(t *testing.T) {
	oldVersion := Version
	Version = versionFromFile
	defer func() {
		Version = oldVersion
	}()

	info := CurrentBuildInfo()
	if info.Version != versionFromFile {
		t.Fatalf("Version = %q, want version file value %q", info.Version, versionFromFile)
	}
}

func TestCurrentBuildInfoHonorsLinkerStyleOverrides(t *testing.T) {
	oldVersion := Version
	oldCommit := Commit
	oldDate := Date
	Version = " v9.8.7 \n"
	Commit = " abc123 \n"
	Date = " 2026-05-03T12:34:56Z \n"
	defer func() {
		Version = oldVersion
		Commit = oldCommit
		Date = oldDate
	}()

	info := CurrentBuildInfo()
	if info.Version != "v9.8.7" {
		t.Fatalf("Version = %q, want linker override", info.Version)
	}
	if info.Commit != "abc123" {
		t.Fatalf("Commit = %q, want linker override", info.Commit)
	}
	if info.Date != "2026-05-03T12:34:56Z" {
		t.Fatalf("Date = %q, want linker override", info.Date)
	}
}

func TestShunterModuleVersionIgnoresLocalReplacePlaceholder(t *testing.T) {
	version := shunterModuleVersion(&debug.BuildInfo{
		Deps: []*debug.Module{
			{
				Path:    modulePath,
				Version: "v0.0.0",
				Replace: &debug.Module{
					Path: "../shunter",
				},
			},
		},
	})
	if version != "" {
		t.Fatalf("shunterModuleVersion with local replace = %q, want empty", version)
	}
}

func TestShunterModuleVersionUsesVersionedReplace(t *testing.T) {
	version := shunterModuleVersion(&debug.BuildInfo{
		Deps: []*debug.Module{
			{
				Path:    modulePath,
				Version: "v0.0.0",
				Replace: &debug.Module{
					Path:    "github.com/ponchione/shunter",
					Version: "v1.2.3",
				},
			},
		},
	})
	if version != "v1.2.3" {
		t.Fatalf("shunterModuleVersion with versioned replace = %q, want v1.2.3", version)
	}
}

package shunter

import (
	_ "embed"
	"runtime"
	"runtime/debug"
	"strings"
)

const (
	modulePath      = "github.com/ponchione/shunter"
	fallbackVersion = "v0.0.0-dev"
)

//go:embed VERSION
var versionFile string

var versionFromFile = cleanVersion(versionFile)

// Version is the Shunter source version. Release builds may override it with:
//
//	-ldflags "-X github.com/ponchione/shunter.Version=vX.Y.Z"
var Version = versionFromFile

// Commit is the source revision for a release build. It is normally supplied
// by -ldflags, and falls back to Go's VCS build metadata when available.
var Commit string

// Date is the UTC build timestamp for a release build. It is normally supplied
// by -ldflags, and falls back to Go's VCS build metadata when available.
var Date string

// BuildInfo describes the Shunter version embedded in the current binary.
type BuildInfo struct {
	ModulePath string
	Version    string
	Commit     string
	Date       string
	Dirty      bool
	GoVersion  string
}

// CurrentBuildInfo returns Shunter version metadata for the current binary.
func CurrentBuildInfo() BuildInfo {
	info := BuildInfo{
		ModulePath: modulePath,
		Version:    cleanVersion(Version),
		Commit:     cleanVersion(Commit),
		Date:       cleanVersion(Date),
		GoVersion:  runtime.Version(),
	}

	var moduleVersion string
	if build, ok := debug.ReadBuildInfo(); ok {
		if build.GoVersion != "" {
			info.GoVersion = build.GoVersion
		}
		moduleVersion = shunterModuleVersion(build)
		applyBuildSettings(&info, build.Settings)
	}

	if info.Version == "" {
		if moduleVersion != "" {
			info.Version = moduleVersion
		}
	}
	if info.Version == "" {
		info.Version = versionFromFile
	}
	if info.Version == "" {
		info.Version = fallbackVersion
	}

	return info
}

func shunterModuleVersion(build *debug.BuildInfo) string {
	if build.Main.Path == modulePath {
		return cleanBuildVersion(build.Main.Version)
	}
	for _, dep := range build.Deps {
		if dep.Path != modulePath {
			continue
		}
		if dep.Replace != nil {
			return cleanBuildVersion(dep.Replace.Version)
		}
		return cleanBuildVersion(dep.Version)
	}
	return ""
}

func applyBuildSettings(info *BuildInfo, settings []debug.BuildSetting) {
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = cleanVersion(setting.Value)
			}
		case "vcs.time":
			if info.Date == "" {
				info.Date = cleanVersion(setting.Value)
			}
		case "vcs.modified":
			info.Dirty = setting.Value == "true"
		}
	}
}

func cleanBuildVersion(version string) string {
	version = cleanVersion(version)
	if version == "" || version == "(devel)" {
		return ""
	}
	return version
}

func cleanVersion(version string) string {
	return strings.TrimSpace(version)
}

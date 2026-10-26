// Package buildinfo describes the build of the running executable.
//
// Release builds set version, commit and date with linker flags:
//
//	-X github.com/MuhametSmaili/hetki/internal/buildinfo.version=v1.2.3
//	-X github.com/MuhametSmaili/hetki/internal/buildinfo.commit=<full SHA>
//	-X github.com/MuhametSmaili/hetki/internal/buildinfo.date=<RFC 3339>
//
// Other builds fall back to the module version and VCS revision that the Go
// toolchain records, when it records them.
package buildinfo

import "runtime/debug"

// Empty means that neither the linker nor the toolchain provided a value, so an
// explicit "dev" or "unknown" from the linker is kept as is.
var (
	version string
	commit  string
	date    string
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		version, commit = resolve(version, commit, info)
	}
}

// resolve keeps linker-provided values and fills only the empty ones.
func resolve(version, commit string, info *debug.BuildInfo) (string, string) {
	if version == "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	if commit != "" {
		return version, commit
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return version, setting.Value
		}
	}
	return version, commit
}

// Version is the release tag or module version, or "dev" when neither is known.
func Version() string { return orDefault(version, "dev") }

// Commit is the source revision, or "unknown".
func Commit() string { return orDefault(commit, "unknown") }

// Date is the build or commit date, or "unknown".
func Date() string { return orDefault(date, "unknown") }

// Identity names this build so that two hetki processes can tell whether they
// run the same build. Development builds without version information share
// one identity.
func Identity() string { return Version() + " " + Commit() }

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

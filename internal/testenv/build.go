package testenv

import "github.com/MuhametSmaili/hetki/internal/buildinfo"

// BuildFlags stamps a separately built hetki executable with the build
// identity of the running test binary. Without it, the toolchain records a
// different version, and status daemon startup treats the executable as
// another build.
func BuildFlags() []string {
	const prefix = "github.com/MuhametSmaili/hetki/internal/buildinfo."
	return []string{"-ldflags", "-X " + prefix + "version=" + buildinfo.Version() + " -X " + prefix + "commit=" + buildinfo.Commit()}
}

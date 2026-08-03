// Package version holds the single source of truth for the build version.
//
// The value is injected at link time:
//
//	go build -ldflags "-X github.com/.../internal/version.Version=v1.2.3"
//
// Every place that reports a version (the CLI, the MCP server identification,
// the container label) reads it from here, so they cannot disagree.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is the release version. Set by the linker for release builds; falls
// back to the module version recorded by the Go toolchain otherwise.
var Version = "dev"

// Commit is the git revision the binary was built from, if injected.
var Commit = "unknown"

// init recovers the version from the build info embedded by the Go toolchain.
//
// `go install module/cmd/x@v0.1.0` does not apply -ldflags, so a binary
// installed that way would otherwise report "dev" and give an operator no way
// to tell which release they are running. The module version and VCS revision
// are recorded in the build info regardless, so read them when the linker did
// not supply anything better.
func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = strings.TrimPrefix(info.Main.Version, "v")
	}
	if Commit == "unknown" {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				Commit = setting.Value
				return
			}
		}
	}
}

// Name is the server and binary name reported over MCP.
const Name = "toc-whmcs-mcp"

// String renders the full version for CLI output.
func String() string {
	return Name + " " + Version + " (" + Commit + ")"
}

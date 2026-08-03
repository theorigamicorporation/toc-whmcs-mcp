// Package version holds the single source of truth for the build version.
//
// The value is injected at link time:
//
//	go build -ldflags "-X github.com/.../internal/version.Version=v1.2.3"
//
// Every place that reports a version (the CLI, the MCP server identification,
// the container label) reads it from here, so they cannot disagree.
package version

// Version is the release version. "dev" when built without a linker override.
var Version = "dev"

// Commit is the git revision the binary was built from, if injected.
var Commit = "unknown"

// Name is the server and binary name reported over MCP.
const Name = "toc-whmcs-mcp"

// String renders the full version for CLI output.
func String() string {
	return Name + " " + Version + " (" + Commit + ")"
}

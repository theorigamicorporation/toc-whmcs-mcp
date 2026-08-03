package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// EnvFileVar names the environment variable holding the path to an env file.
const EnvFileVar = EnvPrefix + "ENV_FILE"

// loadEnvFile reads KEY=VALUE lines from path into the process environment.
//
// This exists so a WHMCS API secret does not have to be written into an MCP
// client's configuration. Clients such as Claude Code store the env block in a
// JSON file in plain text, which puts the credential at rest in a second place
// that is easy to forget when rotating, and impossible to commit safely.
// Pointing the client at this file instead means its config carries a path,
// not a secret.
//
// There is deliberately no automatic discovery of .env in the working
// directory. An MCP client chooses the working directory, so auto-loading
// would let whatever directory the client happened to start in supply
// credentials to a server that talks to a billing system.
//
// Values already present in the environment win. The file supplies defaults,
// so an explicit setting from a wrapper or a systemd unit is never silently
// overridden by a stale file.
//
// The values are returned rather than written into the process environment.
// os.Setenv would put a WHMCS secret in /proc/self/environ, where anything
// able to read the process can see it, and would hand it to any child process
// by inheritance. Keeping it in a map confines it to this package.
func loadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path) // #nosec G304 -- the path is operator-supplied configuration
	if err != nil {
		return nil, fmt.Errorf("read env file: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat env file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("env file %s is a directory", path)
	}
	// A file holding a billing credential must not be readable by other users
	// on the machine. Refusing is better than warning: a warning in an MCP
	// server's stderr is seen by nobody.
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf(
			"env file %s is mode %04o and readable by other users; it holds a WHMCS credential, so run: chmod 600 %s",
			path, perm, path)
	}

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		key, value, err := parseEnvLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		if key == "" {
			continue // blank or comment
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env file %s: %w", path, err)
	}
	return values, nil
}

// parseEnvLine parses one KEY=VALUE line, tolerating the conventions a
// hand-edited env file picks up: comments, blank lines, a leading `export`,
// and quoted values.
//
// It returns an empty key for lines that carry no assignment. Errors are
// reported rather than skipped, because a malformed line in a credential file
// usually means the credential is not what the operator thinks it is.
func parseEnvLine(raw string) (key, value string, err error) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", nil
	}
	line = strings.TrimPrefix(line, "export ")

	name, val, found := strings.Cut(line, "=")
	if !found {
		return "", "", fmt.Errorf("expected KEY=VALUE, got %q", truncate(line))
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("empty variable name")
	}

	val = strings.TrimSpace(val)
	// Strip one layer of matching quotes. Unquoted values keep any trailing
	// comment, because stripping one would silently truncate a secret that
	// legitimately contains a hash character.
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return name, val, nil
}

// truncate bounds a fragment quoted back in an error, so a malformed line
// cannot spill a secret into a log.
func truncate(s string) string {
	const max = 24
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

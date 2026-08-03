package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/config"
)

// writeEnvFile creates an env file with the given contents and mode.
func writeEnvFile(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	// WriteFile is subject to umask, so set the mode explicitly.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return path
}

func TestEnvFileSuppliesConfiguration(t *testing.T) {
	// The point of the feature: an MCP client's config carries a path, not a
	// WHMCS credential.
	path := writeEnvFile(t, `
# a comment
WHMCS_MCP_WHMCS_URL=https://billing.example.com

export WHMCS_MCP_API_IDENTIFIER=identifier-from-file
WHMCS_MCP_API_SECRET="secret-from-file"
WHMCS_MCP_PROFILE='support'
`, 0o600)

	cfg, err := config.Load([]string{"-env-file", path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != "https://billing.example.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Identifier != "identifier-from-file" {
		t.Errorf("Identifier = %q; the export prefix should be tolerated", cfg.Identifier)
	}
	if cfg.Secret != "secret-from-file" {
		t.Errorf("Secret = %q; double quotes should be stripped", cfg.Secret)
	}
	if string(cfg.Profile) != "support" {
		t.Errorf("Profile = %q; single quotes should be stripped", cfg.Profile)
	}
}

func TestEnvironmentWinsOverTheFile(t *testing.T) {
	// The file is a default. A wrapper, a systemd unit or a container that sets
	// something explicitly must not be silently overridden by a stale file.
	path := writeEnvFile(t, "WHMCS_MCP_PROFILE=admin\n", 0o600)
	setEnv(t, validEnv())
	t.Setenv(config.EnvPrefix+"PROFILE", "readonly")

	cfg, err := config.Load([]string{"-env-file", path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(cfg.Profile) != "readonly" {
		t.Errorf("Profile = %q, want readonly; the file overrode an explicit environment value", cfg.Profile)
	}
}

func TestFlagsStillWinOverTheFile(t *testing.T) {
	path := writeEnvFile(t, strings.Join([]string{
		"WHMCS_MCP_WHMCS_URL=https://billing.example.com",
		"WHMCS_MCP_API_IDENTIFIER=i",
		"WHMCS_MCP_API_SECRET=s",
		"WHMCS_MCP_PROFILE=admin",
	}, "\n"), 0o600)

	cfg, err := config.Load([]string{"-env-file", path, "-profile", "billing"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(cfg.Profile) != "billing" {
		t.Errorf("Profile = %q, want billing", cfg.Profile)
	}
}

func TestWorldReadableEnvFileIsRefused(t *testing.T) {
	// The file holds a WHMCS credential. Refusing is better than warning: a
	// warning on an MCP server's stderr is seen by nobody.
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o666} {
		path := writeEnvFile(t, "WHMCS_MCP_API_SECRET=s\n", mode)
		_, err := config.Load([]string{"-env-file", path})
		if err == nil {
			t.Errorf("mode %04o was accepted", mode)
			continue
		}
		if !strings.Contains(err.Error(), "chmod 600") {
			t.Errorf("mode %04o: error does not say how to fix it: %v", mode, err)
		}
	}
}

func TestPrivateEnvFileIsAccepted(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400} {
		path := writeEnvFile(t, strings.Join([]string{
			"WHMCS_MCP_WHMCS_URL=https://billing.example.com",
			"WHMCS_MCP_API_IDENTIFIER=i",
			"WHMCS_MCP_API_SECRET=s",
		}, "\n"), mode)
		if _, err := config.Load([]string{"-env-file", path}); err != nil {
			t.Errorf("mode %04o was refused: %v", mode, err)
		}
	}
}

func TestMissingEnvFileIsAnError(t *testing.T) {
	// Silently ignoring a missing file would start the server with whatever
	// happened to be in the environment, which is how the wrong WHMCS instance
	// gets talked to.
	if _, err := config.Load([]string{"-env-file", "/nonexistent/env"}); err == nil {
		t.Fatal("a missing env file was ignored")
	}
}

func TestMalformedLineIsReportedWithItsLocation(t *testing.T) {
	path := writeEnvFile(t, "WHMCS_MCP_WHMCS_URL=https://x\nthis is not an assignment\n", 0o600)
	_, err := config.Load([]string{"-env-file", path})
	if err == nil {
		t.Fatal("a malformed line was ignored")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error does not locate the bad line: %v", err)
	}
}

func TestEnvFilePathCanComeFromTheEnvironment(t *testing.T) {
	path := writeEnvFile(t, strings.Join([]string{
		"WHMCS_MCP_WHMCS_URL=https://billing.example.com",
		"WHMCS_MCP_API_IDENTIFIER=i",
		"WHMCS_MCP_API_SECRET=s",
	}, "\n"), 0o600)
	t.Setenv(config.EnvFileVar, path)

	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != "https://billing.example.com" {
		t.Errorf("BaseURL = %q; the file named by %s was not read", cfg.BaseURL, config.EnvFileVar)
	}
}

func TestSecretsFromAFileAreStillMaskedInLogs(t *testing.T) {
	path := writeEnvFile(t, strings.Join([]string{
		"WHMCS_MCP_WHMCS_URL=https://billing.example.com",
		"WHMCS_MCP_API_IDENTIFIER=identifier-from-file",
		"WHMCS_MCP_API_SECRET=secret-from-file",
	}, "\n"), 0o600)

	cfg, err := config.Load([]string{"-env-file", path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, attr := range cfg.LogAttrs() {
		if strings.Contains(attr.Value.String(), "secret-from-file") {
			t.Errorf("attribute %s leaked a secret read from a file", attr.Key)
		}
	}
}

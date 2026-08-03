package config_test

import (
	"os"
	"testing"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/config"
)

func TestEnvFileDoesNotLeakIntoTheProcessEnvironment(t *testing.T) {
	// A WHMCS secret in os.Environ ends up in /proc/self/environ and is
	// inherited by any child process. Loading a file must not put it there.
	path := writeEnvFile(t, "WHMCS_MCP_WHMCS_URL=https://billing.example.com\n"+
		"WHMCS_MCP_API_IDENTIFIER=i\nWHMCS_MCP_API_SECRET=leaked-secret-value\n", 0o600)

	if _, err := config.Load([]string{"-env-file", path}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v, ok := os.LookupEnv("WHMCS_MCP_API_SECRET"); ok {
		t.Fatalf("the secret was written into the process environment: %q", v)
	}
	for _, kv := range os.Environ() {
		if len(kv) > 0 && kv == "WHMCS_MCP_API_SECRET=leaked-secret-value" {
			t.Fatal("secret present in os.Environ()")
		}
	}
}

package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/config"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/policy"
)

// setEnv sets the prefixed variables for one test and restores them after.
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(config.EnvPrefix+k, v)
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"WHMCS_URL":      "https://billing.example.com",
		"API_IDENTIFIER": "identifier-value",
		"API_SECRET":     "secret-value-0123456789",
	}
}

func TestMissingCredentialsStopStartup(t *testing.T) {
	// A server that starts and then fails every call puts the failure in an
	// agent transcript instead of a deployment log.
	tests := map[string]string{
		"WHMCS_URL":      "",
		"API_IDENTIFIER": "",
		"API_SECRET":     "",
	}
	for missing := range tests {
		env := validEnv()
		delete(env, missing)
		t.Run(missing, func(t *testing.T) {
			setEnv(t, env)
			_, err := config.Load(nil)
			if err == nil {
				t.Fatalf("configuration without %s was accepted", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("the error does not name the missing setting: %v", err)
			}
		})
	}
}

func TestDefaultsAreSafe(t *testing.T) {
	setEnv(t, validEnv())
	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Profile != policy.ProfileReadOnly {
		t.Errorf("default profile = %s, want readonly", cfg.Profile)
	}
	if cfg.AllowDestructive {
		t.Error("destructive actions are enabled by default")
	}
	if cfg.Transport != config.TransportStdio {
		t.Errorf("default transport = %s, want stdio", cfg.Transport)
	}
	if cfg.MaxPageSize <= 0 || cfg.DefaultPageSize <= 0 {
		t.Error("page sizes are unbounded by default")
	}
	if cfg.RequestTimeout <= 0 {
		t.Error("requests are unbounded by default")
	}
}

func TestFlagsOverrideEnvironment(t *testing.T) {
	env := validEnv()
	env["PROFILE"] = "support"
	setEnv(t, env)

	cfg, err := config.Load([]string{
		"-profile", "billing",
		"-default-page-size", "5",
		"-max-page-size", "10",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Profile != policy.ProfileBilling {
		t.Errorf("profile = %s, want billing (the flag must win)", cfg.Profile)
	}
	if cfg.MaxPageSize != 10 {
		t.Errorf("max page size = %d, want 10", cfg.MaxPageSize)
	}
}

func TestUnknownProfileAndTransportStopStartup(t *testing.T) {
	setEnv(t, validEnv())

	if _, err := config.Load([]string{"-profile", "root"}); err == nil {
		t.Error("an unknown profile was accepted")
	} else if !strings.Contains(err.Error(), "readonly") {
		t.Errorf("the error does not list the valid profiles: %v", err)
	}
	if _, err := config.Load([]string{"-transport", "carrier-pigeon"}); err == nil {
		t.Error("an unknown transport was accepted")
	}
}

func TestHTTPTransportRefusesToBeOpen(t *testing.T) {
	// An unauthenticated HTTP transport on a routable address is a remote
	// control plane for a billing system.
	setEnv(t, validEnv())

	if _, err := config.Load([]string{"-transport", "http", "-addr", "0.0.0.0:8080"}); err == nil {
		t.Fatal("an unauthenticated http transport on 0.0.0.0 was accepted")
	}

	// Loopback is allowed without a token: something else is fronting it.
	if _, err := config.Load([]string{"-transport", "http", "-addr", "127.0.0.1:8080"}); err != nil {
		t.Errorf("loopback http was refused: %v", err)
	}

	// With a token, a routable address is fine.
	env := validEnv()
	env["AUTH_TOKEN"] = "a-long-shared-secret"
	setEnv(t, env)
	if _, err := config.Load([]string{"-transport", "http", "-addr", "0.0.0.0:8080"}); err != nil {
		t.Errorf("authenticated http was refused: %v", err)
	}
}

func TestPageSizeConsistencyIsEnforced(t *testing.T) {
	setEnv(t, validEnv())
	if _, err := config.Load([]string{"-default-page-size", "100", "-max-page-size", "10"}); err == nil {
		t.Error("a default page size above the maximum was accepted")
	}
}

func TestCredentialsAreMaskedInLogAttributes(t *testing.T) {
	env := validEnv()
	setEnv(t, env)
	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, attr := range cfg.LogAttrs() {
		rendered := attr.Value.String()
		if strings.Contains(rendered, env["API_SECRET"]) || strings.Contains(rendered, env["API_IDENTIFIER"]) {
			t.Errorf("attribute %s renders a credential: %s", attr.Key, rendered)
		}
	}
}

func TestAllowlistParsing(t *testing.T) {
	env := validEnv()
	env["TOOL_ALLOWLIST"] = " whmcs_client_get , whmcs_invoice_list ,, "
	setEnv(t, env)

	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Allowlist) != 2 {
		t.Fatalf("allowlist = %v, want two entries", cfg.Allowlist)
	}
	if cfg.Allowlist[0] != "whmcs_client_get" {
		t.Errorf("entries were not trimmed: %q", cfg.Allowlist[0])
	}
}

func TestDurationsAreParsedFromEnvironment(t *testing.T) {
	env := validEnv()
	env["REQUEST_TIMEOUT"] = "3s"
	env["CONFIRM_TTL"] = "90s"
	setEnv(t, env)

	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RequestTimeout != 3*time.Second {
		t.Errorf("request timeout = %s, want 3s", cfg.RequestTimeout)
	}
	if cfg.ConfirmTTL != 90*time.Second {
		t.Errorf("confirm TTL = %s, want 90s", cfg.ConfirmTTL)
	}
}

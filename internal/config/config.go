// Package config loads and validates the server's configuration.
//
// Everything is validated at startup. A server that starts, advertises tools,
// and then fails every call because a credential is missing is worse than one
// that refuses to start: the failure shows up in an agent transcript rather
// than in a deployment log.
package config

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/policy"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/whmcs"
)

// EnvPrefix is the prefix for every environment variable this server reads.
const EnvPrefix = "WHMCS_MCP_"

// Transport selects how MCP is served.
type Transport string

const (
	// TransportStdio serves MCP over stdin and stdout, for local clients.
	TransportStdio Transport = "stdio"
	// TransportHTTP serves MCP over Streamable HTTP, for a hosted deployment.
	TransportHTTP Transport = "http"
)

// Config is the fully resolved configuration.
type Config struct {
	BaseURL    string
	Identifier string
	Secret     string
	AccessKey  string

	Profile          policy.Profile
	AllowDestructive bool
	Allowlist        []string

	Transport Transport
	Addr      string
	AuthToken string

	RequestTimeout   time.Duration
	MaxResponseBytes int64
	MaxRetries       int

	DefaultPageSize int
	MaxPageSize     int
	ConfirmTTL      time.Duration

	LogLevel slog.Level
}

// Load resolves configuration from the environment, then applies flags.
//
// Flags win over the environment, which is the ordering an operator expects
// when debugging a container: the environment is the deployment's opinion, the
// flag is the operator's.
func Load(args []string) (Config, error) {
	cfg := Config{
		BaseURL:          env("WHMCS_URL", ""),
		Identifier:       env("API_IDENTIFIER", ""),
		Secret:           env("API_SECRET", ""),
		AccessKey:        env("API_ACCESS_KEY", ""),
		AllowDestructive: envBool("ALLOW_DESTRUCTIVE", false),
		Allowlist:        envList("TOOL_ALLOWLIST"),
		Addr:             env("ADDR", "127.0.0.1:8080"),
		AuthToken:        env("AUTH_TOKEN", ""),
		RequestTimeout:   envDuration("REQUEST_TIMEOUT", whmcs.DefaultTimeout),
		MaxResponseBytes: int64(envInt("MAX_RESPONSE_BYTES", whmcs.DefaultMaxResponseBytes)),
		MaxRetries:       envInt("MAX_RETRIES", whmcs.DefaultMaxRetries),
		DefaultPageSize:  envInt("DEFAULT_PAGE_SIZE", 25),
		MaxPageSize:      envInt("MAX_PAGE_SIZE", 200),
		ConfirmTTL:       envDuration("CONFIRM_TTL", 5*time.Minute),
	}

	profileRaw := env("PROFILE", "")
	transportRaw := env("TRANSPORT", string(TransportStdio))
	logLevelRaw := env("LOG_LEVEL", "info")

	fs := flag.NewFlagSet("toc-whmcs-mcp", flag.ContinueOnError)
	fs.StringVar(&cfg.BaseURL, "whmcs-url", cfg.BaseURL, "WHMCS base URL, e.g. https://billing.example.com")
	fs.StringVar(&cfg.Identifier, "api-identifier", cfg.Identifier, "WHMCS API identifier")
	fs.StringVar(&cfg.Secret, "api-secret", cfg.Secret, "WHMCS API secret")
	fs.StringVar(&cfg.AccessKey, "api-access-key", cfg.AccessKey, "WHMCS API access key, if the instance requires one")
	fs.StringVar(&profileRaw, "profile", profileRaw, "capability profile: readonly, support, billing, admin")
	fs.BoolVar(&cfg.AllowDestructive, "allow-destructive", cfg.AllowDestructive, "enable actions classified destructive (still requires confirmation)")
	fs.StringVar(&transportRaw, "transport", transportRaw, "transport: stdio or http")
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "bind address for the http transport")
	fs.DurationVar(&cfg.RequestTimeout, "request-timeout", cfg.RequestTimeout, "per-request timeout for WHMCS calls")
	fs.IntVar(&cfg.MaxRetries, "max-retries", cfg.MaxRetries, "retries for transient failures on read-only actions")
	fs.IntVar(&cfg.DefaultPageSize, "default-page-size", cfg.DefaultPageSize, "page size applied when a tool call omits limit")
	fs.IntVar(&cfg.MaxPageSize, "max-page-size", cfg.MaxPageSize, "ceiling applied to any requested limit")
	fs.DurationVar(&cfg.ConfirmTTL, "confirm-ttl", cfg.ConfirmTTL, "confirmation token lifetime")
	fs.StringVar(&logLevelRaw, "log-level", logLevelRaw, "log level: debug, info, warn, error")

	allowlistRaw := strings.Join(cfg.Allowlist, ",")
	fs.StringVar(&allowlistRaw, "tool-allowlist", allowlistRaw, "comma-separated tool names; narrows the profile, never widens it")

	// Secrets are accepted as flags for local development, but a flag value is
	// visible in the process table, so the environment is the documented path.
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	cfg.Allowlist = splitList(allowlistRaw)

	profile, err := policy.ParseProfile(profileRaw)
	if err != nil {
		return Config{}, err
	}
	cfg.Profile = profile

	switch Transport(strings.ToLower(strings.TrimSpace(transportRaw))) {
	case TransportStdio, "":
		cfg.Transport = TransportStdio
	case TransportHTTP:
		cfg.Transport = TransportHTTP
	default:
		return Config{}, fmt.Errorf("unknown transport %q; valid transports are stdio and http", transportRaw)
	}

	level, err := parseLevel(logLevelRaw)
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = level

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate rejects a configuration the server cannot honour.
func (c Config) validate() error {
	var problems []string

	if strings.TrimSpace(c.BaseURL) == "" {
		problems = append(problems, EnvPrefix+"WHMCS_URL is required (the WHMCS base URL)")
	}
	if strings.TrimSpace(c.Identifier) == "" {
		problems = append(problems, EnvPrefix+"API_IDENTIFIER is required")
	}
	if strings.TrimSpace(c.Secret) == "" {
		problems = append(problems, EnvPrefix+"API_SECRET is required")
	}
	if c.DefaultPageSize < 1 {
		problems = append(problems, "default page size must be at least 1")
	}
	if c.MaxPageSize < 1 {
		problems = append(problems, "max page size must be at least 1")
	}
	if c.DefaultPageSize > c.MaxPageSize {
		problems = append(problems, "default page size cannot exceed max page size")
	}
	if c.RequestTimeout <= 0 {
		problems = append(problems, "request timeout must be positive")
	}
	if c.ConfirmTTL <= 0 {
		problems = append(problems, "confirmation TTL must be positive")
	}

	// An HTTP transport with no authentication on a non-loopback address is an
	// unauthenticated remote control plane for a billing system.
	if c.Transport == TransportHTTP && c.AuthToken == "" && !isLoopback(c.Addr) {
		problems = append(problems, fmt.Sprintf(
			"the http transport is bound to %s with no %sAUTH_TOKEN set; "+
				"set a token, or bind to a loopback address and front it with something that authenticates", c.Addr, EnvPrefix))
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// WHMCSConfig projects the client configuration.
func (c Config) WHMCSConfig() whmcs.Config {
	return whmcs.Config{
		BaseURL:          c.BaseURL,
		Identifier:       c.Identifier,
		Secret:           c.Secret,
		AccessKey:        c.AccessKey,
		Timeout:          c.RequestTimeout,
		MaxResponseBytes: c.MaxResponseBytes,
		MaxRetries:       c.MaxRetries,
	}
}

// PolicyConfig projects the access-control configuration.
func (c Config) PolicyConfig() policy.Config {
	return policy.Config{
		Profile:          c.Profile,
		AllowDestructive: c.AllowDestructive,
		Allowlist:        c.Allowlist,
	}
}

// LogAttrs renders the effective configuration for the startup record, with
// credentials masked.
func (c Config) LogAttrs() []slog.Attr {
	return []slog.Attr{
		slog.String("whmcs_url", c.BaseURL),
		slog.String("api_identifier", mask(c.Identifier)),
		slog.String("api_secret", mask(c.Secret)),
		slog.String("profile", string(c.Profile)),
		slog.Bool("allow_destructive", c.AllowDestructive),
		slog.Int("allowlist_size", len(c.Allowlist)),
		slog.String("transport", string(c.Transport)),
		slog.Duration("request_timeout", c.RequestTimeout),
		slog.Int64("max_response_bytes", c.MaxResponseBytes),
		slog.Int("default_page_size", c.DefaultPageSize),
		slog.Int("max_page_size", c.MaxPageSize),
		slog.Duration("confirm_ttl", c.ConfirmTTL),
	}
}

// mask renders a credential as a length hint, which is enough to tell "the
// wrong value" from "no value" without disclosing it.
func mask(s string) string {
	if s == "" {
		return "(unset)"
	}
	return fmt.Sprintf("(set, %d chars)", len(s))
}

func isLoopback(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return strings.HasPrefix(host, "127.")
	}
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q; valid levels are debug, info, warn, error", s)
	}
}

// --- environment helpers ---------------------------------------------------

func env(key, def string) string {
	if v, ok := os.LookupEnv(EnvPrefix + key); ok {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(key string, def bool) bool {
	raw := env(key, "")
	if raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	raw := env(key, "")
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	raw := env(key, "")
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return def
	}
	return d
}

func envList(key string) []string { return splitList(env(key, "")) }

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

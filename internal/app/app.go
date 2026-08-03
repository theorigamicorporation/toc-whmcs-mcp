// Package app assembles the server from its parts.
//
// It exists so that the entrypoint and the tests build the same thing. A test
// that wires up its own subset of the safety layer proves nothing about what
// ships.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"

	"github.com/mark3labs/mcp-go/server"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/audit"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/config"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/confirm"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/policy"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/tools"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/version"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/whmcs"
)

// App is a built server and the facts about it worth reporting.
type App struct {
	Server *server.MCPServer
	Policy *policy.Policy
	Audit  *audit.Logger
	Client *whmcs.Client
	// Tools are the names actually advertised, after the profile and allowlist
	// have been applied.
	Tools []string
}

// Build wires the server. auditSink receives audit and diagnostic records; it
// must not be stdout when the stdio transport is in use.
func Build(cfg config.Config, auditSink io.Writer) (*App, error) {
	client, err := whmcs.New(cfg.WHMCSConfig())
	if err != nil {
		return nil, fmt.Errorf("whmcs client: %w", err)
	}

	pol, err := policy.New(cfg.PolicyConfig())
	if err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}

	confirmStore, err := confirm.NewStore(confirm.WithTTL(cfg.ConfirmTTL))
	if err != nil {
		return nil, fmt.Errorf("confirmation store: %w", err)
	}

	auditor := audit.New(auditSink, cfg.LogLevel)

	mcpServer := server.NewMCPServer(
		version.Name,
		version.Version,
		server.WithToolCapabilities(false),
		// A panic in one tool handler must not take down a session that is in
		// the middle of a billing operation.
		server.WithRecovery(),
	)

	deps := tools.Deps{
		Client:  client,
		Policy:  pol,
		Confirm: confirmStore,
		Audit:   auditor,
		Limits: tools.Limits{
			DefaultPageSize: cfg.DefaultPageSize,
			MaxPageSize:     cfg.MaxPageSize,
		},
	}

	registered, err := tools.Register(mcpServer, deps, tools.All())
	if err != nil {
		return nil, fmt.Errorf("register tools: %w", err)
	}

	app := &App{
		Server: mcpServer,
		Policy: pol,
		Audit:  auditor,
		Client: client,
		Tools:  registered,
	}
	app.logStartup(cfg)
	return app, nil
}

// logStartup records the effective security posture once. A log should always
// be able to answer "what was this process allowed to do" without anyone
// having to reconstruct the environment it ran with.
func (a *App) logStartup(cfg config.Config) {
	attrs := append([]slog.Attr{
		slog.String("version", version.Version),
		slog.String("commit", version.Commit),
		slog.Int("tools_advertised", len(a.Tools)),
		slog.Int("actions_permitted", len(a.Policy.PermittedActions())),
		slog.Int("actions_total", registry.Count()),
	}, cfg.LogAttrs()...)

	a.Audit.Startup(context.Background(), attrs...)

	if ignored := a.Policy.IgnoredAllowlistEntries(); len(ignored) > 0 {
		a.Audit.Slog().Warn(
			"allowlist entries were ignored because the active profile does not grant them; "+
				"the allowlist narrows a profile, it cannot widen one",
			slog.Any("ignored", ignored),
			slog.String("profile", string(a.Policy.Profile())),
		)
	}
}

// HealthCheck verifies that the process can reach WHMCS and authenticate.
//
// This is what the container health check runs. A check that only proves the
// runtime can execute a statement reports healthy while the server is unable to
// do anything at all.
func HealthCheck(ctx context.Context, cfg config.Config) error {
	client, err := whmcs.New(cfg.WHMCSConfig())
	if err != nil {
		return err
	}
	// WhmcsDetails is read-only, cheap, and requires a valid credential, so it
	// distinguishes "reachable" from "reachable and authenticated".
	action, ok := registry.Lookup("WhmcsDetails")
	if !ok {
		return fmt.Errorf("registry is missing WhmcsDetails")
	}
	if _, err := client.Call(ctx, action, url.Values{}); err != nil {
		return err
	}
	return nil
}

// Command toc-whmcs-mcp serves the WHMCS Admin API to MCP clients.
//
// Safety is enforced in this process, not delegated to the model or the MCP
// host. See README.md for the security model and openspec/ for the specification.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/app"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/config"
	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "toc-whmcs-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Handled before flag parsing so they work without a valid configuration.
	if slices.Contains(args, "-version") || slices.Contains(args, "--version") {
		fmt.Println(version.String())
		return nil
	}
	healthcheck := slices.Contains(args, "-healthcheck") || slices.Contains(args, "--healthcheck")
	printTools := slices.Contains(args, "-print-tools") || slices.Contains(args, "--print-tools")
	args = slices.DeleteFunc(args, func(a string) bool {
		switch a {
		case "-healthcheck", "--healthcheck", "-print-tools", "--print-tools":
			return true
		default:
			return false
		}
	})

	cfg, err := config.Load(args)
	if err != nil {
		return err
	}

	if healthcheck {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.HealthCheck(ctx, cfg); err != nil {
			return fmt.Errorf("health check failed: %w", err)
		}
		fmt.Println("ok")
		return nil
	}

	// Audit and diagnostics go to stderr. Under the stdio transport, stdout is
	// the MCP channel and a stray byte there corrupts the protocol.
	a, err := app.Build(cfg, os.Stderr)
	if err != nil {
		return err
	}

	// An operator should be able to see what a deployment would expose without
	// attaching an MCP client to it.
	if printTools {
		fmt.Printf("profile:             %s\n", cfg.Profile)
		fmt.Printf("destructive enabled: %t\n", cfg.AllowDestructive)
		fmt.Printf("actions permitted:   %d\n", len(a.Policy.PermittedActions()))
		fmt.Printf("tools advertised:    %d\n\n", len(a.Tools))
		for _, name := range a.Tools {
			fmt.Println("  " + name)
		}
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cfg.Transport {
	case config.TransportHTTP:
		return serveHTTP(ctx, a, cfg)
	default:
		return serveStdio(ctx, a)
	}
}

func serveStdio(ctx context.Context, a *app.App) error {
	stdio := server.NewStdioServer(a.Server)
	// Listen returns when the context is cancelled, so a SIGTERM ends the
	// session rather than killing an in-flight billing call mid-write.
	if err := stdio.Listen(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func serveHTTP(ctx context.Context, a *app.App, cfg config.Config) error {
	streamable := server.NewStreamableHTTPServer(a.Server)

	handler := http.Handler(streamable)
	if cfg.AuthToken != "" {
		handler = requireBearer(cfg.AuthToken, handler)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		a.Audit.Slog().Info("serving MCP over streamable http", "addr", cfg.Addr, "authenticated", cfg.AuthToken != "")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// requireBearer rejects requests without the configured token.
//
// This is a coarse gate in front of a control plane for a billing system, not
// an identity layer. Deployments that need per-user attribution should front
// this with something that provides it.
func requireBearer(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="toc-whmcs-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

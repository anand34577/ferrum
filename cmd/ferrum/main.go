// Command ferrum runs the single-binary Ferrum server: REST API, console
// WebSocket proxy, and the embedded React frontend, all on one port.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ferrum/internal/api"
	"ferrum/internal/auth"
	"ferrum/internal/config"
	"ferrum/internal/connections"
	"ferrum/internal/poller"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
	"ferrum/web"
)

// version, commit, and date are set at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X main.version=v1.2.3 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// See scripts/build.sh / scripts/build.ps1, which set these for release
// binaries. Left at their defaults for plain `go build`/`go run`.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	dev := flag.Bool("dev", false, "use human-readable logs instead of JSON")
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit 0/1 (for container HEALTHCHECK)")
	showVersion := flag.Bool("version", false, "print version information and exit")
	logFile := flag.String("log-file", "", "append logs to this file instead of stderr — stderr is invisible under the Windows Service Control Manager, which doesn't capture it the way systemd/journald does")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ferrum %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	logger, closeLog, err := newLogger(*dev, *logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "opening log file: %v\n", err)
		os.Exit(1)
	}
	defer closeLog()
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	if *healthcheck {
		addr := cfg.Server.Addr
		if i := strings.LastIndex(addr, ":"); i >= 0 {
			addr = "127.0.0.1" + addr[i:]
		} else {
			addr = "127.0.0.1:" + addr
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + addr + "/api/v1/health")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		resp.Body.Close()
		os.Exit(0)
	}

	run := func(ctx context.Context) { runServer(ctx, cfg) }

	// Under the Windows Service Control Manager there's no console to
	// deliver os.Interrupt/SIGTERM, so the SCM's own stop/shutdown request
	// drives ctx cancellation instead. On every other platform (and when
	// running interactively on Windows) this is a no-op and we fall
	// through to the normal signal-driven path — systemd's SIGTERM on
	// Linux included.
	if runAsWindowsService(run) {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run(ctx)
}

// runServer opens the database, wires up the API server, and serves until
// ctx is canceled, then shuts down gracefully. Fatal setup errors log and
// exit the process directly (there's no partially-started server to unwind).
func runServer(ctx context.Context, cfg config.Config) {
	if cfg.Secret == "" {
		secretPath := cfg.DB.Path
		if cfg.DB.Driver == "postgres" {
			secretPath = "./data/ferrum.db" // no local DB file to sit next to; use the conventional data dir instead
		}
		secret, err := ensurePersistedSecret(secretPath)
		if err != nil {
			slog.Error("app secret", "error", err)
			os.Exit(1)
		}
		cfg.Secret = secret
	}

	db, err := store.Open(cfg.DB)
	if err != nil {
		slog.Error("opening database", "driver", cfg.DB.Driver, "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("database ready", "driver", cfg.DB.Driver)

	secretBox, err := secrets.New(cfg.Secret)
	if err != nil {
		slog.Error("initializing secret box", "error", err)
		os.Exit(1)
	}

	authSvc := auth.NewService(db)
	srv := api.New(db, authSvc, secretBox, api.ServerOptions{
		SecureCookies: cfg.Server.SecureCookies,
		BehindProxy:   cfg.Server.BehindProxy,
	})

	if cfg.OIDC.Enabled {
		srv.SetOIDC(auth.NewOIDCClient(auth.OIDCConfig{
			DisplayName:  cfg.OIDC.DisplayName,
			IssuerURL:    cfg.OIDC.IssuerURL,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.OIDC.RedirectURL,
		}))
		slog.Info("SSO enabled", "issuer", cfg.OIDC.IssuerURL)
	}

	// ctx governs shutdown (process signals normally; the Windows SCM's stop
	// request when running as a service). The poller derives from it so it
	// stops — rather than polling through — the graceful-shutdown window.
	pollerCtx, stopPoller := context.WithCancel(ctx)
	defer stopPoller()
	go poller.NewAlertEvaluator(db, connections.New(db, secretBox)).Run(pollerCtx, 60*time.Second)

	distFS, err := web.DistFS()
	if err != nil {
		slog.Error("loading embedded frontend", "error", err)
		os.Exit(1)
	}
	srv.SetWebFS(distFS)

	httpServer := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		slog.Info("ferrum listening", "addr", cfg.Server.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
}

// newLogger builds the default logger and returns a cleanup func that closes
// the log file, if one was opened (a no-op when logging to stderr).
func newLogger(dev bool, logFile string) (*slog.Logger, func(), error) {
	out := io.Writer(os.Stderr)
	closeLog := func() {}
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, err
		}
		out = f
		closeLog = func() { f.Close() }
	}

	if dev {
		return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug})), closeLog, nil
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo})), closeLog, nil
}

// ensurePersistedSecret generates (or reuses) a random app secret stored
// next to the database, so a fresh install doesn't need any manual config
// but restarts don't invalidate encrypted connection credentials.
func ensurePersistedSecret(dbPath string) (string, error) {
	secretPath := dbPath + ".secret"
	if data, err := os.ReadFile(secretPath); err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data)), nil
	} else if err != nil && !os.IsNotExist(err) {
		// An unreadable secret file (permissions, corruption) must never be
		// silently replaced: regenerating would permanently invalidate every
		// encrypted connection credential in the database.
		return "", fmt.Errorf("reading app secret %s: %w", secretPath, err)
	}

	if dir := dirOf(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("creating data directory %s: %w", dir, err)
		}
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating app secret: %w", err)
	}
	secret := hex.EncodeToString(b)
	if err := os.WriteFile(secretPath, []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("persisting app secret: %w", err)
	}
	return secret, nil
}

func dirOf(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i < 0 {
		return ""
	}
	return p[:i]
}

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
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"ferrum/internal/api"
	"ferrum/internal/auth"
	"ferrum/internal/config"
	"ferrum/internal/connections"
	"ferrum/internal/digest"
	"ferrum/internal/events"
	"ferrum/internal/notify"
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

// lifecycleSweepInterval is how often the snapshot-retention sweep and
// orphaned-disk check run — a lower-frequency background job than the metric
// alert evaluator, since both passes fetch guest configs and storage content
// across the whole fleet.
const lifecycleSweepInterval = 1 * time.Hour

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

	authSvc := auth.NewService(db, secretBox)
	srv := api.New(db, authSvc, secretBox, api.ServerOptions{
		SecureCookies: cfg.Server.SecureCookies,
		BehindProxy:   cfg.Server.BehindProxy,
		NeedleBinPath: cfg.NeedleBinPath,
	})
	defer srv.Close()

	// OIDC and notification (Gotify/SMTP) settings are admin-editable from
	// the Settings UI and live in the database from here on; config.yaml's
	// oidc.* block is only ever used to seed that database row on the very
	// first boot after upgrading, so an existing config.yaml-based deployment
	// keeps working unchanged.
	// eventBus fans out state changes the poller detects (alert
	// triggers/resolutions, connection health flips) to the SSE endpoint and
	// the outgoing webhook dispatcher — see internal/events, internal/api/events.go,
	// and internal/notify/webhooks.go.
	eventBus := events.New()
	srv.SetEventBus(eventBus)

	evaluator := poller.NewAlertEvaluator(db, connections.New(db, secretBox))
	evaluator.SetBus(eventBus)
	srv.SetAlertEvaluator(evaluator)

	webhookDispatcher := notify.NewWebhookDispatcher(db, secretBox)
	srv.SetWebhookDispatcher(webhookDispatcher)

	digestScheduler := digest.NewScheduler(db, connections.New(db, secretBox))
	srv.SetDigestScheduler(digestScheduler)

	if err := srv.BootstrapSettings(ctx, cfg.OIDC.Enabled, auth.OIDCConfig{
		DisplayName:  cfg.OIDC.DisplayName,
		IssuerURL:    cfg.OIDC.IssuerURL,
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		RedirectURL:  cfg.OIDC.RedirectURL,
	}); err != nil {
		slog.Error("loading OIDC/notification settings", "error", err)
		os.Exit(1)
	}
	evaluator.SetNotifier(srv.Notifier())
	digestScheduler.SetNotifier(srv.Notifier())

	// ctx governs shutdown (process signals normally; the Windows SCM's stop
	// request when running as a service). The poller derives from it so it
	// stops — rather than polling through — the graceful-shutdown window.
	pollerCtx, stopPoller := context.WithCancel(ctx)
	defer stopPoller()
	// wg tracks these four background loops so shutdown can wait for them to
	// actually return before the deferred db.Close()/srv.Close() run —
	// canceling pollerCtx only asks them to stop; without this wait, a poll
	// tick still in flight when shutdown proceeds keeps issuing queries
	// against a database (and server resources) that are already closing.
	var wg sync.WaitGroup
	runLoop := func(fn func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(pollerCtx)
		}()
	}
	runLoop(func(ctx context.Context) { evaluator.Run(ctx, srv.AlertPollInterval(ctx)) })
	runLoop(func(ctx context.Context) { webhookDispatcher.Run(ctx, eventBus) })

	// Snapshot retention sweep + orphaned-disk check — read-heavy and slower
	// moving than the metric alert evaluator, so it runs on its own longer
	// interval rather than sharing AlertPollInterval.
	lifecycleEvaluator := poller.NewLifecycleEvaluator(db, connections.New(db, secretBox))
	runLoop(func(ctx context.Context) { lifecycleEvaluator.Run(ctx, lifecycleSweepInterval) })

	runLoop(digestScheduler.Run)

	distFS, err := web.DistFS()
	if err != nil {
		slog.Error("loading embedded frontend", "error", err)
		os.Exit(1)
	}
	srv.SetWebFS(distFS)

	// Shutdown doesn't cancel in-flight request contexts, so an open SSE
	// stream or AI chat would hold it for its full budget. Cancel a shared
	// base context the moment Shutdown starts so those handlers return.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	httpServer := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           srv.Router(),
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	httpServer.RegisterOnShutdown(cancelBase)

	go func() {
		var err error
		if cfg.Server.TLSCertFile != "" {
			slog.Info("ferrum listening (tls)", "addr", cfg.Server.Addr)
			err = httpServer.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		} else {
			slog.Info("ferrum listening", "addr", cfg.Server.Addr)
			err = httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
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
		_ = httpServer.Close()
	}

	// Stop the background loops and wait for their current iteration to
	// actually return before this function's own defers (db.Close,
	// srv.Close) run — see the wg comment above for why.
	stopPoller()
	// 5s on top of httpServer.Shutdown's own 10s budget above — kept under
	// 15s total so this always finishes within the Windows service wrapper's
	// own 15s stop deadline (service_windows.go).
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWait()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-waitCtx.Done():
		slog.Warn("background loops did not stop within the shutdown deadline")
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

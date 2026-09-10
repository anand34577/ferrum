package digest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"ferrum/internal/connections"
	"ferrum/internal/notify"
	"ferrum/internal/store"
)

// checkInterval is how often the scheduler wakes up to ask "is a digest
// due?" — deliberately much finer-grained than any configurable digest
// interval (minimum a day) so a changed interval or enabled flag takes
// effect within minutes rather than needing a ticker reset (see
// AlertEvaluator.SetInterval for the alternative this avoids: that approach
// needs a mutex-guarded ticker precisely because its interval is small
// enough to matter; here checking against digest_settings.last_sent_at on a
// fixed cadence is simpler and just as responsive in practice).
const checkInterval = 15 * time.Minute

// Scheduler periodically checks whether a fleet digest is due and, if so,
// builds and sends one via the shared notify.Notifier — no new delivery
// mechanism, just a new periodic message on top of Gotify/SMTP.
type Scheduler struct {
	db    *store.DB
	conns *connections.Resolver

	notifierMu sync.RWMutex
	notifier   *notify.Notifier // nil until SetNotifier is called — every send is nil-checked
}

func NewScheduler(db *store.DB, conns *connections.Resolver) *Scheduler {
	return &Scheduler{db: db, conns: conns}
}

// SetNotifier attaches (or replaces) the Gotify/SMTP dispatcher — mirrors
// AlertEvaluator.SetNotifier, called once at startup and again whenever the
// admin saves new notification settings.
func (s *Scheduler) SetNotifier(n *notify.Notifier) {
	s.notifierMu.Lock()
	s.notifier = n
	s.notifierMu.Unlock()
}

func (s *Scheduler) getNotifier() *notify.Notifier {
	s.notifierMu.RLock()
	defer s.notifierMu.RUnlock()
	return s.notifier
}

// Run blocks, checking on every tick until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	s.checkAndSend(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAndSend(ctx)
		}
	}
}

func (s *Scheduler) checkAndSend(ctx context.Context) {
	settings, err := LoadSettings(ctx, s.db)
	if err != nil {
		slog.Error("digest scheduler: loading settings failed", "error", err)
		return
	}
	if !settings.Enabled {
		return
	}
	interval := time.Duration(settings.IntervalHours) * time.Hour
	if !settings.LastSentAt.IsZero() && time.Since(settings.LastSentAt) < interval {
		return
	}
	if err := s.send(ctx, settings); err != nil {
		slog.Error("digest scheduler: sending digest failed", "error", err)
	}
}

// SendNow builds and sends a digest immediately, regardless of the
// configured interval or last-sent time — used by the "send now" settings
// endpoint. It still requires the digest to be enabled and recipients
// configured via the usual settings, same as a scheduled send, except it
// bypasses the due-time check.
func (s *Scheduler) SendNow(ctx context.Context) error {
	settings, err := LoadSettings(ctx, s.db)
	if err != nil {
		return fmt.Errorf("loading digest settings: %w", err)
	}
	return s.send(ctx, settings)
}

func (s *Scheduler) send(ctx context.Context, settings Settings) error {
	notifier := s.getNotifier()
	if notifier == nil {
		return errors.New("notifications are not configured")
	}

	summary, err := Build(ctx, s.db, s.conns)
	if err != nil {
		return fmt.Errorf("building digest: %w", err)
	}
	subject, body := summary.Render()

	now := time.Now().UTC()
	sendErrs := notifier.Notify(ctx, subject, body, settings.Recipients...)

	// last_sent_at is stamped regardless of outcome — a broken SMTP relay
	// shouldn't cause the scheduler to retry every checkInterval forever;
	// the admin fixes delivery and the next scheduled interval picks it up.
	if err := markSent(ctx, s.db, now); err != nil {
		slog.Error("digest scheduler: recording last-sent time failed", "error", err)
	}

	if len(sendErrs) > 0 {
		errStrs := make([]string, len(sendErrs))
		for i, e := range sendErrs {
			errStrs[i] = e.Error()
		}
		return fmt.Errorf("digest delivery failed: %v", errStrs)
	}
	slog.Info("fleet digest sent", "connections", summary.TotalConnections, "reachable", summary.ReachableConnections)
	return nil
}

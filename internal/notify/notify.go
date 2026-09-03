// Package notify sends outbound alert notifications to Gotify and/or SMTP —
// both optional and independently enabled. Everything here is stdlib-only
// (net/http, net/smtp): neither channel needs a third-party client.
package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GotifyConfig struct {
	Enabled bool
	URL     string // base URL, e.g. https://gotify.example.com
	Token   string // application token
}

type SMTPConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       string // comma/semicolon-separated recipients
	UseTLS   bool   // STARTTLS on 25/587; implicit TLS is used automatically on port 465
}

type Settings struct {
	Gotify GotifyConfig
	SMTP   SMTPConfig
}

// Notifier holds the live, admin-editable settings and dispatches
// notifications to every enabled channel. Safe for concurrent use — Update
// is called from the settings HTTP handler while Notify runs from the
// background alert evaluator.
type Notifier struct {
	mu       sync.RWMutex
	settings Settings
	client   *http.Client
}

func New(s Settings) *Notifier {
	return &Notifier{settings: s, client: &http.Client{Timeout: 10 * time.Second}}
}

func (n *Notifier) Update(s Settings) {
	n.mu.Lock()
	n.settings = s
	n.mu.Unlock()
}

func (n *Notifier) Settings() Settings {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.settings
}

// Notify sends title/message to every enabled channel. Failures are logged
// by the caller (via the returned errors), never raised as a panic or
// blocking retry — a notification backend being down must never affect
// alert evaluation itself.
//
// extraEmails are per-user opt-in recipients (a user's own account email,
// see user_preferences.notify_email) added on top of — never instead of —
// the admin's globally configured SMTP "to" list. They're silently ignored
// when SMTP itself isn't enabled: opting in to "email me" doesn't stand up
// a relay on its own.
func (n *Notifier) Notify(ctx context.Context, title, message string, extraEmails ...string) []error {
	s := n.Settings()
	var errs []error
	if s.Gotify.Enabled {
		if err := sendGotify(ctx, n.client, s.Gotify, title, message); err != nil {
			errs = append(errs, fmt.Errorf("gotify: %w", err))
		}
	}
	if s.SMTP.Enabled {
		cfg := s.SMTP
		if len(extraEmails) > 0 {
			cfg.To = strings.Join(append(splitRecipients(cfg.To), extraEmails...), ",")
		}
		if err := sendSMTP(cfg, title, message); err != nil {
			errs = append(errs, fmt.Errorf("smtp: %w", err))
		}
	}
	return errs
}

// TestGotify and TestSMTP send a fixed test message through one channel
// using caller-supplied (not-yet-saved) config — the "Send test
// notification" button in the settings UI.
func TestGotify(ctx context.Context, cfg GotifyConfig) error {
	client := &http.Client{Timeout: 10 * time.Second}
	return sendGotify(ctx, client, cfg, "Ferrum test notification", "This is a test notification from Ferrum. If you received this, Gotify is configured correctly.")
}

func TestSMTP(cfg SMTPConfig) error {
	return sendSMTP(cfg, "Ferrum test notification", "This is a test notification from Ferrum. If you received this, SMTP is configured correctly.")
}

func sendGotify(ctx context.Context, client *http.Client, cfg GotifyConfig, title, message string) error {
	if cfg.URL == "" || cfg.Token == "" {
		return errors.New("gotify URL and token are required")
	}
	endpoint := strings.TrimRight(cfg.URL, "/") + "/message?token=" + url.QueryEscape(cfg.Token)
	form := url.Values{"title": {title}, "message": {message}, "priority": {"5"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling gotify: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("gotify returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func sendSMTP(cfg SMTPConfig, subject, body string) error {
	if cfg.Host == "" || cfg.From == "" || cfg.To == "" {
		return errors.New("smtp host, from, and to are required")
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	to := splitRecipients(cfg.To)
	msg := buildMessage(cfg.From, to, subject, body)

	// Port 465 is implicit TLS (the connection is TLS from the first byte,
	// no STARTTLS negotiation) — dialed by hand below. For everything else,
	// cfg.UseTLS picks the transport explicitly rather than going through
	// net/smtp.SendMail: SendMail opportunistically attempts STARTTLS
	// whenever the server *advertises* the extension, regardless of what
	// the caller asked for — so an admin who deliberately unchecked "Use
	// STARTTLS" (an internal relay with a self-signed or expired cert that
	// still advertises STARTTLS) got the send fail on a TLS handshake they
	// never asked for. sendPlain below never attempts TLS at all; only the
	// UseTLS branch does, and only then.
	if cfg.Port == 465 {
		return sendImplicitTLS(addr, cfg.Host, tlsAuth(cfg), cfg.From, to, msg)
	}
	if cfg.UseTLS {
		return sendStartTLS(addr, cfg.Host, tlsAuth(cfg), cfg.From, to, msg)
	}
	return sendPlain(addr, cfg, to, msg)
}

// tlsAuth is smtp.PlainAuth for the two TLS-protected send paths — its
// stdlib TLS-required guard is exactly what should apply there.
func tlsAuth(cfg SMTPConfig) smtp.Auth {
	if cfg.Username == "" {
		return nil
	}
	return smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
}

// sendPlain sends over a bare, never-upgraded connection — no STARTTLS
// attempt regardless of what the server advertises, honoring an explicit
// "don't use TLS" choice instead of net/smtp.SendMail's opportunistic
// upgrade. Authenticates via unencryptedPlainAuth rather than
// smtp.PlainAuth: the stdlib version refuses to send credentials over any
// connection that isn't TLS or literally localhost, which would otherwise
// make "no TLS" and "have a username" mutually exclusive — the admin
// choosing both explicitly (a trusted internal network) is not for Ferrum
// to second-guess.
func sendPlain(addr string, cfg SMTPConfig, to []string, msg []byte) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("dialing: %w", err)
	}
	defer c.Close()
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = &unencryptedPlainAuth{username: cfg.Username, password: cfg.Password}
	}
	return sendViaClient(c, auth, cfg.From, to, msg)
}

// unencryptedPlainAuth is net/smtp's PLAIN mechanism without the
// TLS-or-localhost requirement smtp.PlainAuth enforces — see sendPlain.
type unencryptedPlainAuth struct {
	username, password string
}

func (a *unencryptedPlainAuth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (a *unencryptedPlainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("smtp: unexpected server challenge for PLAIN auth")
	}
	return nil, nil
}

func sendImplicitTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("dialing (implicit TLS): %w", err)
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("starting SMTP session: %w", err)
	}
	defer c.Close()
	return sendViaClient(c, auth, from, to, msg)
}

// sendStartTLS mirrors smtp.SendMail but requires the STARTTLS upgrade to
// succeed rather than silently falling back to plaintext auth, since the
// user explicitly asked for TLS.
func sendStartTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("dialing: %w", err)
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("server does not support STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
		return fmt.Errorf("STARTTLS: %w", err)
	}
	return sendViaClient(c, auth, from, to, msg)
}

func sendViaClient(c *smtp.Client, auth smtp.Auth, from string, to []string, msg []byte) error {
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("authenticating: %w", err)
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func splitRecipients(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func buildMessage(from string, to []string, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

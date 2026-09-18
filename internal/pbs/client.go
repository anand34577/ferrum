// Package pbs is a Proxmox Backup Server REST API client. It mirrors the
// shape of internal/pve — a shared transport in this file, methods for one
// API area per additional file — so a PBS remote can be registered and
// operated the same way a PVE cluster connection is.
package pbs

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxResponseBytes bounds upstream response bodies, same rationale as
// pve.maxResponseBytes.
const maxResponseBytes = 32 << 20 // 32 MiB

// ErrUnauthorized reports that PBS rejected our credentials; do() wraps 401
// responses in it, and callers can detect that case with
// errors.Is(err, ErrUnauthorized).
var ErrUnauthorized = errors.New("pbs: unauthorized")

// PBSResponseTooLargeError reports an upstream response body that exceeded
// maxResponseBytes. The body is deliberately truncated at the limit instead
// of exhausting memory, but surfaced as this explicit error so callers never
// see the confusing "unexpected end of JSON input" that json.Unmarshal
// produces on a half-read body. Same rationale as pve.ResponseTooLargeError.
type PBSResponseTooLargeError struct {
	Method string
	Path   string
	Limit  int
}

func (e *PBSResponseTooLargeError) Error() string {
	return fmt.Sprintf("pbs %s %s response exceeded %d MiB limit", e.Method, e.Path, e.Limit>>20)
}

// StatusError is a non-2xx upstream PBS response.
type StatusError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("pbs %s %s failed (%d): %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// Message extracts a clean, user-facing error string from PBS's JSON error
// body ({"data":null,"message":"..."} or an errors map), falling back to the
// raw body when it isn't that shape.
func (e *StatusError) Message() string {
	var parsed struct {
		Message string            `json:"message"`
		Errors  map[string]string `json:"errors"`
	}
	if err := json.Unmarshal([]byte(e.Body), &parsed); err != nil {
		return e.Body
	}
	var parts []string
	if parsed.Message != "" {
		parts = append(parts, strings.TrimSuffix(parsed.Message, "\n"))
	}
	for k, v := range parsed.Errors {
		parts = append(parts, fmt.Sprintf("%s: %s", k, v))
	}
	if len(parts) == 0 {
		return e.Body
	}
	return strings.Join(parts, "; ")
}

// StatusCodeOf reports the upstream HTTP status carried by err (0 if err
// isn't an upstream status failure).
func StatusCodeOf(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.StatusCode
	}
	return 0
}

// NotAvailableError marks an upstream feature the connected PBS doesn't
// implement — namespaces arrived in PBS 2.2, and older servers answer the
// namespace endpoints with HTTP 501 or a 400/500 complaint naming an
// "unknown parameter" / "no such method" (see isNotAvailable). Callers treat
// it as "no data", not as a failure: ListNamespaces degrades to an empty
// listing instead of failing the whole datastore page.
type NotAvailableError struct {
	Method string
	Path   string
	Err    error
}

func (e *NotAvailableError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("pbs %s %s not available", e.Method, e.Path)
}

func (e *NotAvailableError) Unwrap() error { return e.Err }

// IsNotAvailable reports whether err (or its chain) is a PBS feature that
// isn't implemented on the connected server.
func IsNotAvailable(err error) bool {
	var nae *NotAvailableError
	return errors.As(err, &nae)
}

// Client talks to a single Proxmox Backup Server host.
type Client struct {
	baseURL string
	// httpClient carries the default 30s whole-request timeout; longClient
	// has none (bounded by the caller's ctx) for the synchronous calls that
	// legitimately run for minutes — see Prune.
	httpClient *http.Client
	longClient *http.Client

	// skipVerify / fingerprint select the TLS transport, applied once in
	// New after all Options ran (applyTransport) so option order can't
	// produce a half-configured client. fingerprint wins: a pinned
	// certificate replaces the CA chain as the trust root.
	skipVerify  bool
	fingerprint string // normalized: lowercase hex, no colons

	// API-token auth (preferred): "PBSAPIToken=user@realm!tokenid:secret".
	apiTokenHeader string

	// Ticket auth (username/password): filled in by Login.
	ticket string
	csrf   string
}

type Option func(*Client)

// insecureTransport is shared by every skip-verify client, same rationale as
// pve.insecureTransport — one connection pool instead of a fresh TLS
// handshake per token-auth request.
var insecureTransport = func() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-controlled per-connection trust setting
	return tr
}()

// WithInsecureSkipVerify opts out of upstream certificate verification (the
// out-of-the-box PBS host ships a self-signed cert). Applied in New via
// applyTransport; a configured fingerprint wins over it.
func WithInsecureSkipVerify(skip bool) Option {
	return func(c *Client) {
		c.skipVerify = skip
	}
}

// WithFingerprint pins the server's TLS certificate: only a connection that
// presents a certificate whose SHA-256 fingerprint matches fp (lowercase
// hex, colons optional — the value `openssl s_client` / PBS's node cert view
// shows) is accepted, instead of trusting the system CA pool. Empty fp is a
// no-op.
func WithFingerprint(fp string) Option {
	return func(c *Client) {
		if normalized := normalizeFingerprint(fp); normalized != "" {
			c.fingerprint = normalized
		}
	}
}

// applyTransport selects the TLS transport from skipVerify/fingerprint.
// Called once from New, after every Option, so the options' relative order
// never matters. A pinned client gets its own transport: its TLS config
// carries the per-client pin check.
func (c *Client) applyTransport() {
	switch {
	case c.fingerprint != "":
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{
			// The pin check below is the whole trust decision: chain and
			// hostname validation are skipped exactly like skip-verify, but
			// any certificate not matching the configured fingerprint is
			// rejected before a request is sent.
			InsecureSkipVerify:    true, //nolint:gosec // deliberate: fingerprint pinning replaces CA validation
			VerifyPeerCertificate: c.verifyFingerprint,
		}
		c.httpClient.Transport = tr
		c.longClient.Transport = tr
	case c.skipVerify:
		c.httpClient.Transport = insecureTransport
		c.longClient.Transport = insecureTransport
	}
}

// verifyFingerprint is the VerifyPeerCertificate hook for pinned clients:
// it compares the SHA-256 of the leaf certificate's DER (rawCerts[0]) against
// the configured fingerprint (see fingerprintMatches). certFingerprintSHA256
// yields the same value `openssl x509 -fingerprint -sha256` prints. Chain
// verification never ran in pinned mode (see applyTransport), so
// verifiedChains is always empty here.
func (c *Client) verifyFingerprint(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return errors.New("pbs: server presented no certificate to pin against")
	}
	got := certFingerprintSHA256(rawCerts[0])
	if !fingerprintMatches(c.fingerprint, got) {
		return fmt.Errorf("pbs: TLS certificate fingerprint mismatch — server presented %s, expected %s (as configured on the connection); update the fingerprint or remove it", got, c.fingerprint)
	}
	return nil
}

// normalizeFingerprint folds a fingerprint into lowercase hex without
// separators, so "AA:BB:…" and "aabb…" compare equal.
func normalizeFingerprint(fp string) string {
	return strings.ToLower(strings.ReplaceAll(fp, ":", ""))
}

// certFingerprintSHA256 returns the SHA-256 digest of a DER-encoded
// certificate as lowercase hex — the value `openssl x509 -fingerprint
// -sha256` shows, minus the colons.
func certFingerprintSHA256(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// fingerprintMatches reports whether a configured pin matches a server
// certificate fingerprint; both sides may carry colons and mixed case. An
// empty configured pin never matches — pinning is opt-in.
func fingerprintMatches(configured, got string) bool {
	configured = normalizeFingerprint(configured)
	return configured != "" && configured == normalizeFingerprint(got)
}

// New creates a client for a PBS host reachable at host:port (default 8007).
func New(host string, port int, opts ...Option) *Client {
	c := &Client{
		baseURL: fmt.Sprintf("https://%s:%d/api2/json", host, port),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		// Prune is a synchronous POST whose duration scales with the backup
		// group's snapshot count — minutes on a big group. It carries its
		// own context deadline instead (see pruneTimeout).
		longClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.applyTransport()
	return c
}

// WithAPIToken configures token-based auth (no login step required).
// tokenID is "user@realm!tokenname"; PBS's header form separates the secret
// with a colon rather than PVE's "=".
func (c *Client) WithAPIToken(tokenID, secret string) *Client {
	c.apiTokenHeader = fmt.Sprintf("PBSAPIToken=%s:%s", tokenID, secret)
	return c
}

// Login authenticates with username/password and stores the resulting
// ticket + CSRF token for subsequent requests. PBS requires the realm suffix
// on the username (user@pbs, root@pam) — a bare "root" would otherwise fail
// upstream with a generic 401 that hides what's wrong.
//
// Note: PBS one-time-password logins send "password" as "secret:totp:<code>"
// on the same endpoint; that form is out of scope here — interactive OTP is
// not something a dashboard client does.
func (c *Client) Login(ctx context.Context, username, password string) error {
	if !strings.Contains(username, "@") {
		return fmt.Errorf("login needs a username with its realm — use user@pbs (standard users) or root@pam (root), got %q", username)
	}
	form := url.Values{"username": {username}, "password": {password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/access/ticket", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("login failed (%d): %s", resp.StatusCode, string(body))
	}

	var out struct {
		Data struct {
			Ticket              string `json:"ticket"`
			CSRFPreventionToken string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return err
	}
	c.ticket = out.Data.Ticket
	c.csrf = out.Data.CSRFPreventionToken
	return nil
}

func (c *Client) authenticate(req *http.Request) {
	if c.apiTokenHeader != "" {
		req.Header.Set("Authorization", c.apiTokenHeader)
		return
	}
	if c.ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PBSAuthCookie", Value: c.ticket})
		if req.Method != http.MethodGet {
			req.Header.Set("CSRFPreventionToken", c.csrf)
		}
	}
}

// PathEscape escapes a single URL path segment (datastore name, namespace,
// backup id, ...) so a crafted value can't add or traverse path segments in
// the upstream PBS URL.
func PathEscape(s string) string {
	return url.PathEscape(s)
}

// do issues an authenticated request on the default 30s-timeout client.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, form url.Values, out any) error {
	return c.doOn(ctx, c.httpClient, method, path, query, form, out)
}

// postLong is post() against the longClient — for the POSTs PBS only answers
// after real work (prune), which can outlive the 30s whole-request timeout.
func (c *Client) postLong(ctx context.Context, path string, form url.Values, out any) error {
	return c.doOn(ctx, c.longClient, http.MethodPost, path, nil, form, out)
}

// doOn is do() against an explicit HTTP client — longClient for calls that
// legitimately run for minutes, bounded by the caller's ctx instead of
// httpClient's 30s whole-request timeout.
func (c *Client) doOn(ctx context.Context, hc *http.Client, method, path string, query url.Values, form url.Values, out any) error {
	full := c.baseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	var reqBody io.Reader
	if form != nil {
		reqBody = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, full, reqBody)
	if err != nil {
		return err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c.authenticate(req)

	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxResponseBytes {
		return &PBSResponseTooLargeError{Method: method, Path: path, Limit: maxResponseBytes}
	}
	if resp.StatusCode >= 300 {
		err := &StatusError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(raw)}
		if resp.StatusCode == http.StatusUnauthorized {
			return errors.Join(ErrUnauthorized, err)
		}
		if isNotAvailable(resp.StatusCode, string(raw)) {
			return &NotAvailableError{Method: method, Path: path, Err: err}
		}
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// isNotAvailable classifies upstream responses that mean "this PBS server
// doesn't have that feature" rather than "the request failed". PBS answers
// 501 for endpoints its version doesn't implement; older 2.x servers
// (namespaces arrived in PBS 2.2) reject the namespace routes / ns query
// parameter with a 400/500 body naming an unknown parameter or method. A
// 404 is deliberately NOT classified: it's also what a mistyped datastore
// name produces, and that must stay a visible error.
func isNotAvailable(status int, body string) bool {
	if status == http.StatusNotImplemented {
		return true
	}
	return (status == http.StatusBadRequest || status == http.StatusInternalServerError) &&
		(strings.Contains(body, "unknown parameter") || strings.Contains(body, "no such method"))
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}
func (c *Client) post(ctx context.Context, path string, form url.Values, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, form, out)
}
func (c *Client) put(ctx context.Context, path string, form url.Values, out any) error {
	return c.do(ctx, http.MethodPut, path, nil, form, out)
}
func (c *Client) delete(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodDelete, path, query, nil, out)
}

// Version reports the PBS server's version string, and doubles as a
// connectivity/credential check (mirrors pve.Client.Version's role in
// testConnection).
type Version struct {
	Version string `json:"version"`
	Release string `json:"release"`
	RepoID  string `json:"repoid,omitempty"`
}

func (c *Client) Version(ctx context.Context) (*Version, error) {
	var out struct {
		Data Version `json:"data"`
	}
	if err := c.get(ctx, "/version", nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

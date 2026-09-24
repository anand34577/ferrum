// Package pve is a Proxmox VE REST API client. Each file groups the methods
// for one API area (guests, nodes, storage, firewall, ha, replication,
// backup) around the shared transport defined here.
package pve

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
	"sort"
	"strings"
	"sync"
	"time"
)

// maxResponseBytes bounds upstream response bodies so a misbehaving PVE
// host (or a response smuggling bug) can't exhaust server memory.
const maxResponseBytes = 32 << 20 // 32 MiB — RRD blobs and task logs stay far below this

// ErrUnauthorized reports that PVE rejected our credentials (expired ticket
// or bad token); do() wraps 401 responses in it, and callers can detect that
// case with errors.Is(err, ErrUnauthorized) to invalidate cached sessions.
var ErrUnauthorized = errors.New("pve: unauthorized")

// ResponseTooLargeError reports an upstream response body that exceeded
// maxResponseBytes. The body is deliberately truncated at the limit instead
// of exhausting memory, but surfaced as this explicit error so callers never
// see the confusing "unexpected end of JSON input" that json.Unmarshal
// produces on a half-read body.
type ResponseTooLargeError struct {
	Method string
	Path   string
	Limit  int
}

func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("pve %s %s response exceeded %d MiB limit", e.Method, e.Path, e.Limit>>20)
}

// NotAvailableError marks an upstream feature that isn't present on the
// target server — Ceph not installed ("binary not installed:
// /usr/bin/ceph-mon") or an endpoint this PVE version doesn't implement
// (HTTP 501 "Method ... not implemented"). Callers treat it as "no data",
// not as a failure: pollers shouldn't log it as an error and handlers
// should return an empty/204 response.
type NotAvailableError struct {
	Method string
	Path   string
	Err    error
}

func (e *NotAvailableError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("pve %s %s not available", e.Method, e.Path)
}

func (e *NotAvailableError) Unwrap() error { return e.Err }

// IsNotAvailable reports whether err (or its chain) is a PVE feature that
// isn't installed / implemented on the target server.
func IsNotAvailable(err error) bool {
	var nae *NotAvailableError
	return errors.As(err, &nae)
}

// StatusError is a non-2xx upstream PVE response.
type StatusError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("pve %s %s failed (%d): %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// Message returns a clean, user-facing error string extracted from PVE's
// JSON error body — {"data":null,"errors":{"vmid":"value does not match
// the regex pattern"}} becomes "vmid: value does not match the regex
// pattern" instead of the raw blob. Falls back to e.Body verbatim when the
// body isn't PVE's structured error shape (e.g. a proxy/gateway error page).
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
	// Sort keys for a stable message across calls with the same error set.
	keys := make([]string, 0, len(parsed.Errors))
	for k := range parsed.Errors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, parsed.Errors[k]))
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

// Client talks to a single Proxmox VE host or cluster entrypoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
	// streamClient has no whole-request timeout — see New.
	streamClient *http.Client

	// skipVerify / fingerprint select the TLS transport, applied once in
	// New after all Options ran (applyTransport) so option order can't
	// produce a half-configured client. fingerprint wins: a pinned
	// certificate replaces the CA chain as the trust root.
	skipVerify  bool
	fingerprint string // normalized: lowercase hex, no colons

	// API-token auth (preferred): "PVEAPIToken=user@realm!tokenid=secret"
	apiTokenHeader string

	// authMu guards the ticket-auth fields below: Login writes them (once
	// per login) while any number of concurrent requests read them through
	// authenticate/WSAuth on the shared cached client.
	authMu sync.Mutex
	ticket string
	csrf   string
}

type Option func(*Client)

// insecureTransport is shared by every skip-verify client. Cloning
// DefaultTransport per Client (the old behaviour) gave each one its own
// empty connection pool, so a fresh TLS handshake ran on every single
// upstream call for token-auth connections — which build a Client per
// request. One transport means one pool, reused across connections.
var insecureTransport = func() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-controlled per-connection trust setting
	return tr
}()

func WithInsecureSkipVerify(skip bool) Option {
	return func(c *Client) {
		c.skipVerify = skip
	}
}

// WithFingerprint pins the server's TLS certificate: only a connection that
// presents a certificate whose SHA-256 fingerprint matches fp (lowercase
// hex, colons optional — the value `openssl s_client` / pvenode cert show)
// is accepted, instead of trusting the system CA pool. Empty fp is a no-op.
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
		c.streamClient.Transport = tr
	case c.skipVerify:
		c.httpClient.Transport = insecureTransport
		c.streamClient.Transport = insecureTransport
	}
}

// WSTLSConfig returns the TLS config a raw WebSocket dial to this host must
// use — the same trust decision as the REST transport (pin, skip-verify, or
// system CA), so the console proxy can't bypass a configured pin.
func (c *Client) WSTLSConfig() *tls.Config {
	switch {
	case c.fingerprint != "":
		return &tls.Config{InsecureSkipVerify: true, VerifyPeerCertificate: c.verifyFingerprint} //nolint:gosec // pin replaces CA validation
	case c.skipVerify:
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // per-connection trust setting
	}
	return &tls.Config{}
}

// verifyFingerprint is the VerifyPeerCertificate hook for pinned clients:
// it compares the SHA-256 of the leaf certificate's DER (rawCerts[0]) against
// the configured fingerprint (see fingerprintMatches). certFingerprintSHA256
// yields the same value PVE prints for its node certificates. Chain
// verification never ran in pinned mode (see applyTransport), so
// verifiedChains is always empty here.
func (c *Client) verifyFingerprint(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return errors.New("pve: server presented no certificate to pin against")
	}
	got := certFingerprintSHA256(rawCerts[0])
	if !fingerprintMatches(c.fingerprint, got) {
		return fmt.Errorf("pve: TLS certificate fingerprint mismatch — server presented %s, expected %s (as configured on the connection); update the fingerprint or remove it", got, c.fingerprint)
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

// New creates a client for a Proxmox host reachable at host:port.
func New(host string, port int, opts ...Option) *Client {
	c := &Client{
		baseURL:    fmt.Sprintf("https://%s:%d/api2/json", host, port),
		httpClient: &http.Client{Timeout: 15 * time.Second},
		// Bulk transfers (ISO/template uploads, file-restore downloads) are
		// bounded by their request context, not by a fixed whole-request
		// deadline: a 4 GiB ISO can never finish inside httpClient's 15s.
		streamClient: &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.applyTransport()
	return c
}

// WithAPIToken configures token-based auth (no login step required).
func (c *Client) WithAPIToken(tokenID, secret string) *Client {
	c.apiTokenHeader = fmt.Sprintf("PVEAPIToken=%s=%s", tokenID, secret)
	return c
}

// Login authenticates with username/password and stores the resulting
// ticket + CSRF token for subsequent requests.
func (c *Client) Login(ctx context.Context, username, password string) error {
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
	c.authMu.Lock()
	c.ticket = out.Data.Ticket
	c.csrf = out.Data.CSRFPreventionToken
	c.authMu.Unlock()
	return nil
}

// isNotAvailable classifies upstream responses that mean "this server
// doesn't have that feature" rather than "the request failed": PVE answers
// 501 for endpoints its version doesn't implement, and 500 with a
// "binary not installed" message for Ceph endpoints on hosts without
// Ceph (the response body names the missing binary, e.g. /usr/bin/ceph-mon).
func isNotAvailable(status int, body string) bool {
	if status == http.StatusNotImplemented {
		return true
	}
	return status == http.StatusInternalServerError && strings.Contains(body, "not installed")
}

func (c *Client) do(ctx context.Context, method, path string, body url.Values, out any) error {
	return c.doOn(ctx, c.httpClient, method, path, body, out)
}

// doOn is do() against an explicit HTTP client — the streamClient for the
// rare calls that legitimately outlive httpClient's 15s whole-request
// timeout (cluster join blocks until the joining node has synced corosync
// state, which can take minutes).
func (c *Client) doOn(ctx context.Context, hc *http.Client, method, path string, body url.Values, out any) error {
	var reqBody io.Reader
	if body != nil {
		reqBody = strings.NewReader(body.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
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
		return &ResponseTooLargeError{Method: method, Path: path, Limit: maxResponseBytes}
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
		// A 2xx with an empty body (some PVE endpoints, e.g. the journal
		// endpoint on certain configurations, do this when there's nothing
		// to report) isn't malformed JSON — leave out at its zero value
		// instead of failing json.Unmarshal's "unexpected end of JSON input".
		return nil
	}
	return json.Unmarshal(raw, out)
}

// PostMultipart issues an authenticated POST with an arbitrary body and
// content-type (a multipart/form-data file upload) instead of do()'s
// url.Values-encoded form — used for storage content uploads (ISOs,
// container templates) where the payload is a file stream, not form fields.
func (c *Client) PostMultipart(ctx context.Context, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	c.authenticate(req)

	resp, err := c.streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxResponseBytes {
		return &ResponseTooLargeError{Method: http.MethodPost, Path: path, Limit: maxResponseBytes}
	}
	if resp.StatusCode >= 300 {
		err := &StatusError{Method: http.MethodPost, Path: path, StatusCode: resp.StatusCode, Body: string(raw)}
		if resp.StatusCode == http.StatusUnauthorized {
			return errors.Join(ErrUnauthorized, err)
		}
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// StreamGet issues an authenticated GET and hands back the raw response for
// the caller to stream (e.g. proxying a file-restore download) rather than
// buffering it into memory like do() — the caller MUST close the response
// body. path is relative to /api2/json, same as every other method here.
func (c *Client) StreamGet(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	c.authenticate(req)
	resp, err := c.streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := &StatusError{Method: http.MethodGet, Path: path, StatusCode: resp.StatusCode, Body: string(raw)}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, errors.Join(ErrUnauthorized, err)
		}
		return nil, err
	}
	return resp, nil
}

// WSAuth returns the HTTP header/value pair needed to authenticate a raw
// WebSocket dial (console proxy) against this client's credentials — either
// the API token Authorization header, or the PVEAuthCookie ticket.
func (c *Client) WSAuth() (headerKey, headerVal string, cookieName, cookieVal string) {
	if c.apiTokenHeader != "" {
		return "Authorization", c.apiTokenHeader, "", ""
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return "", "", "PVEAuthCookie", c.ticket
}

func (c *Client) authenticate(req *http.Request) {
	if c.apiTokenHeader != "" {
		req.Header.Set("Authorization", c.apiTokenHeader)
		return
	}
	c.authMu.Lock()
	ticket, csrf := c.ticket, c.csrf
	c.authMu.Unlock()
	if ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
		if req.Method != http.MethodGet {
			req.Header.Set("CSRFPreventionToken", csrf)
		}
	}
}

// PathEscape escapes a single URL path segment (node, storage, vmid,
// volid, ...) so a crafted value can't add or traverse path segments in the
// upstream PVE URL.
func PathEscape(s string) string {
	return url.PathEscape(s)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}
func (c *Client) post(ctx context.Context, path string, form url.Values, out any) error {
	return c.do(ctx, http.MethodPost, path, form, out)
}
func (c *Client) put(ctx context.Context, path string, form url.Values, out any) error {
	return c.do(ctx, http.MethodPut, path, form, out)
}
// delete issues a DELETE. Proxmox's API rejects any request body on DELETE
// ("Unexpected content for method 'DELETE'"), unlike POST/PUT, so params go
// on the query string instead of through do()'s form-body encoding.
func (c *Client) delete(ctx context.Context, path string, form url.Values, out any) error {
	if len(form) > 0 {
		path += "?" + form.Encode()
	}
	return c.do(ctx, http.MethodDelete, path, nil, out)
}

// --- Version & cluster-wide endpoints ---

type Version struct {
	Version string `json:"version"`
	Release string `json:"release"`
}

func (c *Client) Version(ctx context.Context) (*Version, error) {
	var out struct {
		Data Version `json:"data"`
	}
	if err := c.get(ctx, "/version", &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// ClusterResource is the flattened row shape returned by /cluster/resources,
// covering nodes, qemu/lxc guests, storage, and pools in one call.
type ClusterResource struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"` // "node" | "qemu" | "lxc" | "storage" | "pool"
	Node    string  `json:"node"`
	VMID    int     `json:"vmid,omitempty"`
	Name    string  `json:"name,omitempty"`
	Status  string  `json:"status,omitempty"`
	CPU     float64 `json:"cpu,omitempty"`
	MaxCPU  int     `json:"maxcpu,omitempty"`
	Mem     int64   `json:"mem,omitempty"`
	MaxMem  int64   `json:"maxmem,omitempty"`
	Disk    int64   `json:"disk,omitempty"`
	MaxDisk int64   `json:"maxdisk,omitempty"`
	Uptime  int64   `json:"uptime,omitempty"`
	Storage string  `json:"storage,omitempty"`
	Tags    string  `json:"tags,omitempty"`
	PoolID  string  `json:"pool,omitempty"`

	// Populated for type=="storage" rows only.
	PluginType string `json:"plugintype,omitempty"` // "dir" | "nfs" | "cifs" | "zfspool" | "lvmthin" | "rbd" (Ceph) | ...
	Shared     int    `json:"shared,omitempty"`     // 1 if this storage is shared across every node in the cluster

	// Populated for type=="qemu"/"lxc" rows: the guest's HA state, when a
	// resource manager (HA) is watching it — "started" | "stopped" |
	// "error" | "fence" | "" (not HA-managed).
	HAState string `json:"hastate,omitempty"`

	// 1 when the qemu row is a VM template rather than a runnable guest.
	Template int `json:"template,omitempty"`
}

func (c *Client) ClusterResources(ctx context.Context) ([]ClusterResource, error) {
	var out struct {
		Data []ClusterResource `json:"data"`
	}
	if err := c.get(ctx, "/cluster/resources", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

type ClusterStatus struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // "cluster" | "node"
	Name    string `json:"name,omitempty"`
	Version int    `json:"version,omitempty"`
	Quorate int    `json:"quorate,omitempty"`
	Nodeid  int    `json:"nodeid,omitempty"`
	IP      string `json:"ip,omitempty"`
	Online  int    `json:"online,omitempty"`
	Local   int    `json:"local,omitempty"`
}

func (c *Client) ClusterStatus(ctx context.Context) ([]ClusterStatus, error) {
	var out struct {
		Data []ClusterStatus `json:"data"`
	}
	if err := c.get(ctx, "/cluster/status", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ClusterLogEntry is one line from the cluster-wide syslog feed (/cluster/log).
type ClusterLogEntry struct {
	Node string `json:"node"`
	Msg  string `json:"msg"`
	PID  int    `json:"pid"`
	Tag  string `json:"tag"`
	UID  int    `json:"uid"`
	// Time (unix seconds) and Pri (syslog priority, 0=emerg..7=debug) come
	// back from /cluster/log on every row; dropping them left the feed with
	// no way to say when a line happened or how bad it was.
	Time int64  `json:"time"`
	Pri  int    `json:"pri"`
	User string `json:"user,omitempty"`
}

func (c *Client) ClusterLog(ctx context.Context, limit int) ([]ClusterLogEntry, error) {
	q := ""
	if limit > 0 {
		q = fmt.Sprintf("?max=%d", limit)
	}
	var out struct {
		Data []ClusterLogEntry `json:"data"`
	}
	if err := c.get(ctx, "/cluster/log"+q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

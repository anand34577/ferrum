// Package pbs is a Proxmox Backup Server REST API client. It mirrors the
// shape of internal/pve — a shared transport in this file, methods for one
// API area per additional file — so a PBS remote can be registered and
// operated the same way a PVE cluster connection is.
package pbs

import (
	"context"
	"crypto/tls"
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

// ErrUnauthorized reports that PBS rejected our credentials.
var ErrUnauthorized = errors.New("pbs: unauthorized")

// IsUnauthorized reports whether err (or its chain) is a PBS auth rejection.
func IsUnauthorized(err error) bool { return errors.Is(err, ErrUnauthorized) }

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

// Client talks to a single Proxmox Backup Server host.
type Client struct {
	baseURL    string
	httpClient *http.Client

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

func WithInsecureSkipVerify(skip bool) Option {
	return func(c *Client) {
		if !skip {
			return
		}
		c.httpClient.Transport = insecureTransport
	}
}

// New creates a client for a PBS host reachable at host:port (default 8007).
func New(host string, port int, opts ...Option) *Client {
	c := &Client{
		baseURL:    fmt.Sprintf("https://%s:%d/api2/json", host, port),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
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

func (c *Client) do(ctx context.Context, method, path string, query url.Values, form url.Values, out any) error {
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		err := &StatusError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(raw)}
		if resp.StatusCode == http.StatusUnauthorized {
			return errors.Join(ErrUnauthorized, err)
		}
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
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

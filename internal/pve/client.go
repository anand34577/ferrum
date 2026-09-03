// Package pve is a Proxmox VE REST API client. Each file groups the methods
// for one API area (guests, nodes, storage, firewall, ha, replication,
// backup) around the shared transport defined here.
package pve

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

// maxResponseBytes bounds upstream response bodies so a misbehaving PVE
// host (or a response smuggling bug) can't exhaust server memory.
const maxResponseBytes = 32 << 20 // 32 MiB — RRD blobs and task logs stay far below this

// ErrUnauthorized reports that PVE rejected our credentials (expired ticket
// or bad token); callers use it to invalidate cached sessions.
var ErrUnauthorized = errors.New("pve: unauthorized")

// IsUnauthorized reports whether err (or its chain) is a PVE auth rejection.
func IsUnauthorized(err error) bool { return errors.Is(err, ErrUnauthorized) }

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

	// API-token auth (preferred): "PVEAPIToken=user@realm!tokenid=secret"
	apiTokenHeader string

	// Ticket auth (username/password): filled in by Login. A Client is not
	// safe for concurrent use while a Login is in flight; the connections
	// resolver serializes Login per connection.
	ticket string
	csrf   string
}

type Option func(*Client)

func WithInsecureSkipVerify(skip bool) Option {
	return func(c *Client) {
		if !skip {
			return // keep the default transport (proxy support, HTTP/2, pool tuning)
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-controlled per-connection trust setting
		c.httpClient.Transport = tr
	}
}

// New creates a client for a Proxmox host reachable at host:port.
func New(host string, port int, opts ...Option) *Client {
	c := &Client{
		baseURL:    fmt.Sprintf("https://%s:%d/api2/json", host, port),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
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
	c.ticket = out.Data.Ticket
	c.csrf = out.Data.CSRFPreventionToken
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
		if isNotAvailable(resp.StatusCode, string(raw)) {
			return &NotAvailableError{Method: method, Path: path, Err: err}
		}
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// WSAuth returns the HTTP header/value pair needed to authenticate a raw
// WebSocket dial (console proxy) against this client's credentials — either
// the API token Authorization header, or the PVEAuthCookie ticket.
func (c *Client) WSAuth() (headerKey, headerVal string, cookieName, cookieVal string) {
	if c.apiTokenHeader != "" {
		return "Authorization", c.apiTokenHeader, "", ""
	}
	return "", "", "PVEAuthCookie", c.ticket
}

func (c *Client) authenticate(req *http.Request) {
	if c.apiTokenHeader != "" {
		req.Header.Set("Authorization", c.apiTokenHeader)
		return
	}
	if c.ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: c.ticket})
		if req.Method != http.MethodGet {
			req.Header.Set("CSRFPreventionToken", c.csrf)
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
func (c *Client) delete(ctx context.Context, path string, form url.Values, out any) error {
	return c.do(ctx, http.MethodDelete, path, form, out)
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

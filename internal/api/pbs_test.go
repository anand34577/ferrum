package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ferrum/internal/pbs"
	"ferrum/internal/pve"
)

type errString string

func (e errString) Error() string { return string(e) }

// TestWriteErrorUpstreamMapping locks the error-mapping contract: an
// upstream 4xx keeps its status and actionable message (e.g. PBS's "bad
// prune options"), an upstream 5xx stays sanitized to the generic message
// (bodies can be proxy error pages or stack traces), and an upstream 401
// never surfaces as a dashboard 401 — the frontend signs the session out on
// any 401, and a rejected PBS ticket must not log the user out of Ferrum.
func TestWriteErrorUpstreamMapping(t *testing.T) {
	var s Server
	badPrune := &pbs.StatusError{
		Method: "POST", Path: "/admin/datastore/x/prune", StatusCode: http.StatusBadRequest,
		Body: `{"data":null,"message":"keep-daily: value does not match the regex pattern"}`,
	}
	internal := &pbs.StatusError{
		Method: "GET", Path: "/admin/datastore/x/status", StatusCode: http.StatusInternalServerError,
		Body: `{"data":null,"message":"thread 'tokio' panicked at ..."}`,
	}

	run := func(name string, status int, err error, wantStatus int, wantContains, wantNotContains string) {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.writeError(rec, status, err)
			if rec.Code != wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, wantStatus, rec.Body.String())
			}
			if wantContains != "" && !strings.Contains(rec.Body.String(), wantContains) {
				t.Fatalf("body %q missing %q", rec.Body.String(), wantContains)
			}
			if wantNotContains != "" && strings.Contains(rec.Body.String(), wantNotContains) {
				t.Fatalf("body %q must not contain %q", rec.Body.String(), wantNotContains)
			}
		})
	}

	run("upstream 400 message passes through with its status", http.StatusBadGateway, badPrune,
		http.StatusBadRequest, "value does not match the regex pattern", "")
	run("upstream 500 stays sanitized at the caller's status", http.StatusBadGateway, internal,
		http.StatusBadGateway, "internal server error", "panicked")
	run("upstream 401 keeps the caller's status (not a session 401)", http.StatusBadGateway,
		&pbs.StatusError{StatusCode: http.StatusUnauthorized, Body: `{"message":"ticket invalid"}`},
		http.StatusBadGateway, "ticket invalid", "")
	run("pve upstream 400 passes through as well", http.StatusBadGateway,
		&pve.StatusError{StatusCode: http.StatusBadRequest, Body: `{"data":null,"errors":{"vmid":"value does not match the regex pattern"}}`},
		http.StatusBadRequest, "vmid: value does not match the regex pattern", "")
	run("local 500 stays sanitized", http.StatusInternalServerError,
		errString("leaky: SELECT * FROM users"),
		http.StatusInternalServerError, "internal server error", "leaky")
	run("local 4xx shows its message", http.StatusBadRequest, errString("backupType and backupId are required"),
		http.StatusBadRequest, "backupType and backupId are required", "")
}

// TestPBSDatastoresRetryOnUnauthorized drives the PBS datastore listing
// through the real router: a cached token-auth client whose upstream starts
// answering 401 must recover within the same request — the resolver drops
// the cached client, pbsCall retries once with a fresh one, and the listing
// succeeds instead of surfacing "unauthorized".
func TestPBSDatastoresRetryOnUnauthorized(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api2/json/admin/datastore":
			calls++
			if calls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"data":null,"message":"ticket invalid"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"store":"backups","comment":"main"}]}`))
		case "/api2/json/admin/datastore/backups/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"total":100,"used":10,"avail":90}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e := newTestEnv(t)
	cookie := e.loginAs(t, "admin", "admin@example.com", "test-password-123", true)

	hostPort := strings.TrimPrefix(srv.URL, "https://")
	host, portStr, _ := strings.Cut(hostPort, ":")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing stub port %q: %v", portStr, err)
	}
	enc, err := e.server.secrets.Encrypt("token-secret")
	if err != nil {
		t.Fatalf("encrypting token secret: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := e.db.Exec(`
		INSERT INTO connections
			(id, name, type, host, port, auth_type, token_id, token_secret_enc, username, password_enc,
			 verify_tls, behind_reverse_proxy, created_at, updated_at)
		VALUES ('conn-pbs-1', 'test-pbs', 'pbs', ?, ?, 'token', 'root@pbs!test', ?, '', '', 0, 0, ?, ?)`,
		host, port, enc, now, now); err != nil {
		t.Fatalf("inserting connection row: %v", err)
	}

	rec := e.get(t, "/api/v1/connections/conn-pbs-1/pbs/datastores", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("datastores status = %d, want 200 after 401 retry (body %s)", rec.Code, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if calls < 2 {
		t.Fatalf("expected the first listing call to 401 and a retried second one, got calls=%d", calls)
	}
	if !strings.Contains(rec.Body.String(), `"store":"backups"`) {
		t.Fatalf("listing body missing the store row: %s", rec.Body.String())
	}
}

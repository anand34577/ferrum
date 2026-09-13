package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ferrum/internal/config"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

// newTestResolver opens a real SQLite database in a temp dir (migrations run
// on Open) and wires a Resolver against a secrets.Box derived from a fixed
// test secret.
func newTestResolver(t *testing.T) (*store.DB, *secrets.Box, *Resolver) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	db, err := store.Open(config.DBConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := secrets.New("test-app-secret-for-connections")
	if err != nil {
		t.Fatalf("secrets.New: %v", err)
	}
	return db, box, New(db, box)
}

// connRow describes the connection row a test wants, with plaintext secrets;
// insertConnRow encrypts them through the same Box the Resolver decrypts with.
// connType defaults to "pve" when left empty.
type connRow struct {
	id       string
	connType string
	authType string
	tokenID  string
	tokenSec string
	username string
	password string
}

func insertConnRow(t *testing.T, db *store.DB, box *secrets.Box, host string, port int, row connRow) {
	t.Helper()
	var tokenID, tokenEnc, username, passwordEnc string
	switch row.authType {
	case "token":
		tokenID = row.tokenID
		enc, err := box.Encrypt(row.tokenSec)
		if err != nil {
			t.Fatalf("encrypting token secret: %v", err)
		}
		tokenEnc = enc
	case "password":
		username = row.username
		enc, err := box.Encrypt(row.password)
		if err != nil {
			t.Fatalf("encrypting password: %v", err)
		}
		passwordEnc = enc
	}
	connType := row.connType
	if connType == "" {
		connType = "pve"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// verify_tls=0 so the pve client skips verification of the stub's
	// self-signed certificate, like an out-of-the-box PVE host.
	_, err := db.Exec(`
		INSERT INTO connections
			(id, name, type, host, port, auth_type, token_id, token_secret_enc, username, password_enc,
			 verify_tls, behind_reverse_proxy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?)`,
		row.id, "test-conn-"+row.id, connType, host, port, row.authType, tokenID, tokenEnc, username, passwordEnc, now, now)
	if err != nil {
		t.Fatalf("inserting connection row: %v", err)
	}
}

// stubPVE is a minimal fake PVE API served over TLS: it counts POSTs to
// /api2/json/access/ticket and records the credentials presented by every
// other request, so tests can assert on what actually reached the wire.
type stubPVE struct {
	mu         sync.Mutex
	loginHits  int
	failLogin  bool
	loginDelay time.Duration
	authHeader string // Authorization header on the last data request
	authCookie string // PVEAuthCookie on the last data request
}

// startPVEStub serves s on an httptest TLS server and returns the host/port
// to store in a connections row pointing at it.
func startPVEStub(t *testing.T, s *stubPVE) (host string, port int) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			s.mu.Lock()
			s.loginHits++
			fail, delay := s.failLogin, s.loginDelay
			s.mu.Unlock()
			if delay > 0 {
				time.Sleep(delay)
			}
			if fail {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"data":null}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"ticket":"PVE:test-ticket","CSRFPreventionToken":"CSRF:test-csrf"}}`))
		case "/api2/json/version":
			s.mu.Lock()
			s.authHeader = r.Header.Get("Authorization")
			if ck, err := r.Cookie("PVEAuthCookie"); err == nil {
				s.authCookie = ck.Value
			}
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"version":"8.2.4","release":"8.2"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	hostPort := strings.TrimPrefix(srv.URL, "https://")
	host, portStr, _ := strings.Cut(hostPort, ":")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing stub port %q: %v", portStr, err)
	}
	return host, port
}

func (s *stubPVE) logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loginHits
}

func (s *stubPVE) lastAuthHeader() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authHeader
}

// ClientFor must hand back a client that actually talks to the stored
// host/port, authenticating API-token requests with the PVEAPIToken header
// built from the row's decrypted secret — and token auth must never log in.
func TestClientForTokenAuth(t *testing.T) {
	db, box, res := newTestResolver(t)
	var s stubPVE
	host, port := startPVEStub(t, &s)
	insertConnRow(t, db, box, host, port, connRow{
		id: "conn-token", authType: "token",
		tokenID: "root@pam!testtoken", tokenSec: "token-secret-123",
	})

	c, err := res.ClientFor(context.Background(), "conn-token")
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if got := s.logins(); got != 0 {
		t.Errorf("token auth hit the login endpoint %d times, want 0", got)
	}

	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version through resolved client: %v", err)
	}
	if v.Version != "8.2.4" {
		t.Errorf("version = %q, want 8.2.4", v.Version)
	}
	want := "PVEAPIToken=root@pam!testtoken=token-secret-123"
	if got := s.lastAuthHeader(); got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

// Sequential ClientFor calls must reuse the cached client: one login for
// password auth no matter how many calls, none for token auth.
func TestClientForReusesCache(t *testing.T) {
	ctx := context.Background()

	t.Run("password auth logs in exactly once", func(t *testing.T) {
		db, box, res := newTestResolver(t)
		var s stubPVE
		host, port := startPVEStub(t, &s)
		insertConnRow(t, db, box, host, port, connRow{
			id: "conn-pw", authType: "password", username: "root@pam", password: "hunter2!",
		})

		for i := 0; i < 3; i++ {
			if _, err := res.ClientFor(ctx, "conn-pw"); err != nil {
				t.Fatalf("ClientFor #%d: %v", i+1, err)
			}
		}
		if got := s.logins(); got != 1 {
			t.Errorf("login endpoint hit %d times across 3 ClientFor calls, want 1", got)
		}
	})

	t.Run("token auth never logs in", func(t *testing.T) {
		db, box, res := newTestResolver(t)
		var s stubPVE
		host, port := startPVEStub(t, &s)
		insertConnRow(t, db, box, host, port, connRow{
			id: "conn-tok", authType: "token", tokenID: "root@pam!t", tokenSec: "s3cret",
		})

		for i := 0; i < 2; i++ {
			if _, err := res.ClientFor(ctx, "conn-tok"); err != nil {
				t.Fatalf("ClientFor #%d: %v", i+1, err)
			}
		}
		if got := s.logins(); got != 0 {
			t.Errorf("token auth hit the login endpoint %d times, want 0", got)
		}
	})
}

// Invalidate drops the cached ticket so the next ClientFor re-authenticates.
func TestInvalidateForcesNewLogin(t *testing.T) {
	db, box, res := newTestResolver(t)
	var s stubPVE
	host, port := startPVEStub(t, &s)
	insertConnRow(t, db, box, host, port, connRow{
		id: "conn-pw", authType: "password", username: "root@pam", password: "hunter2!",
	})
	ctx := context.Background()

	if _, err := res.ClientFor(ctx, "conn-pw"); err != nil {
		t.Fatalf("first ClientFor: %v", err)
	}
	if got := s.logins(); got != 1 {
		t.Fatalf("logins after first ClientFor = %d, want 1", got)
	}

	res.Invalidate("conn-pw")

	if _, err := res.ClientFor(ctx, "conn-pw"); err != nil {
		t.Fatalf("ClientFor after Invalidate: %v", err)
	}
	if got := s.logins(); got != 2 {
		t.Errorf("logins after Invalidate = %d, want 2", got)
	}
}

// A rejecting login endpoint surfaces the error and must not poison the
// cache: the next ClientFor retries the login instead of reusing the failure.
func TestFailedLoginNotCached(t *testing.T) {
	db, box, res := newTestResolver(t)
	var s stubPVE
	s.failLogin = true
	host, port := startPVEStub(t, &s)
	insertConnRow(t, db, box, host, port, connRow{
		id: "conn-bad", authType: "password", username: "root@pam", password: "wrong-password",
	})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, err := res.ClientFor(ctx, "conn-bad")
		if err == nil {
			t.Fatalf("ClientFor #%d: expected a login error, got nil", i+1)
		}
		if !strings.Contains(err.Error(), "login failed") {
			t.Errorf("ClientFor #%d error = %v, want it to mention the failed login", i+1, err)
		}
	}
	if got := s.logins(); got != 2 {
		t.Errorf("login endpoint hit %d times, want 2 (a failed login must not be cached)", got)
	}
}

func TestUnknownAuthTypeRejected(t *testing.T) {
	db, box, res := newTestResolver(t)
	var s stubPVE
	host, port := startPVEStub(t, &s)
	insertConnRow(t, db, box, host, port, connRow{id: "conn-weird", authType: "kerberos"})

	_, err := res.ClientFor(context.Background(), "conn-weird")
	if err == nil {
		t.Fatal("expected an error for an unknown auth_type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown auth type") {
		t.Errorf("error = %v, want it to mention the unknown auth type", err)
	}
	if got := s.logins(); got != 0 {
		t.Errorf("login endpoint hit %d times for an unknown auth type, want 0", got)
	}
}

// TargetCredentials is the remote-migration path: password-auth connections
// must be refused (PVE remote-migrate only accepts token auth), token-auth
// connections yield host/port/tokenID and the decrypted secret.
func TestTargetCredentials(t *testing.T) {
	ctx := context.Background()

	t.Run("password auth rejected", func(t *testing.T) {
		db, box, res := newTestResolver(t)
		var s stubPVE
		host, port := startPVEStub(t, &s)
		insertConnRow(t, db, box, host, port, connRow{
			id: "conn-pw", authType: "password", username: "root@pam", password: "hunter2!",
		})

		h, p, _, _, err := res.TargetCredentials(ctx, "conn-pw")
		if err == nil {
			t.Fatalf("TargetCredentials on a password connection = %s:%d, want an error", h, p)
		}
		if !strings.Contains(err.Error(), "API token") {
			t.Errorf("error = %v, want it to mention API token auth", err)
		}
	})

	t.Run("token auth returns decrypted credentials", func(t *testing.T) {
		db, box, res := newTestResolver(t)
		var s stubPVE
		host, port := startPVEStub(t, &s)
		insertConnRow(t, db, box, host, port, connRow{
			id: "conn-tok", authType: "token",
			tokenID: "root@pam!testtoken", tokenSec: "token-secret-123",
		})

		h, p, tokenID, secret, err := res.TargetCredentials(ctx, "conn-tok")
		if err != nil {
			t.Fatalf("TargetCredentials: %v", err)
		}
		if h != host || p != port {
			t.Errorf("host/port = %s:%d, want %s:%d", h, p, host, port)
		}
		if tokenID != "root@pam!testtoken" {
			t.Errorf("tokenID = %q", tokenID)
		}
		if secret != "token-secret-123" {
			t.Errorf("secret = %q, want the decrypted token secret", secret)
		}
	})
}

// A dozen dashboard widgets resolving the same fresh connection must
// collapse into one login via singleflight — the login endpoint sees exactly
// one request.
func TestConcurrentClientForSingleLogin(t *testing.T) {
	db, box, res := newTestResolver(t)
	var s stubPVE
	// Widen the in-flight window so every goroutine piles into the same
	// singleflight call; latecomers would hit the cache and still count as 1.
	s.loginDelay = 150 * time.Millisecond
	host, port := startPVEStub(t, &s)
	insertConnRow(t, db, box, host, port, connRow{
		id: "conn-pw", authType: "password", username: "root@pam", password: "hunter2!",
	})
	ctx := context.Background()

	const callers = 8
	start := make(chan struct{})
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = res.ClientFor(ctx, "conn-pw")
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	if got := s.logins(); got != 1 {
		t.Errorf("login endpoint received %d requests across %d concurrent callers, want 1", got, callers)
	}
}

// --- PBS resolver ---

// stubPBS is a minimal fake PBS API served over TLS, mirroring stubPVE:
// it counts POSTs to /api2/json/access/ticket and records the credentials
// presented by data requests (GET /api2/json/admin/datastore), so tests can
// assert on what actually reached the wire.
type stubPBS struct {
	mu         sync.Mutex
	loginHits  int
	authHeader string // Authorization header on the last data request
	authCookie string // PBSAuthCookie on the last data request
}

// startPBSStub serves s on an httptest TLS server and returns the host/port
// to store in a connections row pointing at it.
func startPBSStub(t *testing.T, s *stubPBS) (host string, port int) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/access/ticket":
			s.mu.Lock()
			s.loginHits++
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"ticket":"PBS:test-ticket","CSRFPreventionToken":"PBS:csrf"}}`))
		case r.URL.Path == "/api2/json/admin/datastore":
			s.mu.Lock()
			s.authHeader = r.Header.Get("Authorization")
			if ck, err := r.Cookie("PBSAuthCookie"); err == nil {
				s.authCookie = ck.Value
			}
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"store":"tank","total":1000,"used":250,"avail":750}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	hostPort := strings.TrimPrefix(srv.URL, "https://")
	host, portStr, _ := strings.Cut(hostPort, ":")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing stub port %q: %v", portStr, err)
	}
	return host, port
}

func (s *stubPBS) logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loginHits
}

func (s *stubPBS) lastAuthHeader() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authHeader
}

func (s *stubPBS) lastAuthCookie() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authCookie
}

// PBSClientFor must hand back a client that talks to the stored PBS
// host/port, authenticating with the PBSAPIToken header built from the
// row's decrypted secret, without ever logging in — and cache the client
// across calls.
func TestPBSClientForTokenAuth(t *testing.T) {
	db, box, res := newTestResolver(t)
	var s stubPBS
	host, port := startPBSStub(t, &s)
	insertConnRow(t, db, box, host, port, connRow{
		id: "pbs-token", connType: "pbs", authType: "token",
		tokenID: "root@pbs!testtoken", tokenSec: "token-secret-123",
	})

	c1, err := res.PBSClientFor(context.Background(), "pbs-token")
	if err != nil {
		t.Fatalf("PBSClientFor: %v", err)
	}
	c2, err := res.PBSClientFor(context.Background(), "pbs-token")
	if err != nil {
		t.Fatalf("second PBSClientFor: %v", err)
	}
	if c1 != c2 {
		t.Error("repeated PBSClientFor calls returned different clients; cache not used")
	}
	if got := s.logins(); got != 0 {
		t.Errorf("token auth hit the login endpoint %d times, want 0", got)
	}

	stores, err := c1.ListDatastores(context.Background())
	if err != nil {
		t.Fatalf("ListDatastores through resolved client: %v", err)
	}
	if len(stores) != 1 || stores[0].Store != "tank" {
		t.Errorf("datastores = %+v, want one store \"tank\"", stores)
	}
	want := "PBSAPIToken=root@pbs!testtoken:token-secret-123"
	if got := s.lastAuthHeader(); got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

// Password-auth PBS connections log in once, reuse the ticket cookie for
// data requests, and re-login after Invalidate drops the cache.
func TestPBSClientForPasswordAuthCachesAndReloginsAfterInvalidate(t *testing.T) {
	db, box, res := newTestResolver(t)
	var s stubPBS
	host, port := startPBSStub(t, &s)
	insertConnRow(t, db, box, host, port, connRow{
		id: "pbs-pw", connType: "pbs", authType: "password", username: "root@pbs", password: "hunter2!",
	})
	ctx := context.Background()

	c, err := res.PBSClientFor(ctx, "pbs-pw")
	if err != nil {
		t.Fatalf("first PBSClientFor: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := res.PBSClientFor(ctx, "pbs-pw"); err != nil {
			t.Fatalf("PBSClientFor #%d: %v", i+1, err)
		}
	}
	if got := s.logins(); got != 1 {
		t.Errorf("login endpoint hit %d times across 3 PBSClientFor calls, want 1", got)
	}

	stores, err := c.ListDatastores(ctx)
	if err != nil {
		t.Fatalf("ListDatastores: %v", err)
	}
	if len(stores) != 1 {
		t.Fatalf("datastores = %+v", stores)
	}
	if got := s.lastAuthCookie(); got != "PBS:test-ticket" {
		t.Errorf("data request PBSAuthCookie = %q, want the login ticket", got)
	}

	res.Invalidate("pbs-pw")
	if _, err := res.PBSClientFor(ctx, "pbs-pw"); err != nil {
		t.Fatalf("PBSClientFor after Invalidate: %v", err)
	}
	if got := s.logins(); got != 2 {
		t.Errorf("logins after Invalidate = %d, want 2", got)
	}
}

// A connection row is one type or the other: PBSClientFor on a pve-typed
// row (and ClientFor on a pbs-typed row) must fail cleanly instead of
// building a client pointed at the wrong kind of server.
func TestWrongConnectionTypeRejected(t *testing.T) {
	db, box, res := newTestResolver(t)
	var s stubPBS
	host, port := startPBSStub(t, &s)
	insertConnRow(t, db, box, host, port, connRow{
		id: "conn-pve", connType: "pve", authType: "token", tokenID: "root@pam!t", tokenSec: "s",
	})
	insertConnRow(t, db, box, host, port, connRow{
		id: "conn-pbs", connType: "pbs", authType: "token", tokenID: "root@pbs!t", tokenSec: "s",
	})

	if _, err := res.PBSClientFor(context.Background(), "conn-pve"); err == nil {
		t.Fatal("PBSClientFor on a pve-typed connection: expected an error, got nil")
	} else if !strings.Contains(err.Error(), "not a PBS connection") {
		t.Errorf("error = %v, want it to mention the wrong connection type", err)
	}
	if got := s.logins(); got != 0 {
		t.Errorf("login endpoint hit %d times for a wrong-type connection, want 0", got)
	}

	if _, err := res.ClientFor(context.Background(), "conn-pbs"); err == nil {
		t.Fatal("ClientFor on a pbs-typed connection: expected an error, got nil")
	} else if !strings.Contains(err.Error(), "not a PVE connection") {
		t.Errorf("error = %v, want it to mention the wrong connection type", err)
	}
}

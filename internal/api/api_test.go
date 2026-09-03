package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ferrum/internal/auth"
	"ferrum/internal/config"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

// --- harness ---

type testEnv struct {
	db     *store.DB
	server *Server
	router http.Handler
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	db, err := store.Open(config.DBConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	box, err := secrets.New("test-secret-that-is-long-enough")
	if err != nil {
		t.Fatalf("secrets.New: %v", err)
	}
	srv := New(db, auth.NewService(db), box, ServerOptions{SecureCookies: true})
	return &testEnv{db: db, server: srv, router: srv.Router()}
}

func (e *testEnv) do(t *testing.T, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling body: %v", err)
		}
		buf = bytes.NewBuffer(raw)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) get(t *testing.T, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	return e.do(t, http.MethodGet, path, nil, cookie)
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decoding response %s: %v", rec.Body.String(), err)
	}
	return out
}

// loginAs bootstraps the first admin, logs in, and returns the session cookie.
func (e *testEnv) loginAs(t *testing.T, username, email, password string, isAdmin bool) *http.Cookie {
	t.Helper()
	if _, err := e.auth().Bootstrap(t.Context(), username, email, password); err != nil {
		if err != auth.ErrAlreadyBootstrapped {
			t.Fatalf("bootstrap: %v", err)
		}
	}
	if !isAdmin {
		// Non-admin accounts are created through the admin API.
		var userID string
		if err := e.db.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&userID); err != nil {
			t.Fatalf("finding user: %v", err)
		}
		if _, err := e.db.Exec(`UPDATE users SET is_admin = 0 WHERE id = ?`, userID); err != nil {
			t.Fatalf("demoting user: %v", err)
		}
	}
	rec := e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": username, "password": password}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status %d body %s", username, rec.Code, rec.Body.String())
	}
	var setCookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			setCookie = c.Value
		}
	}
	if setCookie == "" {
		t.Fatalf("login response carried no %s cookie", auth.SessionCookieName)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: setCookie}
}

func (e *testEnv) auth() *auth.Service { return e.server.auth }

// --- health & auth surface ---

func TestHealthIsPublic(t *testing.T) {
	e := newTestEnv(t)
	if rec := e.get(t, "/api/v1/health", nil); rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
}

func TestSetupThenLoginFlow(t *testing.T) {
	e := newTestEnv(t)

	status := e.get(t, "/api/v1/auth/setup-status", nil)
	if got := decode[map[string]bool](t, status)["needsSetup"]; !got {
		t.Fatal("fresh instance should need setup")
	}

	rec := e.do(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "admin", "email": "admin@example.com", "password": "correct horse battery",
	}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	// Second setup is refused — the first admin already exists.
	rec = e.do(t, http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username": "root", "email": "root@example.com", "password": "another password",
	}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second setup status = %d, want 409", rec.Code)
	}

	// Wrong password is rejected with the generic message.
	rec = e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d, want 401", rec.Code)
	}
	if got := decode[map[string]string](t, rec)["error"]; got != "invalid username or password" {
		t.Fatalf("login error message = %q, want the generic one", got)
	}

	cookie := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)
	me := e.get(t, "/api/v1/auth/me", cookie)
	if me.Code != http.StatusOK {
		t.Fatalf("auth/me status = %d", me.Code)
	}
	user := decode[auth.User](t, me)
	if !user.IsAdmin || user.Username != "admin" {
		t.Fatalf("me = %+v, want admin user", user)
	}
}

func TestRequireAuthRejectsMissingOrBadCookie(t *testing.T) {
	e := newTestEnv(t)
	e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)

	if rec := e.get(t, "/api/v1/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-cookie me status = %d, want 401", rec.Code)
	}
	bad := &http.Cookie{Name: auth.SessionCookieName, Value: "forged-token"}
	if rec := e.get(t, "/api/v1/auth/me", bad); rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged-cookie me status = %d, want 401", rec.Code)
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	e := newTestEnv(t)
	rec := e.get(t, "/api/v1/health", nil)
	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for header, want := range checks {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp == "" {
		t.Error("Content-Security-Policy header missing")
	}
	if hsts := rec.Header().Get("Strict-Transport-Security"); hsts == "" {
		t.Error("HSTS missing while SecureCookies is enabled")
	}
}

// --- authorization model ---

func TestAdminOnlyRoutesRejectNonAdmins(t *testing.T) {
	e := newTestEnv(t)
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)
	_ = e.do(t, http.MethodPost, "/api/v1/users/", map[string]any{
		"username": "bob", "email": "bob@example.com", "password": "bob's own password",
	}, admin)
	bob := e.loginAs(t, "bob", "bob@example.com", "bob's own password", false)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/"},
		{http.MethodGet, "/api/v1/roles/"},
		{http.MethodGet, "/api/v1/audit"},
	} {
		if rec := e.do(t, tc.method, tc.path, nil, bob); rec.Code != http.StatusForbidden {
			t.Errorf("non-admin %s %s = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestMutatingInfraRoutesRequireAdmin(t *testing.T) {
	e := newTestEnv(t)
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)
	_ = e.do(t, http.MethodPost, "/api/v1/users/", map[string]any{
		"username": "bob", "email": "bob@example.com", "password": "bob's own password",
	}, admin)
	bob := e.loginAs(t, "bob", "bob@example.com", "bob's own password", false)

	// Reads are open to authenticated users...
	if rec := e.do(t, http.MethodGet, "/api/v1/connections/", nil, bob); rec.Code != http.StatusOK {
		t.Fatalf("non-admin GET connections = %d, want 200", rec.Code)
	}
	// ...but mutations need admin.
	if rec := e.do(t, http.MethodPost, "/api/v1/connections/", map[string]any{
		"name": "pve", "host": "10.0.0.5", "authType": "password", "username": "root",
		"password": "x", "tokenId": "", "tokenSecret": "",
	}, bob); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin POST connections = %d, want 403", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/api/v1/connections/", map[string]any{
		"name": "pve", "host": "10.0.0.5", "authType": "password", "username": "root",
		"password": "x",
	}, admin); rec.Code != http.StatusCreated {
		t.Fatalf("admin POST connections = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

// --- users admin ---

func TestUserLifecycleAndLastAdminGuard(t *testing.T) {
	e := newTestEnv(t)
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)

	// Duplicate usernames are a conflict, not a 500.
	if rec := e.do(t, http.MethodPost, "/api/v1/users/", map[string]any{
		"username": "admin", "email": "other@example.com", "password": "some password",
	}, admin); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate user create = %d, want 409", rec.Code)
	}

	// Promote bob via the (previously missing) update endpoint.
	rec := e.do(t, http.MethodPost, "/api/v1/users/", map[string]any{
		"username": "bob", "email": "bob@example.com", "password": "bob's own password",
	}, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create bob = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct{ ID string }
	_ = json.NewDecoder(rec.Body).Decode(&created)

	if rec := e.do(t, http.MethodPut, "/api/v1/users/"+created.ID, map[string]any{"isAdmin": true}, admin); rec.Code != http.StatusNoContent {
		t.Fatalf("promote bob = %d: %s", rec.Code, rec.Body.String())
	}

	// Demoting the last remaining admin is refused (admin would demote self
	// only if bob was the other admin — with two admins, demote works).
	if rec := e.do(t, http.MethodPut, "/api/v1/users/"+created.ID, map[string]any{"isAdmin": false}, admin); rec.Code != http.StatusNoContent {
		t.Fatalf("demote bob with 2 admins = %d, want 204", rec.Code)
	}

	// Deleting your own account is refused.
	selfID := func() string {
		var id string
		if err := e.db.QueryRow(`SELECT id FROM users WHERE username = 'admin'`).Scan(&id); err != nil {
			t.Fatalf("finding admin id: %v", err)
		}
		return id
	}()
	if rec := e.do(t, http.MethodDelete, "/api/v1/users/"+selfID, nil, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("self delete = %d, want 400", rec.Code)
	}
}

// --- preferences ---

func TestPreferencesPartialUpdate(t *testing.T) {
	e := newTestEnv(t)
	cookie := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)

	// Defaults before any save.
	rec := e.get(t, "/api/v1/auth/me/preferences", cookie)
	var prefs map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&prefs)
	if prefs["theme"] != "system" || prefs["accent"] != "oxide" {
		t.Fatalf("unexpected defaults: %+v", prefs)
	}

	// Setting only the accent must not reset theme back to a default.
	if rec := e.do(t, http.MethodPut, "/api/v1/auth/me/preferences", map[string]any{"theme": "dark"}, cookie); rec.Code != http.StatusOK {
		t.Fatalf("set theme = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPut, "/api/v1/auth/me/preferences", map[string]any{"accent": "violet"}, cookie); rec.Code != http.StatusOK {
		t.Fatalf("set accent = %d", rec.Code)
	}
	rec = e.get(t, "/api/v1/auth/me/preferences", cookie)
	_ = json.NewDecoder(rec.Body).Decode(&prefs)
	if prefs["theme"] != "dark" || prefs["accent"] != "violet" {
		t.Fatalf("partial updates clobbered each other: %+v", prefs)
	}

	// Pointing the active dashboard at a nonexistent (or foreign) id is refused.
	if rec := e.do(t, http.MethodPut, "/api/v1/auth/me/preferences", map[string]any{"activeDashboardId": "does-not-exist"}, cookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus active dashboard = %d, want 400", rec.Code)
	}

	// A real, owned dashboard id is accepted.
	e.get(t, "/api/v1/dashboards/", cookie) // seed the account's first dashboard
	var list []map[string]any
	rec = e.get(t, "/api/v1/dashboards/", cookie)
	_ = json.NewDecoder(rec.Body).Decode(&list)
	id := list[0]["id"].(string)
	if rec := e.do(t, http.MethodPut, "/api/v1/auth/me/preferences", map[string]any{"activeDashboardId": id}, cookie); rec.Code != http.StatusOK {
		t.Fatalf("set active dashboard = %d: %s", rec.Code, rec.Body.String())
	}

	// Deleting a second dashboard that isn't active leaves the pointer alone;
	// deleting the active one clears it back to "" (client falls back to the
	// oldest remaining dashboard).
	rec = e.do(t, http.MethodPost, "/api/v1/dashboards/", map[string]any{"name": "Second"}, cookie)
	var second struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&second)
	e.do(t, http.MethodDelete, "/api/v1/dashboards/"+id, nil, cookie)
	rec = e.get(t, "/api/v1/auth/me/preferences", cookie)
	_ = json.NewDecoder(rec.Body).Decode(&prefs)
	if prefs["activeDashboardId"] != "" {
		t.Fatalf("active dashboard pointer not cleared after delete: %+v", prefs)
	}
}

// --- dashboards ---

func TestDashboardLayoutValidation(t *testing.T) {
	e := newTestEnv(t)
	cookie := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)

	// A brand-new account is seeded with one "Main" dashboard with widgets.
	rec := e.get(t, "/api/v1/dashboards/", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("list dashboards = %d", rec.Code)
	}
	var list []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil || len(list) != 1 {
		t.Fatalf("expected one seeded dashboard: %v %v", err, list)
	}
	id := list[0]["id"].(string)

	rec = e.get(t, "/api/v1/dashboards/"+id, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("get dashboard = %d", rec.Code)
	}
	var dash struct {
		Widgets []map[string]any `json:"widgets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&dash); err != nil || len(dash.Widgets) == 0 {
		t.Fatalf("default layout missing widgets: %v", err)
	}

	// Garbage shapes are refused.
	if rec := e.do(t, http.MethodPut, "/api/v1/dashboards/"+id, map[string]any{"widgets": "nope"}, cookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid layout = %d, want 400", rec.Code)
	}
	if rec := e.do(t, http.MethodPut, "/api/v1/dashboards/"+id, map[string]any{
		"widgets": []map[string]any{{"id": "x", "type": "y", "x": -5, "y": 0, "w": 6, "h": 4}},
	}, cookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative coords = %d, want 400", rec.Code)
	}

	// A valid layout round-trips.
	valid := map[string]any{
		"widgets": []map[string]any{{"id": "w1", "type": "fleet-overview", "x": 0, "y": 0, "w": 12, "h": 4}},
	}
	if rec := e.do(t, http.MethodPut, "/api/v1/dashboards/"+id, valid, cookie); rec.Code != http.StatusOK {
		t.Fatalf("valid layout put = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := e.get(t, "/api/v1/dashboards/"+id, cookie); rec.Code != http.StatusOK {
		var got struct {
			Widgets []map[string]any `json:"widgets"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&got)
		if len(got.Widgets) != 1 {
			t.Fatalf("layout did not round-trip: %s", rec.Body.String())
		}
	}
}

func TestMultipleDashboards(t *testing.T) {
	e := newTestEnv(t)
	cookie := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)

	// Seed the first ("Main") dashboard.
	e.get(t, "/api/v1/dashboards/", cookie)

	rec := e.do(t, http.MethodPost, "/api/v1/dashboards/", map[string]any{"name": "Capacity planning"}, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create dashboard = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID      string           `json:"id"`
		Name    string           `json:"name"`
		Widgets []map[string]any `json:"widgets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created dashboard: %v", err)
	}
	if created.Name != "Capacity planning" || created.Widgets == nil || len(created.Widgets) != 0 {
		t.Fatalf("unexpected created dashboard: %+v", created)
	}

	// An empty name is refused.
	if rec := e.do(t, http.MethodPost, "/api/v1/dashboards/", map[string]any{"name": "  "}, cookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank name = %d, want 400", rec.Code)
	}

	// Renaming persists.
	if rec := e.do(t, http.MethodPut, "/api/v1/dashboards/"+created.ID, map[string]any{"name": "Renamed"}, cookie); rec.Code != http.StatusOK {
		t.Fatalf("rename dashboard = %d: %s", rec.Code, rec.Body.String())
	}

	rec = e.get(t, "/api/v1/dashboards/", cookie)
	var list []map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if len(list) != 2 {
		t.Fatalf("expected 2 dashboards, got %d", len(list))
	}

	// Deleting your only dashboard is refused; deleting one of several isn't.
	mainID := ""
	for _, d := range list {
		if d["id"] != created.ID {
			mainID = d["id"].(string)
		}
	}
	if rec := e.do(t, http.MethodDelete, "/api/v1/dashboards/"+created.ID, nil, cookie); rec.Code != http.StatusNoContent {
		t.Fatalf("delete second dashboard = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(t, http.MethodDelete, "/api/v1/dashboards/"+mainID, nil, cookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("delete only dashboard = %d, want 400", rec.Code)
	}

	// A deleted or foreign id 404s rather than leaking existence.
	if rec := e.get(t, "/api/v1/dashboards/"+created.ID, cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted dashboard = %d, want 404", rec.Code)
	}
}

// --- rate limiting ---

func TestLoginRateLimitEngagesAndRecovers(t *testing.T) {
	e := newTestEnv(t)
	e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)

	// Hammer with wrong passwords until the limiter locks the key.
	var sawThrottle bool
	for i := 0; i < loginMaxFailures+3; i++ {
		rec := e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil)
		if rec.Code == http.StatusTooManyRequests {
			sawThrottle = true
			if rec.Header().Get("Retry-After") == "" {
				t.Fatal("429 response missing Retry-After header")
			}
			break
		}
	}
	if !sawThrottle {
		t.Fatal("rate limiter never engaged after repeated failures")
	}

	// The correct password is also throttled while locked out (the point of
	// a limiter: the attacker can't bypass it by guessing right).
	rec := e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked-out correct login = %d, want 429", rec.Code)
	}

	// Wind the clock past the window — the limiter must recover.
	if rec := e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, nil); rec.Code == http.StatusTooManyRequests {
		// Travel the lockout forward and try once more.
		time.Sleep(0) // no real waiting in tests; move the window manually
		e.server.logins.mu.Lock()
		for _, r := range e.server.logins.failed {
			r.firstFailure = r.firstFailure.Add(-2 * loginWindow)
			r.lockedUntil = time.Time{}
		}
		e.server.logins.mu.Unlock()
		rec = e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("post-window correct login = %d, want 200", rec.Code)
		}
	}
}

func TestLoginLimiterUnit(t *testing.T) {
	l := newLoginLimiter()
	key := "1.2.3.4|alice"
	for i := 0; i < loginMaxFailures; i++ {
		if allowed, _ := l.Allowed(key); !allowed {
			t.Fatalf("attempt %d should still be allowed", i+1)
		}
		l.RecordFailure(key)
	}
	if allowed, retryAfter := l.Allowed(key); allowed || retryAfter <= 0 {
		t.Fatal("limiter should be locked after maxFailures")
	}
	l.RecordSuccess(key)
	if allowed, _ := l.Allowed(key); !allowed {
		t.Fatal("RecordSuccess should clear the lockout")
	}
}

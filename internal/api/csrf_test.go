package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"ferrum/internal/auth"
	"ferrum/internal/config"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

// doWithHeaders is e.do plus arbitrary extra headers, which the origin-check
// tests need to set (Origin, Sec-Fetch-Site, X-Forwarded-Host).
func (e *testEnv) doWithHeaders(t *testing.T, method, path string, body any, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
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
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// newTestEnvBehindProxy is newTestEnv with BehindProxy enabled, for the
// X-Forwarded-Host host-comparison cases.
func newTestEnvBehindProxy(t *testing.T) *testEnv {
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
	srv := New(db, auth.NewService(db, box), box, ServerOptions{BehindProxy: true})
	return &testEnv{db: db, server: srv, router: srv.Router()}
}

// login with cross-site Origin credentials is refused outright: this is the
// login-CSRF scenario (a forged auto-submitting form on a malicious page),
// which SameSite=Lax does not prevent for top-level navigations. The request
// carries no cookie — exactly what the attack looks like.
func TestOriginCheckBlocksCrossSiteLogin(t *testing.T) {
	e := newTestEnv(t)
	rec := e.doWithHeaders(t, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "whatever"}, nil,
		map[string]string{"Origin": "https://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site login status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if got := decode[map[string]string](t, rec)["error"]; got != "cross-origin request blocked" {
		t.Fatalf("error = %q, want %q", got, "cross-origin request blocked")
	}
}

func TestOriginCheck(t *testing.T) {
	e := newTestEnv(t)
	e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)

	// httptest.NewRequest defaults the request Host to "example.com"; the
	// same-origin Origin must pass the check (a 401 from wrong credentials
	// proves the login handler ran — anything but 403 means allowed).
	sameOrigin := map[string]string{"Origin": "http://example.com"}
	if rec := e.doWithHeaders(t, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "wrong"}, nil, sameOrigin); rec.Code != http.StatusUnauthorized {
		t.Fatalf("same-origin login status = %d, want 401 (origin allowed)", rec.Code)
	}

	// No Origin and no Sec-Fetch-Site — curl / API-key clients — passes.
	if rec := e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("header-less login status = %d, want 401 (origin allowed)", rec.Code)
	}

	// Origin absent but Sec-Fetch-Site says cross-site: blocked.
	if rec := e.doWithHeaders(t, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "wrong"}, nil,
		map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Fatalf("Sec-Fetch-Site cross-site login status = %d, want 403", rec.Code)
	}
	// ...while other Sec-Fetch-Site values (same-origin, same-site) pass.
	if rec := e.doWithHeaders(t, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "wrong"}, nil,
		map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("Sec-Fetch-Site same-origin login status = %d, want 401", rec.Code)
	}

	// GETs are never policed — only mutations are.
	if rec := e.doWithHeaders(t, http.MethodGet, "/api/v1/health", nil, nil,
		map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusOK {
		t.Fatalf("cross-site GET health status = %d, want 200", rec.Code)
	}
}

// An origin on the admin CORS allow-list (system_settings.
// cors_allowed_origins) may call the API cross-origin with a bearer token
// and must pass the origin check too.
func TestOriginCheckAllowsConfiguredCORSOrigin(t *testing.T) {
	e := newTestEnv(t)
	e.server.SetCORSOrigins([]string{"https://dashboard.example.org"})

	if rec := e.doWithHeaders(t, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "wrong"}, nil,
		map[string]string{"Origin": "https://dashboard.example.org"}); rec.Code == http.StatusForbidden {
		t.Fatalf("allow-listed CORS origin status = %d, want anything but 403", rec.Code)
	}
	// An unlisted origin is still blocked.
	if rec := e.doWithHeaders(t, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "wrong"}, nil,
		map[string]string{"Origin": "https://unlisted.example"}); rec.Code != http.StatusForbidden {
		t.Fatalf("unlisted origin status = %d, want 403", rec.Code)
	}
}

// Behind a proxy, Host may be rewritten; the Origin must be compared against
// X-Forwarded-Host instead.
func TestOriginCheckBehindProxyPrefersForwardedHost(t *testing.T) {
	e := newTestEnvBehindProxy(t)

	forwarded := map[string]map[string]string{
		"origin matching forwarded host":  {"Origin": "https://ferrum.example.com", "X-Forwarded-Host": "ferrum.example.com"},
		"fallback to Host without header": {"Origin": "http://example.com"},
	}
	for name, headers := range forwarded {
		if rec := e.doWithHeaders(t, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"username": "admin", "password": "wrong"}, nil, headers); rec.Code == http.StatusForbidden {
			t.Fatalf("%s: status = %d, want anything but 403", name, rec.Code)
		}
	}
	// A foreign origin is blocked even with the forwarding headers present.
	if rec := e.doWithHeaders(t, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "wrong"}, nil,
		map[string]string{"Origin": "https://evil.example", "X-Forwarded-Host": "ferrum.example.com"}); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site login behind proxy status = %d, want 403", rec.Code)
	}
}

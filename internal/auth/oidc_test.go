package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// fakeOIDCProvider is a minimal in-process stand-in for a real provider
// (Keycloak, etc.) — enough of the OIDC surface (discovery, JWKS, token
// endpoint) to prove OIDCClient's discovery, JWKS-based signature
// verification, and claim checks all actually work end to end, not just
// that the code compiles.
type fakeOIDCProvider struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string
	nextID string // the id_token the token endpoint will hand back
	// endSession, when true, advertises an end_session_endpoint in
	// discovery — real providers make this optional, so tests exercise both.
	endSession bool
}

func newFakeOIDCProvider(t *testing.T) *fakeOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test key: %v", err)
	}
	p := &fakeOIDCProvider{key: key, kid: "test-key-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		doc := map[string]string{
			"issuer":                 base,
			"authorization_endpoint": base + "/auth",
			"token_endpoint":         base + "/token",
			"jwks_uri":               base + "/jwks",
		}
		if p.endSession {
			doc["end_session_endpoint"] = base + "/logout"
		}
		_ = json.NewEncoder(w).Encode(doc)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": p.kid,
				"n":   base64.RawURLEncoding.EncodeToString(p.key.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big64(p.key.PublicKey.E)),
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": p.nextID, "access_token": "unused"})
	})
	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

func big64(i int) []byte {
	b := []byte{byte(i >> 16), byte(i >> 8), byte(i)}
	// Trim a leading zero byte the way real JWKS "e" values do (e=65537 -> 3 bytes, no leading zero).
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

// mint builds and signs a real RS256 ID token for this provider.
func (p *fakeOIDCProvider) mint(claims map[string]any) string {
	header := map[string]string{"alg": "RS256", "kid": p.kid}
	headerB, _ := json.Marshal(header)
	payloadB, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(headerB) + "." + base64.RawURLEncoding.EncodeToString(payloadB)
	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, hashed[:])
	if err != nil {
		panic(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (p *fakeOIDCProvider) issuer() string { return p.server.URL }

func TestOIDCExchangeVerifiesRealSignedToken(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	client := NewOIDCClient(OIDCConfig{
		IssuerURL:   provider.issuer(),
		ClientID:    "ferrum",
		RedirectURL: "http://localhost/api/v1/auth/oidc/callback",
	})

	provider.nextID = provider.mint(map[string]any{
		"iss":                provider.issuer(),
		"aud":                "ferrum",
		"sub":                "user-123",
		"email":              "alice@example.com",
		"preferred_username": "alice",
		"nonce":              "expected-nonce",
		"exp":                time.Now().Add(time.Hour).Unix(),
	})

	claims, err := client.Exchange("some-code", "expected-nonce")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if claims.Subject != "user-123" || claims.Email != "alice@example.com" || claims.PreferredUsername != "alice" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestOIDCExchangeRejectsTamperedSignature(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	client := NewOIDCClient(OIDCConfig{IssuerURL: provider.issuer(), ClientID: "ferrum", RedirectURL: "http://x/callback"})

	real := provider.mint(map[string]any{
		"iss": provider.issuer(), "aud": "ferrum", "sub": "user-123", "nonce": "n", "exp": time.Now().Add(time.Hour).Unix(),
	})
	// Flip a character in the payload segment without re-signing — simulates
	// a forged/altered token an attacker controls but didn't get the IdP to sign.
	tampered := real[:len(real)-40] + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	provider.nextID = tampered

	if _, err := client.Exchange("code", "n"); err == nil {
		t.Fatal("expected signature verification to reject a tampered token")
	}
}

func TestOIDCExchangeRejectsWrongAudience(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	client := NewOIDCClient(OIDCConfig{IssuerURL: provider.issuer(), ClientID: "ferrum", RedirectURL: "http://x/callback"})

	provider.nextID = provider.mint(map[string]any{
		"iss": provider.issuer(), "aud": "some-other-app", "sub": "user-123", "nonce": "n", "exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := client.Exchange("code", "n"); err == nil {
		t.Fatal("expected a token minted for a different client to be rejected")
	}
}

func TestOIDCExchangeRejectsNonceMismatch(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	client := NewOIDCClient(OIDCConfig{IssuerURL: provider.issuer(), ClientID: "ferrum", RedirectURL: "http://x/callback"})

	provider.nextID = provider.mint(map[string]any{
		"iss": provider.issuer(), "aud": "ferrum", "sub": "user-123", "nonce": "actual-nonce", "exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := client.Exchange("code", "different-nonce"); err == nil {
		t.Fatal("expected nonce mismatch (possible replay) to be rejected")
	}
}

func TestOIDCExchangeRejectsExpiredToken(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	client := NewOIDCClient(OIDCConfig{IssuerURL: provider.issuer(), ClientID: "ferrum", RedirectURL: "http://x/callback"})

	provider.nextID = provider.mint(map[string]any{
		"iss": provider.issuer(), "aud": "ferrum", "sub": "user-123", "nonce": "n", "exp": time.Now().Add(-time.Hour).Unix(),
	})

	if _, err := client.Exchange("code", "n"); err == nil {
		t.Fatal("expected an expired token to be rejected")
	}
}

func TestFindOrCreateOIDCUserLinksExistingEmailThenReusesSubject(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Pre-existing local (password) account with the same email.
	local, err := svc.Bootstrap(ctx, "alice", "alice@example.com", "supersecret1")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	linked, err := svc.FindOrCreateOIDCUser(ctx, "sub-1", "alice@example.com", "alice", true, true)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser (link): %v", err)
	}
	if linked.ID != local.ID {
		t.Fatalf("expected SSO login to link to the existing local account, got a different user")
	}

	again, err := svc.FindOrCreateOIDCUser(ctx, "sub-1", "alice@example.com", "alice", true, true)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser (reuse): %v", err)
	}
	if again.ID != local.ID {
		t.Fatalf("expected repeat SSO login to resolve to the same linked account")
	}

	// A brand-new subject/email should provision a fresh, non-admin account.
	fresh, err := svc.FindOrCreateOIDCUser(ctx, "sub-2", "bob@example.com", "bob", true, true)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser (new): %v", err)
	}
	if fresh.ID == local.ID || fresh.IsAdmin {
		t.Fatalf("expected a distinct, non-admin account for a new SSO identity, got %+v", fresh)
	}
}

func TestFindOrCreateOIDCUserNeverLinksWithUnverifiedEmail(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// A local account already owns the email the SSO identity claims.
	if _, err := svc.Bootstrap(ctx, "alice", "alice@example.com", "supersecret1"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Unverified claim on a colliding email must NOT link into alice's
	// account (account takeover via IdP email spoofing) and must NOT create
	// a second user holding the same address — it refuses instead.
	if _, err := svc.FindOrCreateOIDCUser(ctx, "sub-evil", "alice@example.com", "alice", false, true); err == nil {
		t.Fatal("unverified email colliding with a local account should be refused")
	}

	var oidcSubject string
	err := svc.db.QueryRowContext(ctx, `SELECT oidc_subject FROM users WHERE username = 'alice'`).Scan(&oidcSubject)
	if err == nil && oidcSubject == "sub-evil" {
		t.Fatal("local account was linked from an unverified SSO identity")
	}

	// A fresh unverified identity still gets an account, but with a
	// placeholder email that can't collide with anyone's real address.
	created, err := svc.FindOrCreateOIDCUser(ctx, "sub-new", "mallory@example.com", "mallory", false, true)
	if err != nil {
		t.Fatalf("unverified fresh identity should be provisioned: %v", err)
	}
	if created.IsAdmin {
		t.Fatal("provisioned SSO users must never be admins")
	}
	var email string
	if err := svc.db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = ?`, created.ID).Scan(&email); err != nil {
		t.Fatalf("loading created user: %v", err)
	}
	if email == "mallory@example.com" {
		t.Fatal("unverified email was persisted verbatim — it must use the placeholder")
	}

	// The same identity logging in again resolves to the same account.
	again, err := svc.FindOrCreateOIDCUser(ctx, "sub-new", "mallory@example.com", "mallory", false, true)
	if err != nil || again.ID != created.ID {
		t.Fatalf("second login should reuse the subject's account: %v %+v", err, again)
	}
}

func TestFindOrCreateOIDCUserLinksVerifiedEmail(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Bootstrap(ctx, "alice", "alice@example.com", "supersecret1"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// A provider-verified email authorizes the account link.
	linked, err := svc.FindOrCreateOIDCUser(ctx, "sub-1", "alice@example.com", "alice", true, true)
	if err != nil {
		t.Fatalf("link with verified email: %v", err)
	}
	var username string
	if err := svc.db.QueryRowContext(ctx, `SELECT username FROM users WHERE oidc_subject = ?`, "sub-1").Scan(&username); err != nil {
		t.Fatalf("linked account not persisted: %v", err)
	}
	if username != "alice" || linked.Username != "alice" {
		t.Fatalf("linked to %q, want alice", linked.Username)
	}
}

func TestFindOrCreateOIDCUserRefusesAutoProvisionWhenDisabled(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// No local account exists for this identity, and auto-provisioning is
	// off: the login must be refused, not silently create an account.
	_, err := svc.FindOrCreateOIDCUser(ctx, "sub-new", "carol@example.com", "carol", true, false)
	if !errors.Is(err, ErrOIDCUserNotProvisioned) {
		t.Fatalf("expected ErrOIDCUserNotProvisioned, got %v", err)
	}
	var count int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = ?`, "carol@example.com").Scan(&count); err != nil {
		t.Fatalf("counting users: %v", err)
	}
	if count != 0 {
		t.Fatal("no account should have been created")
	}

	// An existing account (linked by subject) still resolves normally even
	// with auto-provisioning off — the gate only blocks *new* accounts.
	local, err := svc.Bootstrap(ctx, "dave", "dave@example.com", "supersecret1")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE users SET oidc_subject = ? WHERE id = ?`, "sub-dave", local.ID); err != nil {
		t.Fatalf("linking subject: %v", err)
	}
	found, err := svc.FindOrCreateOIDCUser(ctx, "sub-dave", "dave@example.com", "dave", true, false)
	if err != nil {
		t.Fatalf("existing account should still resolve: %v", err)
	}
	if found.ID != local.ID {
		t.Fatalf("expected to resolve to dave's existing account, got %+v", found)
	}
}

func TestEndSessionURLNotAdvertised(t *testing.T) {
	provider := newFakeOIDCProvider(t) // endSession left false
	client := NewOIDCClient(OIDCConfig{IssuerURL: provider.issuer(), ClientID: "ferrum", RedirectURL: "http://localhost/api/v1/auth/oidc/callback"})

	if _, ok := client.EndSessionURL("some-id-token"); ok {
		t.Fatal("expected ok=false when the provider doesn't advertise end_session_endpoint")
	}
}

func TestEndSessionURLBuildsRPInitiatedLogout(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	provider.endSession = true
	client := NewOIDCClient(OIDCConfig{IssuerURL: provider.issuer(), ClientID: "ferrum", RedirectURL: "http://localhost/api/v1/auth/oidc/callback"})

	got, ok := client.EndSessionURL("the-id-token")
	if !ok {
		t.Fatal("expected ok=true when the provider advertises end_session_endpoint")
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("EndSessionURL returned an unparseable URL: %v", err)
	}
	if u.Path != "/logout" {
		t.Fatalf("path = %q, want /logout", u.Path)
	}
	q := u.Query()
	if q.Get("client_id") != "ferrum" {
		t.Errorf("client_id = %q, want ferrum", q.Get("client_id"))
	}
	if q.Get("id_token_hint") != "the-id-token" {
		t.Errorf("id_token_hint = %q, want the-id-token", q.Get("id_token_hint"))
	}
	// Derived from RedirectURL's origin, not something an attacker-controlled
	// request could influence.
	if want := "http://localhost/login"; q.Get("post_logout_redirect_uri") != want {
		t.Errorf("post_logout_redirect_uri = %q, want %q", q.Get("post_logout_redirect_uri"), want)
	}
}

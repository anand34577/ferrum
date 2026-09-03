package auth

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OIDCConfig is the subset of internal/config.OIDCConfig the client needs —
// kept local to auth so this package doesn't import internal/config.
type OIDCConfig struct {
	DisplayName  string
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// AllowAutoProvision controls whether a first-time SSO login creates a
	// new local account (the historical default) or is refused — see
	// Service.FindOrCreateOIDCUser and ErrOIDCUserNotProvisioned. An admin
	// who wants SSO restricted to pre-existing accounts turns this off.
	AllowAutoProvision bool
}

// OIDCClient implements the OpenID Connect authorization-code flow against
// any standard-compliant provider (Keycloak, Authentik, Entra ID, Okta, ...)
// using only the standard library. It supports RS256-signed ID tokens,
// which covers every mainstream provider's default signing algorithm; a
// provider configured for a different alg (ES256, HS256, ...) will fail
// verification with a clear error rather than silently accepting a token
// it can't actually check.
type OIDCClient struct {
	cfg        OIDCConfig
	httpClient *http.Client

	mu           sync.Mutex
	discovery    *oidcDiscovery
	discoveredAt time.Time
	jwks         map[string]*rsa.PublicKey
	jwksAt       time.Time
}

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	// EndSessionEndpoint (RP-Initiated Logout 1.0) is optional per spec —
	// not every provider advertises one (see EndSessionURL).
	EndSessionEndpoint string `json:"end_session_endpoint"`
}

func NewOIDCClient(cfg OIDCConfig) *OIDCClient {
	return &OIDCClient{cfg: cfg, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (c *OIDCClient) DisplayName() string {
	if c.cfg.DisplayName != "" {
		return c.cfg.DisplayName
	}
	return "SSO"
}

// AllowAutoProvision reports whether a first-time SSO login may create a new
// local account (see OIDCConfig.AllowAutoProvision).
func (c *OIDCClient) AllowAutoProvision() bool {
	return c.cfg.AllowAutoProvision
}

// discoveryTTL bounds how long a cached discovery document is trusted, so a
// provider rotating its endpoints is picked up instead of pinned forever.
const discoveryTTL = 10 * time.Minute

func (c *OIDCClient) discover() (*oidcDiscovery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.discovery != nil && time.Since(c.discoveredAt) < discoveryTTL {
		return c.discovery, nil
	}
	resp, err := c.httpClient.Get(strings.TrimRight(c.cfg.IssuerURL, "/") + "/.well-known/openid-configuration")
	if err != nil {
		return nil, fmt.Errorf("fetching OIDC discovery document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery returned %d", resp.StatusCode)
	}
	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("parsing OIDC discovery document: %w", err)
	}
	if d.Issuer != c.cfg.IssuerURL && d.Issuer != strings.TrimRight(c.cfg.IssuerURL, "/") {
		return nil, fmt.Errorf("OIDC discovery issuer %q does not match configured issuer %q", d.Issuer, c.cfg.IssuerURL)
	}
	c.discovery = &d
	c.discoveredAt = time.Now()
	return &d, nil
}

// AuthURL builds the redirect URL that sends the browser to the provider's
// login page, embedding the CSRF state and the replay-protection nonce.
func (c *OIDCClient) AuthURL(state, nonce string) (string, error) {
	d, err := c.discover()
	if err != nil {
		return "", err
	}
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {c.cfg.ClientID},
		"redirect_uri":  {c.cfg.RedirectURL},
		"scope":         {"openid profile email"},
		"state":         {state},
		"nonce":         {nonce},
	}
	return d.AuthorizationEndpoint + "?" + q.Encode(), nil
}

// Claims is the subset of ID token claims Ferrum acts on.
type Claims struct {
	Subject           string
	Email             string
	EmailVerified     bool
	PreferredUsername string
	// RawIDToken is the verified ID token JWT itself, kept only so it can be
	// handed back to the provider as id_token_hint on RP-Initiated Logout —
	// see EndSessionURL. Ferrum's own session is a random bearer token; this
	// is never used to re-authenticate a request.
	RawIDToken string
}

// Exchange trades an authorization code for an ID token at the provider's
// token endpoint, then verifies it, returning the caller's identity claims.
func (c *OIDCClient) Exchange(code, nonce string) (*Claims, error) {
	d, err := c.discover()
	if err != nil {
		return nil, err
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {c.cfg.RedirectURL},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
	}
	req, err := http.NewRequest(http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling OIDC token endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing OIDC token response: %w", err)
	}
	if tokenResp.IDToken == "" {
		return nil, errors.New("OIDC provider did not return an id_token")
	}

	claims, err := c.verifyIDToken(tokenResp.IDToken, d.Issuer, nonce)
	if err != nil {
		return nil, err
	}
	claims.RawIDToken = tokenResp.IDToken
	return claims, nil
}

// EndSessionURL builds the RP-Initiated Logout redirect (OpenID Connect
// RP-Initiated Logout 1.0): the browser is sent here to end the session at
// the provider too, not just at Ferrum, then bounced back to Ferrum's login
// page (same origin as the configured callback URL — never taken from the
// request, which is attacker-controlled). ok is false when the provider
// doesn't advertise an end_session_endpoint (optional per spec — plenty of
// providers omit it), in which case there's nothing to redirect to and
// Ferrum's own logout (already done by the caller) is all that happens.
func (c *OIDCClient) EndSessionURL(idTokenHint string) (endSessionURL string, ok bool) {
	d, err := c.discover()
	if err != nil || d.EndSessionEndpoint == "" {
		return "", false
	}
	q := url.Values{"client_id": {c.cfg.ClientID}}
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	if redirect := c.postLogoutRedirectURI(); redirect != "" {
		q.Set("post_logout_redirect_uri", redirect)
	}
	return d.EndSessionEndpoint + "?" + q.Encode(), true
}

// postLogoutRedirectURI derives Ferrum's login page from the configured
// callback URL's origin (…/api/v1/auth/oidc/callback → …/login), so it's
// never a second thing the admin has to configure separately.
func (c *OIDCClient) postLogoutRedirectURI() string {
	u, err := url.Parse(c.cfg.RedirectURL)
	if err != nil {
		return ""
	}
	u.Path = "/login"
	u.RawQuery = ""
	return u.String()
}

func (c *OIDCClient) verifyIDToken(idToken, expectIssuer, expectNonce string) (*Claims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed ID token")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decoding ID token header: %w", err)
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, err
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported ID token signing algorithm %q (only RS256 is supported)", header.Alg)
	}

	sig, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decoding ID token signature: %w", err)
	}
	key, err := c.publicKey(header.Kid)
	if err != nil {
		return nil, err
	}
	hashed := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hashed[:], sig); err != nil {
		return nil, fmt.Errorf("ID token signature verification failed: %w", err)
	}

	payloadJSON, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding ID token payload: %w", err)
	}
	var claims struct {
		Iss               string `json:"iss"`
		Aud               any    `json:"aud"` // string or []string per the OIDC spec
		Exp               int64  `json:"exp"`
		Nonce             string `json:"nonce"`
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, err
	}

	if claims.Iss != expectIssuer {
		return nil, fmt.Errorf("ID token issuer %q does not match expected issuer %q", claims.Iss, expectIssuer)
	}
	if !audienceContains(claims.Aud, c.cfg.ClientID) {
		return nil, errors.New("ID token audience does not include this client")
	}
	if time.Now().After(time.Unix(claims.Exp, 0)) {
		return nil, errors.New("ID token has expired")
	}
	if claims.Nonce != expectNonce {
		return nil, errors.New("ID token nonce does not match — possible replay")
	}
	if claims.Sub == "" {
		return nil, errors.New("ID token has no subject claim")
	}

	return &Claims{Subject: claims.Sub, Email: claims.Email, EmailVerified: claims.EmailVerified, PreferredUsername: claims.PreferredUsername}, nil
}

func audienceContains(aud any, clientID string) bool {
	switch v := aud.(type) {
	case string:
		return v == clientID
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == clientID {
				return true
			}
		}
	}
	return false
}

// publicKey resolves a JWKS key id to an *rsa.PublicKey, fetching (and
// caching for 10 minutes) the provider's JWKS document as needed.
func (c *OIDCClient) publicKey(kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	fresh := c.jwks != nil && time.Since(c.jwksAt) < 10*time.Minute
	c.mu.Unlock()

	if !fresh {
		if err := c.refreshJWKS(); err != nil {
			return nil, err
		}
	}

	c.mu.Lock()
	key, ok := c.jwks[kid]
	c.mu.Unlock()
	if !ok {
		// The key may have rotated since our last fetch — try once more before failing.
		if err := c.refreshJWKS(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		key, ok = c.jwks[kid]
		c.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("no JWKS key found for kid %q", kid)
		}
	}
	return key, nil
}

func (c *OIDCClient) refreshJWKS() error {
	d, err := c.discover()
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Get(d.JWKSURI)
	if err != nil {
		return fmt.Errorf("fetching JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}

	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("parsing JWKS: %w", err)
	}

	keys := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nBytes, err := base64URLDecode(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64URLDecode(k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(new(big.Int).SetBytes(eBytes).Int64()),
		}
	}

	c.mu.Lock()
	c.jwks = keys
	c.jwksAt = time.Now()
	c.mu.Unlock()
	return nil
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

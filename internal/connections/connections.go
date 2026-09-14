// Package connections resolves stored connection rows into authenticated
// Proxmox clients. It exists as its own package (rather than living in
// internal/api) so background jobs like the alert poller can build clients
// the same way HTTP handlers do, without an import cycle.
package connections

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"ferrum/internal/pbs"
	"ferrum/internal/pve"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

// loginTimeout bounds a shared login attempt (see Resolver.logins below) —
// deliberately generous since it only ever runs against a real network
// round-trip to Proxmox, not a per-request budget.
const loginTimeout = 30 * time.Second

type Info struct {
	ID   string
	Name string
	Type string // "pve" | "pbs" — which API surface this connection speaks
}

// ticketTTL bounds how long a cached password-auth client is reused before
// re-login. PVE tickets are valid for 2h; refreshing well before expiry
// keeps long-lived consoles and poller ticks working while cutting the
// per-request login cost to zero for the common case.
const ticketTTL = 60 * time.Minute

type Resolver struct {
	db      *store.DB
	secrets *secrets.Box

	mu        sync.Mutex
	cached    map[string]*cachedClient
	cachedPBS map[string]*cachedPBSClient
	// generations counts invalidations per connection id (bumped by
	// invalidate) so a login that read its credentials before an edit/delete
	// can't store a client built from the OLD credentials over the
	// invalidation — see login.
	generations map[string]uint64

	// logins coalesces concurrent cache-miss callers for the same connection
	// id into one login attempt — see ClientFor.
	logins singleflight.Group
	// pbsLogins does the same for PBSClientFor, kept separate so a PVE and a
	// PBS login for the same connection id (which can't actually happen,
	// since a row is one type or the other, but keeps the two code paths
	// independent) never collide in one singleflight.Group key space.
	pbsLogins singleflight.Group
}

// cachedClient pairs an authenticated client with its login time so
// password-auth connections don't re-authenticate on every request.
// Token-auth entries carry a zero loggedIn and never expire — there is no
// ticket to refresh, only a header to replay.
type cachedClient struct {
	client  *pve.Client
	expires time.Time // zero == never (token auth)
}

// live reports whether this cached client can still be handed out.
func (e *cachedClient) live() bool { return e.expires.IsZero() || time.Now().Before(e.expires) }

// cachedPBSClient mirrors cachedClient for PBS connections.
type cachedPBSClient struct {
	client  *pbs.Client
	expires time.Time
}

func (e *cachedPBSClient) live() bool { return e.expires.IsZero() || time.Now().Before(e.expires) }

func New(db *store.DB, secretBox *secrets.Box) *Resolver {
	return &Resolver{
		db: db, secrets: secretBox,
		cached:      map[string]*cachedClient{},
		cachedPBS:   map[string]*cachedPBSClient{},
		generations: map[string]uint64{},
	}
}

func (r *Resolver) List(ctx context.Context) ([]Info, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, type FROM connections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Info
	for rows.Next() {
		var c Info
		if err := rows.Scan(&c.ID, &c.Name, &c.Type); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type connectionCreds struct {
	host           string
	port           int
	connType       string // "pve" | "pbs" — a row is routed to exactly one client type
	authType       string
	tokenID        string
	tokenSecretEnc string
	username       string
	passwordEnc    string
	verifyTLS      bool
	tlsFingerprint string // "" = unset (no pinning)
}

func (r *Resolver) credsFor(ctx context.Context, id string) (*connectionCreds, error) {
	var (
		c           connectionCreds
		verify      int
		tokenID     string
		tokenEnc    string
		username    string
		passwordEn  string
		fingerprint string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT host, port, type, auth_type, COALESCE(token_id,''), COALESCE(token_secret_enc,''), COALESCE(username,''), COALESCE(password_enc,''), verify_tls, COALESCE(tls_fingerprint,'')
		FROM connections WHERE id = ?`, id,
	).Scan(&c.host, &c.port, &c.connType, &c.authType, &tokenID, &tokenEnc, &username, &passwordEn, &verify, &fingerprint)
	if err != nil {
		return nil, err
	}
	c.tokenID, c.tokenSecretEnc, c.username, c.passwordEnc = tokenID, tokenEnc, username, passwordEn
	c.verifyTLS = verify == 1
	c.tlsFingerprint = fingerprint
	return &c, nil
}

// ClientFor returns an authenticated pve.Client for a stored connection.
// Password-auth logins are cached per connection and reused until the ticket
// TTL elapses or the upstream rejects the ticket with 401 (then re-login
// happens once, on demand).
func (r *Resolver) ClientFor(ctx context.Context, id string) (*pve.Client, error) {
	// Cache lookup first, for both auth types. Token auth used to skip the
	// cache entirely and build a fresh Client per request, which meant a DB
	// read, a secret decrypt, and (before the shared transport landed in
	// pve.New) a cold TLS pool on every single upstream call.
	r.mu.Lock()
	entry, ok := r.cached[id]
	r.mu.Unlock()
	if ok && entry.live() {
		return entry.client, nil
	}

	// Cold start (or a just-expired ticket) commonly has a dozen dashboard
	// widgets all resolving the same connection within milliseconds of each
	// other — without this, every one of them raced its own login POST to
	// Proxmox, and whichever caller's own request got cancelled first (a
	// React Query dedupe abort, a navigated-away tab) took its login down
	// with it and surfaced as "context canceled" even though nothing was
	// actually wrong. singleflight collapses them into one login, run on
	// its own bounded timeout instead of any single caller's ctx, so no
	// caller's cancellation can take the others' result with it.
	v, err, _ := r.logins.Do(id, func() (any, error) {
		loginCtx, cancel := context.WithTimeout(context.Background(), loginTimeout)
		defer cancel()
		return r.login(loginCtx, id)
	})
	if err != nil {
		return nil, err
	}
	return v.(*pve.Client), nil
}

// generation returns the connection's current invalidation generation.
func (r *Resolver) generation(id string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generations[id]
}

// login does the actual credential lookup + authentication for id. Callers
// go through ClientFor's singleflight, not this directly.
func (r *Resolver) login(ctx context.Context, id string) (*pve.Client, error) {
	// Re-check the cache: a sequential (not concurrent) call can arrive
	// after singleflight's previous generation already populated it.
	r.mu.Lock()
	entry, ok := r.cached[id]
	r.mu.Unlock()
	if ok && entry.live() {
		return entry.client, nil
	}

	creds, err := r.credsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	// Enforce the row's type so a pbs-typed connection id can't be driven
	// through the PVE client path (the PBS mirror of this check is in
	// pbsLogin) — the API layer guards its routes, but the poller, digest,
	// and health scorers call the resolver directly with whatever is stored.
	if creds.connType != "pve" {
		return nil, fmt.Errorf("connection %s is not a PVE connection", id)
	}
	// Captured after the credential read: any invalidation from here until
	// the store below means the credentials may already be stale, and the
	// freshly-built client must not be cached over that invalidation.
	gen := r.generation(id)
	client := pve.New(creds.host, creds.port,
		pve.WithInsecureSkipVerify(!creds.verifyTLS),
		// A configured pin replaces CA validation entirely (see
		// pve.applyTransport); empty is a no-op.
		pve.WithFingerprint(creds.tlsFingerprint),
	)

	var expires time.Time
	switch creds.authType {
	case "token":
		secret, err := r.secrets.Decrypt(creds.tokenSecretEnc)
		if err != nil {
			return nil, err
		}
		client.WithAPIToken(creds.tokenID, secret)
		// expires stays zero: a token never goes stale on our side. Editing
		// or deleting the connection calls Invalidate, which evicts it.
	case "password":
		password, err := r.secrets.Decrypt(creds.passwordEnc)
		if err != nil {
			return nil, err
		}
		if err := client.Login(ctx, creds.username, password); err != nil {
			// A stale cached ticket surfacing as 401 shouldn't poison the
			// cache entry forever; drop it so the next caller starts fresh.
			r.invalidate(id)
			return nil, err
		}
		expires = time.Now().Add(ticketTTL)
	default:
		return nil, &unknownAuthTypeError{authType: creds.authType}
	}

	r.mu.Lock()
	if r.generations[id] == gen { // skip storing if invalidated mid-login
		r.cached[id] = &cachedClient{client: client, expires: expires}
	}
	r.mu.Unlock()
	return client, nil
}

// PBSClientFor returns an authenticated pbs.Client for a stored PBS
// connection, with the same caching behavior as ClientFor.
func (r *Resolver) PBSClientFor(ctx context.Context, id string) (*pbs.Client, error) {
	r.mu.Lock()
	entry, ok := r.cachedPBS[id]
	r.mu.Unlock()
	if ok && entry.live() {
		return entry.client, nil
	}

	v, err, _ := r.pbsLogins.Do(id, func() (any, error) {
		loginCtx, cancel := context.WithTimeout(context.Background(), loginTimeout)
		defer cancel()
		return r.pbsLogin(loginCtx, id)
	})
	if err != nil {
		return nil, err
	}
	return v.(*pbs.Client), nil
}

func (r *Resolver) pbsLogin(ctx context.Context, id string) (*pbs.Client, error) {
	r.mu.Lock()
	entry, ok := r.cachedPBS[id]
	r.mu.Unlock()
	if ok && entry.live() {
		return entry.client, nil
	}

	creds, err := r.credsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	// Mirror of login's type check: only pbs-typed rows may build a PBS
	// client, so a pve-typed id can't be pointed at a PBS endpoint.
	if creds.connType != "pbs" {
		return nil, fmt.Errorf("connection %s is not a PBS connection", id)
	}
	gen := r.generation(id)
	// Both paths pin identically: an empty fingerprint is a no-op, a set one
	// replaces skip-verify with a SHA-256 pin of the server certificate.
	client := pbs.New(creds.host, creds.port, pbs.WithInsecureSkipVerify(!creds.verifyTLS), pbs.WithFingerprint(creds.tlsFingerprint))

	var expires time.Time
	switch creds.authType {
	case "token":
		secret, err := r.secrets.Decrypt(creds.tokenSecretEnc)
		if err != nil {
			return nil, err
		}
		client.WithAPIToken(creds.tokenID, secret)
	case "password":
		password, err := r.secrets.Decrypt(creds.passwordEnc)
		if err != nil {
			return nil, err
		}
		if err := client.Login(ctx, creds.username, password); err != nil {
			r.invalidate(id)
			return nil, err
		}
		expires = time.Now().Add(ticketTTL)
	default:
		return nil, &unknownAuthTypeError{authType: creds.authType}
	}

	r.mu.Lock()
	if r.generations[id] == gen { // skip storing if invalidated mid-login
		r.cachedPBS[id] = &cachedPBSClient{client: client, expires: expires}
	}
	r.mu.Unlock()
	return client, nil
}

// TargetCredentials returns the decrypted host/port/token needed to build a
// PVE remote-migrate "target-endpoint" string for a stored connection. It is
// only ever consumed server-side (internal/api builds the endpoint string
// and hands it straight to the source PVE cluster) — the decrypted secret
// must never be sent back to the frontend. Only token auth is supported: PVE
// remote-migrate authenticates against the target with an API token, not a
// ticket.
func (r *Resolver) TargetCredentials(ctx context.Context, id string) (host string, port int, tokenID, tokenSecret string, err error) {
	creds, err := r.credsFor(ctx, id)
	if err != nil {
		return "", 0, "", "", err
	}
	if creds.authType != "token" {
		return "", 0, "", "", fmt.Errorf("target connection must use API token auth for remote migration")
	}
	secret, err := r.secrets.Decrypt(creds.tokenSecretEnc)
	if err != nil {
		return "", 0, "", "", err
	}
	return creds.host, creds.port, creds.tokenID, secret, nil
}

// OnUpstreamUnauthorized drops the cached ticket for a connection after the
// upstream rejected it with 401, so the next request re-authenticates.
func (r *Resolver) OnUpstreamUnauthorized(id string) {
	r.invalidate(id)
}

// RetryOnUnauthorized runs op, and when it fails because the upstream
// rejected the cached session (pve.ErrUnauthorized / pbs.ErrUnauthorized —
// the 401 case OnUpstreamUnauthorized exists for), drops the cached client
// and runs op exactly once more so it re-resolves a freshly logged-in one.
// Any other error is returned as-is with no retry: only an auth rejection
// is ever worth a second attempt, and op must re-resolve its client
// through the resolver so the retry actually picks up the fresh login.
func (r *Resolver) RetryOnUnauthorized(connID string, op func() error) error {
	err := op()
	if !errors.Is(err, pve.ErrUnauthorized) && !errors.Is(err, pbs.ErrUnauthorized) {
		return err
	}
	r.OnUpstreamUnauthorized(connID)
	return op()
}

// Invalidate drops the cached ticket for one connection — used when its
// stored credentials are edited or the connection is deleted, so a
// previously-authenticated client is never reused after that point.
func (r *Resolver) Invalidate(id string) {
	r.invalidate(id)
}

func (r *Resolver) invalidate(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cached, id)
	delete(r.cachedPBS, id)
	r.generations[id]++
}

type unknownAuthTypeError struct{ authType string }

func (e *unknownAuthTypeError) Error() string {
	return "connection has unknown auth type " + e.authType
}

// Host returns the stored host/port/verify-TLS for a connection, used by the
// console WebSocket proxy which dials Proxmox directly rather than through
// the REST client.
func (r *Resolver) Host(ctx context.Context, id string) (host string, port int, verifyTLS bool, err error) {
	var verify int
	err = r.db.QueryRowContext(ctx, `SELECT host, port, verify_tls FROM connections WHERE id = ?`, id).Scan(&host, &port, &verify)
	verifyTLS = verify == 1
	return
}

// SSHCredentials returns a connection's stored SSH host/port/username and
// decrypted secret (a password or a private key PEM, per authType), for
// minting an SSH session against this connection's own host straight from
// Inventory without retyping credentials each time. ok is false when the
// connection has no SSH credentials configured (ssh_auth_type is empty) —
// not an error, since most connections simply don't have any.
func (r *Resolver) SSHCredentials(ctx context.Context, id string) (host string, port int, username, authType, secret string, ok bool, err error) {
	var (
		sshPort     int
		sshUsername string
		sshAuthType string
		sshSecret   string
	)
	err = r.db.QueryRowContext(ctx, `
		SELECT host, COALESCE(ssh_port, 22), COALESCE(ssh_username,''), COALESCE(ssh_auth_type,''), COALESCE(ssh_secret_enc,'')
		FROM connections WHERE id = ?`, id,
	).Scan(&host, &sshPort, &sshUsername, &sshAuthType, &sshSecret)
	if err != nil {
		return "", 0, "", "", "", false, err
	}
	if sshAuthType == "" {
		return "", 0, "", "", "", false, nil
	}
	decrypted, err := r.secrets.Decrypt(sshSecret)
	if err != nil {
		return "", 0, "", "", "", false, err
	}
	return host, sshPort, sshUsername, sshAuthType, decrypted, true, nil
}

// InvalidateAll drops every cached PVE ticket and PBS client in one go.
func (r *Resolver) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = map[string]*cachedClient{}
	r.cachedPBS = map[string]*cachedPBSClient{}
}

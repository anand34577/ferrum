// Package connections resolves stored connection rows into authenticated
// Proxmox clients. It exists as its own package (rather than living in
// internal/api) so background jobs like the alert poller can build clients
// the same way HTTP handlers do, without an import cycle.
package connections

import (
	"context"
	"sync"
	"time"

	"ferrum/internal/pve"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

type Info struct {
	ID   string
	Name string
}

// ticketTTL bounds how long a cached password-auth client is reused before
// re-login. PVE tickets are valid for 2h; refreshing well before expiry
// keeps long-lived consoles and poller ticks working while cutting the
// per-request login cost to zero for the common case.
const ticketTTL = 60 * time.Minute

type Resolver struct {
	db      *store.DB
	secrets *secrets.Box

	mu     sync.Mutex
	cached map[string]*cachedClient
}

// cachedClient pairs an authenticated client with its login time so
// password-auth connections don't re-authenticate on every request.
type cachedClient struct {
	client    *pve.Client
	loggedIn  time.Time
	isToken   bool // token-auth clients never expire on our side
	validated bool // has served at least one successful request since login
}

func New(db *store.DB, secretBox *secrets.Box) *Resolver {
	return &Resolver{db: db, secrets: secretBox, cached: map[string]*cachedClient{}}
}

func (r *Resolver) List(ctx context.Context) ([]Info, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name FROM connections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Info
	for rows.Next() {
		var c Info
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
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
	authType       string
	tokenID        string
	tokenSecretEnc string
	username       string
	passwordEnc    string
	verifyTLS      bool
}

func (r *Resolver) credsFor(ctx context.Context, id string) (*connectionCreds, error) {
	var (
		c          connectionCreds
		verify     int
		tokenID    string
		tokenEnc   string
		username   string
		passwordEn string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT host, port, auth_type, COALESCE(token_id,''), COALESCE(token_secret_enc,''), COALESCE(username,''), COALESCE(password_enc,''), verify_tls
		FROM connections WHERE id = ?`, id,
	).Scan(&c.host, &c.port, &c.authType, &tokenID, &tokenEnc, &username, &passwordEn, &verify)
	if err != nil {
		return nil, err
	}
	c.tokenID, c.tokenSecretEnc, c.username, c.passwordEnc = tokenID, tokenEnc, username, passwordEn
	c.verifyTLS = verify == 1
	return &c, nil
}

// ClientFor returns an authenticated pve.Client for a stored connection.
// Password-auth logins are cached per connection and reused until the ticket
// TTL elapses or the upstream rejects the ticket with 401 (then re-login
// happens once, on demand).
func (r *Resolver) ClientFor(ctx context.Context, id string) (*pve.Client, error) {
	creds, err := r.credsFor(ctx, id)
	if err != nil {
		return nil, err
	}

	switch creds.authType {
	case "token":
		// Token auth is stateless — no login, no caching needed.
		client := pve.New(creds.host, creds.port, pve.WithInsecureSkipVerify(!creds.verifyTLS))
		secret, err := r.secrets.Decrypt(creds.tokenSecretEnc)
		if err != nil {
			return nil, err
		}
		client.WithAPIToken(creds.tokenID, secret)
		return client, nil
	case "password":
		// fall through to the ticket cache below
	default:
		return nil, &unknownAuthTypeError{authType: creds.authType}
	}

	r.mu.Lock()
	entry, ok := r.cached[id]
	r.mu.Unlock()
	if ok && time.Since(entry.loggedIn) < ticketTTL {
		return entry.client, nil
	}

	password, err := r.secrets.Decrypt(creds.passwordEnc)
	if err != nil {
		return nil, err
	}
	client := pve.New(creds.host, creds.port, pve.WithInsecureSkipVerify(!creds.verifyTLS))
	if err := client.Login(ctx, creds.username, password); err != nil {
		// A stale cached ticket surfacing as 401 shouldn't poison the cache
		// entry forever; drop it so the next caller starts fresh.
		r.invalidate(id)
		return nil, err
	}

	r.mu.Lock()
	r.cached[id] = &cachedClient{client: client, loggedIn: time.Now(), isToken: false}
	r.mu.Unlock()
	return client, nil
}

// OnUpstreamUnauthorized drops the cached ticket for a connection after the
// upstream rejected it with 401, so the next request re-authenticates.
func (r *Resolver) OnUpstreamUnauthorized(id string) {
	r.invalidate(id)
}

func (r *Resolver) invalidate(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cached, id)
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

// InvalidateAll drops every cached ticket — used when a connection row is
// updated or deleted so stale credentials are never reused.
func (r *Resolver) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = map[string]*cachedClient{}
}

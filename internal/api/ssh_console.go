package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// sshSession is a short-lived hand-off between the "open SSH shell" REST call
// (which takes the target host and credentials) and the WebSocket upgrade
// that actually runs the session — same pattern as consoleSession in
// console.go, but for a direct SSH connection instead of a Proxmox VNC/term
// proxy ticket. Keeping the password out of the WebSocket URL/query string is
// the whole reason for the hand-off: it travels once, in an authenticated
// POST body, and is gone from memory the moment the WS consumes it.
type sshSession struct {
	host, username string
	// authType is "password" or "key"; secret holds whichever it implies —
	// a password, or an unencrypted PEM private key.
	authType, secret string
	port             int
	cols, rows       int
	expires          time.Time
}

// authMethod builds the ssh.AuthMethod for this session's authType — the
// one place password vs. key auth branches, shared by both ways a session
// gets minted (an ad-hoc dialog, or a connection's stored credentials).
func (sess sshSession) authMethod() (ssh.AuthMethod, error) {
	if sess.authType == "key" {
		signer, err := ssh.ParsePrivateKey([]byte(sess.secret))
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return ssh.PublicKeys(signer), nil
	}
	return ssh.Password(sess.secret), nil
}

var (
	sshSessionsMu sync.Mutex
	sshSessions   = map[string]sshSession{}
)

// sshSessionTTL mirrors consoleSessionTTL — generous relative to how quickly
// a browser actually opens the follow-up WebSocket, since (unlike Proxmox's
// termproxy) there is no tight upstream timer to race here at all: a plain
// SSH server just waits for a TCP connection and an auth attempt.
const sshSessionTTL = 60 * time.Second

// verifySSHHostKey implements trust-on-first-use host-key pinning against
// the ssh_known_hosts table: the first key seen for a host:port is stored
// and accepted, and every later connection must present that exact key.
func (s *Server) verifySSHHostKey(host string, port int) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		// A background context, not the dial's request context: this pin
		// check/write is a durability concern independent of the request
		// lifecycle — a browser tab closing mid-handshake must not turn
		// into a bogus "checking pinned host key" failure via a context
		// that was canceled for an unrelated reason.
		ctx := context.Background()

		// Atomic claim-then-read: INSERT ... ON CONFLICT DO NOTHING lets at
		// most one of two concurrent first-connections to the same
		// host:port actually write the pin, and the SELECT right after
		// always reads back whichever fingerprint won — so both goroutines
		// compare against the same authoritative row instead of each one
		// silently trusting whatever key it happened to see first.
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO ssh_known_hosts (host, port, fingerprint, created_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (host, port) DO NOTHING`,
			host, port, got, time.Now().UTC().Format(time.RFC3339)); err != nil {
			// Fail closed: a security pin we couldn't durably record must
			// not silently degrade back to "accept anything" — that defeats
			// the whole point of this check.
			return fmt.Errorf("pinning host key: %w", err)
		}
		var pinned string
		if err := s.db.QueryRowContext(ctx, `SELECT fingerprint FROM ssh_known_hosts WHERE host = ? AND port = ?`, host, port).Scan(&pinned); err != nil {
			return fmt.Errorf("checking pinned host key: %w", err)
		}
		if pinned != got {
			return fmt.Errorf("host key for %s:%d changed (expected %s, got %s) — possible MITM, or the host was reinstalled/rekeyed", host, port, pinned, got)
		}
		return nil
	}
}

// forgetSSHHostKey is DELETE /ssh/known-hosts?host=&port= — drops a pinned
// host key so a reinstalled/rekeyed host can be re-pinned on next connect.
func (s *Server) forgetSSHHostKey(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	port, err := strconv.Atoi(r.URL.Query().Get("port"))
	if host == "" || err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "host and numeric port query parameters are required")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM ssh_known_hosts WHERE host = ? AND port = ?`, host, port)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErrorMsg(w, http.StatusNotFound, "no pinned host key for that host and port")
		return
	}
	s.audit(r, "ssh.forget_host_key", "ssh", fmt.Sprintf("%s:%d", host, port))
	w.WriteHeader(http.StatusNoContent)
}

func sweepSSHSessionsLocked(now time.Time) {
	for id, sess := range sshSessions {
		if now.After(sess.expires.Add(consoleSessionSweepAfter)) {
			delete(sshSessions, id)
		}
	}
}

type openSSHShellRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	// AuthType is "password" (default, when Password is set) or "key"; Key
	// holds the unencrypted PEM private key when AuthType is "key".
	AuthType string `json:"authType"`
	Key      string `json:"key"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// openSSHShell mints a hand-off session for a direct SSH connection — this
// bypasses Proxmox entirely (no PVE connection, no termproxy ticket), so it
// works for any host the user can reach and authenticate to: a guest without
// qemu-guest-agent, a node whose termproxy is unreachable/misbehaving, or any
// other box on the network. Credentials are held server-side only for the
// single WebSocket connect that follows (openSSHShell never returns them).
func (s *Server) openSSHShell(w http.ResponseWriter, r *http.Request) {
	var req openSSHShellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	req.Host = strings.TrimSpace(req.Host)
	req.Username = strings.TrimSpace(req.Username)
	authType := "password"
	secret := req.Password
	if req.AuthType == "key" {
		authType = "key"
		secret = req.Key
	}
	if req.Host == "" || req.Username == "" || secret == "" {
		writeErrorMsg(w, http.StatusBadRequest, "host, username, and password or key are required")
		return
	}

	sessionID := newSSHSession(req.Host, req.Port, req.Username, authType, secret, req.Cols, req.Rows)
	s.audit(r, "ssh.shell", "ssh", fmt.Sprintf("%s@%s:%d", req.Username, req.Host, req.Port))
	writeJSON(w, http.StatusOK, map[string]string{"sessionId": sessionID, "wsPath": "/ws/ssh/" + sessionID})
}

// openConnectionSSHShell mints an SSH hand-off session using a connection's
// own stored SSH credentials (see Connections settings) instead of asking
// the user to retype a username/password every time — the counterpart to
// openSSHShell's fully ad-hoc form.
func (s *Server) openConnectionSSHShell(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	var req struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // cols/rows are optional; a missing/empty body just uses the defaults below

	host, port, username, authType, secret, ok, err := s.connections.SSHCredentials(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeErrorMsg(w, http.StatusNotFound, "this connection has no SSH credentials configured — add them in Settings first")
		return
	}

	sessionID := newSSHSession(host, port, username, authType, secret, req.Cols, req.Rows)
	s.audit(r, "ssh.shell", "ssh", fmt.Sprintf("%s@%s:%d (connection %s)", username, host, port, connID))
	writeJSON(w, http.StatusOK, map[string]string{"sessionId": sessionID, "wsPath": "/ws/ssh/" + sessionID})
}

// newSSHSession records a short-lived hand-off entry and returns its id —
// shared by openSSHShell (ad-hoc credentials) and openConnectionSSHShell
// (a connection's stored ones).
func newSSHSession(host string, port int, username, authType, secret string, cols, rows int) string {
	if port <= 0 {
		port = 22
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	sessionID := uuid.NewString()
	sshSessionsMu.Lock()
	sweepSSHSessionsLocked(time.Now())
	sshSessions[sessionID] = sshSession{
		host: host, port: port, username: username, authType: authType, secret: secret,
		cols: cols, rows: rows, expires: time.Now().Add(sshSessionTTL),
	}
	sshSessionsMu.Unlock()
	return sessionID
}

// sshWebSocket upgrades to a WebSocket, dials the target over SSH using the
// credentials handed off by openSSHShell, and bridges a PTY shell to the
// browser — plain byte relay each direction (frame type, text or binary,
// doesn't matter; the frontend's ShellTerminal — shared with the Proxmox
// termproxy shell — sends keystrokes as text and reads either). No window
// resize control channel yet: same as the existing Proxmox shell view, the
// PTY just opens at the size handed off at mint time.
func (s *Server) sshWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")

	// Slot first: a 503 at the cap must not burn the single-use session.
	release, ok := acquireSessionSlot(w)
	if !ok {
		return
	}
	defer release()

	sshSessionsMu.Lock()
	sess, ok := sshSessions[sessionID]
	if ok {
		delete(sshSessions, sessionID)
	}
	sshSessionsMu.Unlock()

	if !ok || time.Now().After(sess.expires) {
		http.Error(w, "ssh session expired", http.StatusGone)
		return
	}

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	auth, err := sess.authMethod()
	if err != nil {
		slog.Warn("ssh auth setup failed", "host", sess.host, "error", err)
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
		return
	}
	addr := net.JoinHostPort(sess.host, fmt.Sprintf("%d", sess.port))
	config := &ssh.ClientConfig{
		User:    sess.username,
		Auth:    []ssh.AuthMethod{auth},
		Timeout: 10 * time.Second,
		// Trust-on-first-use: the first connection to a host:port pins its
		// key fingerprint (ssh_known_hosts table); every later connection is
		// checked against that pin, so a MITM after the first connection is
		// rejected instead of silently trusted. The first connection itself
		// is unverifiable without an out-of-band fingerprint — same as any
		// classic known_hosts flow the first time you connect to a host.
		HostKeyCallback: s.verifySSHHostKey(sess.host, sess.port),
	}
	sshConn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		slog.Warn("ssh dial failed", "host", sess.host, "port", sess.port, "error", err)
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte("connection failed: "+err.Error()))
		_ = clientConn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "ssh dial failed"),
			time.Now().Add(consoleWriteWait))
		return
	}
	defer sshConn.Close()

	session, err := sshConn.NewSession()
	if err != nil {
		slog.Warn("ssh session failed", "host", sess.host, "error", err)
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte("session failed: "+err.Error()))
		return
	}
	defer session.Close()

	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := session.RequestPty("xterm-256color", sess.rows, sess.cols, modes); err != nil {
		slog.Warn("ssh pty request failed", "host", sess.host, "error", err)
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte("pty request failed: "+err.Error()))
		return
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return
	}
	session.Stderr = session.Stdout // same terminal, no separate stream to juggle client-side

	if err := session.Shell(); err != nil {
		slog.Warn("ssh shell failed", "host", sess.host, "error", err)
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte("shell failed: "+err.Error()))
		return
	}

	slog.Info("ssh session opened", "host", sess.host, "port", sess.port, "username", sess.username)
	errc := make(chan error, 2)

	// Guest -> browser.
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				_ = clientConn.SetWriteDeadline(time.Now().Add(consoleWriteWait))
				if werr := clientConn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					errc <- werr
					return
				}
			}
			if err != nil {
				errc <- err
				return
			}
		}
	}()

	// Keep-alive: same ping/read-deadline reaping as pipeWebsockets, so a
	// browser that vanished (laptop sleep, NAT drop) ends the session.
	_ = clientConn.SetReadDeadline(time.Now().Add(consolePongWait))
	clientConn.SetPongHandler(func(string) error {
		return clientConn.SetReadDeadline(time.Now().Add(consolePongWait))
	})
	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		ticker := time.NewTicker(consolePingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-stopPing:
				return
			case <-ticker.C:
				_ = clientConn.WriteControl(websocket.PingMessage, nil, time.Now().Add(consoleWriteWait))
			}
		}
	}()

	// Browser -> guest, plus resize control frames.
	go func() {
		for {
			_, msg, err := clientConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if _, err := stdin.Write(msg); err != nil {
				errc <- err
				return
			}
		}
	}()

	<-errc
	// Close before Wait: an interactive shell with stdin open never exits
	// on its own, so Wait alone would pin this goroutine and its slot forever.
	_ = session.Close()
	_ = sshConn.Close()
	_ = session.Wait()
	slog.Info("ssh session closed", "host", sess.host, "port", sess.port)
}

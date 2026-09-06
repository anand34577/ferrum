package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"ferrum/internal/pve"
)

// consoleSession is a short-lived hand-off between the "open console" REST
// call (which mints a PVE VNC ticket) and the WebSocket upgrade that
// actually streams the console — mirrors PVE's own ticket pattern, scoped
// to this app's session id instead of exposing the raw PVE ticket to the
// browser's URL/query string.
type consoleSession struct {
	connectionID string
	guestType    string // "qemu" | "lxc" | "" for a node-level shell
	node         string
	vmid         int // 0 for a node-level shell
	port         string
	ticket       string
	expires      time.Time
}

var (
	consoleSessionsMu sync.Mutex
	consoleSessions   = map[string]consoleSession{}
)

const (
	consoleSessionTTL  = 60 * time.Second
	consoleWriteWait   = 10 * time.Second
	consolePongWait    = 60 * time.Second
	consolePingPeriod  = 50 * time.Second // must be < consolePongWait
	consoleDialTimeout = 15 * time.Second
	// How long after expiry an unconsumed hand-off entry is kept before the
	// sweeper deletes it.
	consoleSessionSweepAfter = 2 * time.Minute
)

func vmidParam(r *http.Request) (int, error) {
	return strconv.Atoi(chi.URLParam(r, "vmid"))
}

// sweepConsoleSessions drops long-expired hand-off entries so a client that
// requests consoles but never upgrades can't grow the map unboundedly.
// Caller must hold consoleSessionsMu.
func sweepConsoleSessionsLocked(now time.Time) {
	for id, sess := range consoleSessions {
		if now.After(sess.expires.Add(consoleSessionSweepAfter)) {
			delete(consoleSessions, id)
		}
	}
}

func (s *Server) openGuestConsole(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	guestType := chi.URLParam(r, "type")
	node := chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	proxy, err := client.OpenVNCProxy(r.Context(), guestType, node, vmid)
	if err != nil {
		// Per-user action failure, not a system fault: say why (PVE's own
		// message when it has one) instead of a sanitized 500-style body.
		slog.Warn("console proxy request failed", "connectionId", connID, "guestType", guestType, "node", node, "vmid", vmid, "error", err)
		msg := strings.TrimSpace(err.Error())
		if strings.HasSuffix(msg, "{\"data\":null}") {
			msg = fmt.Sprintf("Proxmox refused to open a VNC console for this %s — try the Shell console instead (containers in TTY console mode need it).", guestType)
		}
		writeErrorMsg(w, http.StatusBadGateway, msg)
		return
	}

	sessionID := newConsoleSession(connID, guestType, node, vmid, proxy.Port, proxy.Ticket)
	s.audit(r, "vm.console", "vm", fmt.Sprintf("%s/%s/%d", node, guestType, vmid))
	// The PVE VNC ticket doubles as the VNC-level password — PVE challenges
	// for it during the RFB handshake after the WebSocket is up, so the
	// browser needs it to complete the connection. It's single-use and
	// expires with the session, same trust window as wsPath.
	writeJSON(w, http.StatusOK, map[string]string{"sessionId": sessionID, "wsPath": "/ws/console/" + sessionID, "password": proxy.Ticket})
}

// openGuestShell mints a termproxy (xterm.js shell) session, using the same
// hand-off/websocket-proxy plumbing as openGuestConsole's VNC session — PVE
// exposes both over the same vncwebsocket endpoint, keyed by whichever
// ticket kind was requested.
func (s *Server) openGuestShell(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	guestType := chi.URLParam(r, "type")
	node := chi.URLParam(r, "node")
	vmid, err := vmidParam(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	proxy, err := client.GuestTermProxy(r.Context(), guestType, node, vmid)
	if err != nil {
		slog.Warn("shell proxy request failed", "connectionId", connID, "guestType", guestType, "node", node, "vmid", vmid, "error", err)
		writeErrorMsg(w, http.StatusBadGateway, strings.TrimSpace(err.Error()))
		return
	}

	sessionID := newConsoleSession(connID, guestType, node, vmid, proxy.Port, proxy.Ticket)
	s.audit(r, "vm.shell", "vm", fmt.Sprintf("%s/%s/%d", node, guestType, vmid))
	writeJSON(w, http.StatusOK, map[string]string{"sessionId": sessionID, "wsPath": "/ws/console/" + sessionID})
}

// openNodeShell mints a termproxy session for the node's own host shell —
// same flow as openGuestShell but with no guest to scope it to.
func (s *Server) openNodeShell(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "id")
	node := chi.URLParam(r, "node")

	client, err := s.clientFor(r.Context(), connID)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	proxy, err := client.NodeTermProxy(r.Context(), node)
	if err != nil {
		slog.Warn("node shell proxy request failed", "connectionId", connID, "node", node, "error", err)
		writeErrorMsg(w, http.StatusBadGateway, strings.TrimSpace(err.Error()))
		return
	}

	sessionID := newConsoleSession(connID, "", node, 0, proxy.Port, proxy.Ticket)
	s.audit(r, "node.shell", "node", node)
	writeJSON(w, http.StatusOK, map[string]string{"sessionId": sessionID, "wsPath": "/ws/console/" + sessionID})
}

// newConsoleSession records a short-lived hand-off entry and returns its id —
// shared by the VNC, guest-shell, and node-shell "open a console" handlers.
func newConsoleSession(connID, guestType, node string, vmid int, port, ticket string) string {
	sessionID := uuid.NewString()
	consoleSessionsMu.Lock()
	sweepConsoleSessionsLocked(time.Now())
	consoleSessions[sessionID] = consoleSession{
		connectionID: connID, guestType: guestType, node: node, vmid: vmid,
		port: port, ticket: ticket, expires: time.Now().Add(consoleSessionTTL),
	}
	consoleSessionsMu.Unlock()
	return sessionID
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	// The console endpoint requires a single-use, short-lived session id
	// minted by an authenticated REST call, so a cross-origin upgrade can't
	// reach a console without that secret. Reject foreign origins anyway to
	// keep the WebSocket same-origin like every other route.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (curl, tests)
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

// consoleWebSocket proxies a browser WebSocket to Proxmox's authenticated
// VNC websocket endpoint using the ticket minted by openGuestConsole. The
// session id is single-use and expires quickly, so no long-lived PVE
// credential is ever exposed to the browser.
func (s *Server) consoleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")

	consoleSessionsMu.Lock()
	sess, ok := consoleSessions[sessionID]
	if ok {
		delete(consoleSessions, sessionID)
	}
	consoleSessionsMu.Unlock()

	if !ok || time.Now().After(sess.expires) {
		http.Error(w, "console session expired", http.StatusGone)
		return
	}

	host, port, verifyTLS, err := s.connectionHost(r.Context(), sess.connectionID)
	if err != nil {
		http.Error(w, "connection lookup failed", http.StatusBadGateway)
		return
	}
	client, err := s.clientFor(r.Context(), sess.connectionID)
	if err != nil {
		http.Error(w, "connection auth failed", http.StatusBadGateway)
		return
	}

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	// A guest (VNC or shell) session scopes the path to its qemu/lxc/vmid;
	// a node-level shell session (guestType == "") has no guest segment —
	// PVE serves both kinds of ticket through the same node-level endpoint.
	guestSegment := ""
	if sess.guestType != "" {
		guestSegment = fmt.Sprintf("/%s/%d", sess.guestType, sess.vmid)
	}
	pveURL := url.URL{
		Scheme:   "wss",
		Host:     fmt.Sprintf("%s:%d", host, port),
		Path:     fmt.Sprintf("/api2/json/nodes/%s%s/vncwebsocket", pve.PathEscape(sess.node), guestSegment),
		RawQuery: url.Values{"port": {sess.port}, "vncticket": {sess.ticket}}.Encode(),
	}

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: !verifyTLS}, //nolint:gosec // per-connection trust setting, mirrors REST client
		HandshakeTimeout: consoleDialTimeout,
		ReadBufferSize:   8192,
		WriteBufferSize:  8192,
	}
	header := http.Header{}
	headerKey, headerVal, cookieName, cookieVal := client.WSAuth()
	if headerKey != "" {
		header.Set(headerKey, headerVal)
	} else if cookieVal != "" {
		header.Set("Cookie", cookieName+"="+cookieVal)
	}

	upstream, _, err := dialer.Dial(pveURL.String(), header)
	if err != nil {
		slog.Warn("console upstream dial failed", "connectionId", sess.connectionID, "node", sess.node, "vmid", sess.vmid, "error", err)
		_ = clientConn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "upstream dial failed"),
			time.Now().Add(consoleWriteWait))
		return
	}
	defer upstream.Close()

	slog.Info("console session opened", "connectionId", sess.connectionID, "node", sess.node, "guestType", sess.guestType, "vmid", sess.vmid)
	pipeWebsockets(clientConn, upstream)
	slog.Info("console session closed", "connectionId", sess.connectionID, "node", sess.node, "vmid", sess.vmid)
}

func (s *Server) connectionHost(ctx context.Context, id string) (host string, port int, verifyTLS bool, err error) {
	return s.connections.Host(ctx, id)
}

// pipeWebsockets relays binary frames bidirectionally until either side
// closes. Keep-alive pings and read/write deadlines reap connections whose
// peer vanished (laptop sleep, NAT timeout) instead of pinning two
// goroutines per dead console forever.
func pipeWebsockets(a, b *websocket.Conn) {
	errc := make(chan error, 2)
	relay := func(dst, src *websocket.Conn) {
		_ = src.SetReadDeadline(time.Now().Add(consolePongWait))
		src.SetPongHandler(func(string) error {
			return src.SetReadDeadline(time.Now().Add(consolePongWait))
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
					_ = dst.WriteControl(websocket.PingMessage, nil, time.Now().Add(consoleWriteWait))
				}
			}
		}()

		for {
			msgType, msg, err := src.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := dst.SetWriteDeadline(time.Now().Add(consoleWriteWait)); err != nil {
				errc <- err
				return
			}
			if err := dst.WriteMessage(msgType, msg); err != nil {
				errc <- err
				return
			}
		}
	}
	go relay(a, b)
	go relay(b, a)
	<-errc
}

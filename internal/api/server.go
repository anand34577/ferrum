// Package api wires together HTTP handlers for Ferrum's REST API.
package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"ferrum/internal/auth"
	"ferrum/internal/connections"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

type Server struct {
	db          *store.DB
	auth        *auth.Service
	secrets     *secrets.Box
	connections *connections.Resolver
	options     ServerOptions
	webFS       fs.FS
	oidc        *auth.OIDCClient // nil when SSO isn't configured

	logins *loginLimiter
}

// ServerOptions carries deployment-level behavior that handlers need at
// request time (cookie flags, proxy awareness).
type ServerOptions struct {
	// SecureCookies marks the session cookie Secure unconditionally. When
	// BehindProxy is also set, cookies are additionally marked Secure for
	// any request whose X-Forwarded-Proto is https.
	SecureCookies bool
	// BehindProxy enables trusting X-Forwarded-For / X-Forwarded-Proto
	// headers set by the reverse proxy in front of Ferrum.
	BehindProxy bool
}

// SetOIDC attaches an SSO client built from config. Call it (or don't) once
// at startup — nil is the normal "SSO not configured" state.
func (s *Server) SetOIDC(client *auth.OIDCClient) {
	s.oidc = client
}

func New(db *store.DB, authSvc *auth.Service, secretBox *secrets.Box, opts ServerOptions) *Server {
	return &Server{
		db: db, auth: authSvc, secrets: secretBox, options: opts,
		connections: connections.New(db, secretBox),
		logins:      newLoginLimiter(),
	}
}

// cookieSecure reports whether the session cookie for this request should
// carry the Secure flag: either configured globally, or — behind a proxy —
// when the proxy says the browser connection is https.
func (s *Server) cookieSecure(r *http.Request) bool {
	if s.options.SecureCookies {
		return true
	}
	if s.options.BehindProxy && r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}

// maxRequestBody bounds JSON bodies (configs, VM specs, dashboard layouts).
const maxRequestBody = 2 << 20 // 2 MiB

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if s.options.BehindProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.Recoverer)
	r.Use(s.securityHeaders)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Route("/api/v1", func(r chi.Router) {
		// Bound every JSON body; handlers never need more than a few KiB.
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Body != nil {
					r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
				}
				next.ServeHTTP(w, r)
			})
		})

		r.Get("/health", s.health)

		r.Route("/auth", func(r chi.Router) {
			r.Get("/setup-status", s.authSetupStatus)
			r.Post("/setup", s.authSetup)
			r.Post("/login", s.authLogin)
			r.Post("/login/totp", s.authLoginTOTP)
			r.Get("/oidc/config", s.oidcConfig)
			r.Get("/oidc/login", s.oidcLogin)
			r.Get("/oidc/callback", s.oidcCallback)
			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Post("/logout", s.authLogout)
				r.Get("/me", s.authMe)
				// Per-account UI settings (theme, …) — every user may set
				// their own, no admin required.
				r.Get("/me/preferences", s.getPreferences)
				r.Put("/me/preferences", s.putPreferences)
				r.Route("/2fa", func(r chi.Router) {
					r.Get("/status", s.totpStatus)
					r.Post("/enroll", s.totpEnroll)
					r.Post("/confirm", s.totpConfirm)
					r.Post("/disable", s.totpDisable)
				})
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Route("/connections", func(r chi.Router) {
				// Everything under /connections proxies a live PVE instance;
				// mutating it (power actions, deletes, firewall, HA, ...) is
				// an admin operation. Reads stay available to all users.
				r.Use(s.requireAdminForMutations)
				r.Get("/", s.listConnections)
				r.Post("/", s.createConnection)
				r.Post("/test", s.testConnection)

				r.Route("/{id}", func(r chi.Router) {
					r.Put("/", s.updateConnection)
					r.Delete("/", s.deleteConnection)

					r.Post("/vms", s.createVM)
					r.Post("/lxc", s.createLXC)
					r.Get("/nextid", s.nextGuestID)
					r.Get("/templates", s.listTemplates)

					r.Route("/guests/{type}/{node}/{vmid}", func(r chi.Router) {
						r.Post("/power/{action}", s.guestPowerAction)
						r.Post("/console", s.openGuestConsole)
						r.Get("/config", s.getGuestConfig)
						r.Put("/config", s.updateGuestConfig)
						r.Post("/clone", s.cloneGuest)
						r.Post("/migrate", s.migrateGuest)
						r.Post("/resize", s.resizeGuestDisk)
						r.Post("/template", s.setGuestTemplate)
						r.Post("/unlock", s.unlockGuest)
						r.Delete("/", s.deleteGuest)
						r.Get("/firewall/rules", s.guestFirewallRules)
						r.Post("/firewall/rules", s.addGuestFirewallRule)
						r.Delete("/firewall/rules/{pos}", s.deleteGuestFirewallRule)
						r.Get("/rrddata", s.guestRRDData)
						r.Get("/status", s.guestLiveStatus)
						r.Get("/agent/network", s.guestAgentNetwork)
						r.Get("/backups", s.guestBackups)

						r.Route("/snapshots", func(r chi.Router) {
							r.Get("/", s.listSnapshots)
							r.Post("/", s.createSnapshot)
							r.Post("/{snapname}/rollback", s.rollbackSnapshot)
							r.Delete("/{snapname}", s.deleteSnapshot)
						})
					})

					r.Route("/nodes/{node}", func(r chi.Router) {
						r.Get("/status", s.nodeStatus)
						r.Get("/rrddata", s.nodeRRDData)
						r.Post("/reboot", s.rebootNode)
						r.Post("/shutdown", s.shutdownNode)
						r.Get("/network", s.nodeNetwork)
						r.Get("/syslog", s.nodeSyslog)
						r.Get("/firewall/rules", s.nodeFirewallRules)
						r.Post("/firewall/rules", s.addNodeFirewallRule)
						r.Delete("/firewall/rules/{pos}", s.deleteNodeFirewallRule)
						r.Get("/replication", s.nodeReplicationStatus)
						r.Get("/subscription", s.nodeSubscription)

						r.Get("/storage", s.nodeStorageList)
						r.Get("/storage/{storage}/content", s.storageContent)
						r.Delete("/storage/{storage}/content/{volid}", s.deleteStorageContent)

						r.Get("/apt/updates", s.aptUpdates)
						r.Post("/apt/refresh", s.aptRefresh)
						r.Post("/apt/upgrade", s.aptUpgrade)

						r.Get("/ceph/status", s.cephStatus)
						r.Get("/ceph/pools", s.cephPools)
						r.Get("/ceph/osds", s.cephOSDs)

						r.Get("/tasks", s.nodeTasks)
						r.Get("/tasks/{upid}/log", s.taskLog)
						r.Delete("/tasks/{upid}", s.cancelTask)

						r.Post("/replication/{repId}/run", s.scheduleReplicationNow)
					})

					r.Route("/cluster", func(r chi.Router) {
						r.Get("/status", s.clusterStatus)
						r.Get("/log", s.clusterLog)
						r.Get("/firewall/rules", s.clusterFirewallRules)
						r.Post("/firewall/rules", s.addClusterFirewallRule)
						r.Delete("/firewall/rules/{pos}", s.deleteClusterFirewallRule)

						r.Get("/firewall/aliases", s.clusterFirewallAliases)
						r.Post("/firewall/aliases", s.addFirewallAlias)
						r.Delete("/firewall/aliases/{name}", s.deleteFirewallAlias)

						r.Get("/firewall/ipsets", s.clusterFirewallIPSets)
						r.Post("/firewall/ipsets", s.addFirewallIPSet)
						r.Delete("/firewall/ipsets/{name}", s.deleteFirewallIPSet)
						r.Get("/firewall/ipsets/{name}/entries", s.firewallIPSetEntries)
						r.Post("/firewall/ipsets/{name}/entries", s.addFirewallIPSetEntry)
						r.Delete("/firewall/ipsets/{name}/entries", s.deleteFirewallIPSetEntry)

						r.Get("/firewall/options", s.clusterFirewallOptions)
						r.Put("/firewall/options", s.updateClusterFirewallOptions)

						r.Get("/ha/resources", s.haResources)
						r.Post("/ha/resources", s.addHAResource)
						r.Delete("/ha/resources/{sid}", s.removeHAResource)
						r.Get("/ha/groups", s.haGroups)
						r.Get("/ha/status", s.haStatus)

						r.Get("/backup-jobs", s.backupJobs)
						r.Post("/backup-jobs", s.createBackupJob)
						r.Put("/backup-jobs/{jobId}", s.updateBackupJob)
						r.Delete("/backup-jobs/{jobId}", s.deleteBackupJob)
						r.Post("/backup-jobs/run", s.runBackupNow)
						r.Get("/replication-jobs", s.replicationJobs)

						r.Get("/options", s.datacenterOptions)
						r.Put("/options", s.updateDatacenterOptions)
					})

					r.Route("/pools", func(r chi.Router) {
						r.Get("/", s.listPools)
						r.Post("/", s.createPool)
						r.Route("/{poolId}", func(r chi.Router) {
							r.Get("/", s.poolDetail)
							r.Put("/members", s.setPoolMembers)
							r.Delete("/", s.deletePool)
						})
					})
				})
			})

			r.Route("/inventory", func(r chi.Router) {
				r.Get("/", s.inventoryOverview)
			})

			r.Get("/overview", s.fleetOverviewHandler)

			r.Route("/dashboards", func(r chi.Router) {
				r.Get("/", s.listDashboards)
				r.Post("/", s.createDashboard)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getDashboard)
					r.Put("/", s.updateDashboard)
					r.Delete("/", s.deleteDashboard)
				})
			})

			r.Route("/users", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/", s.listUsers)
				r.Post("/", s.createUser)
				r.Route("/{id}", func(r chi.Router) {
					r.Put("/", s.updateUser)
					r.Delete("/", s.deleteUser)
				})
			})
			r.Route("/admin", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/roles", s.listRoles)
				r.Get("/audit", s.auditLog)
			})
			// Compatibility: the frontend calls /roles and /audit directly;
			// keep the un-prefixed paths working but admin-only.
			r.Route("/roles", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/", s.listRoles)
			})
			r.Route("/audit", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/", s.auditLog)
			})

			r.Route("/alert-rules", func(r chi.Router) {
				r.Use(s.requireAdminForMutations)
				r.Get("/", s.listAlertRules)
				r.Post("/", s.createAlertRule)
				r.Delete("/{id}", s.deleteAlertRule)
			})
			r.Route("/alerts", func(r chi.Router) {
				r.Get("/", s.listAlerts)
				r.Get("/summary", s.alertsSummary)
				r.Post("/{id}/silence", s.silenceAlert)
			})
		})
	})

	// WebSocket console proxy — auth is validated via the console session ticket, not the cookie.
	r.Get("/ws/console/{sessionId}", s.consoleWebSocket)

	r.NotFound(s.serveSPA)

	return r
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- context helpers ---

type ctxKey string

const userCtxKey ctxKey = "user"

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.SessionCookieName)
		if err != nil {
			writeErrorMsg(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		user, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			writeErrorMsg(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)
		if u == nil || !u.IsAdmin {
			writeErrorMsg(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdminForMutations lets read-only requests (GET/HEAD/OPTIONS) through
// for any authenticated user but requires admin for anything that changes
// state. Applied to route trees that proxy live infrastructure.
func (s *Server) requireAdminForMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		u := userFromContext(r)
		if u == nil || !u.IsAdmin {
			writeErrorMsg(w, http.StatusForbidden, "admin access required for this operation")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets the baseline browser-protection headers on every
// response. The CSP allows inline styles (React/Recharts set style
// attributes at runtime) and same-origin WebSockets (the console proxy),
// but no inline scripts — the SPA is fully served from /assets/.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:; "+
				"font-src 'self'; "+
				"connect-src 'self' ws: wss:; "+
				"object-src 'none'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		if s.cookieSecure(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func userFromContext(r *http.Request) *auth.User {
	u, _ := r.Context().Value(userCtxKey).(*auth.User)
	return u
}

// audit records a best-effort audit log entry; failures are swallowed since
// auditing must never block the primary action.
func (s *Server) audit(r *http.Request, action, category, target string) {
	u := userFromContext(r)
	var userID *string
	if u != nil {
		userID = &u.ID
	}
	_, _ = s.db.ExecContext(r.Context(),
		`INSERT INTO audit_log (id, user_id, action, category, target, ip, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), userID, action, category, target, r.RemoteAddr, time.Now().UTC().Format(time.RFC3339),
	)
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError maps an error to a client response. 5xx responses are sanitized
// to a generic message (the details — SQL fragments, file paths, driver
// errors — are logged server-side instead); 4xx/502 messages are shown to
// the user because they describe actionable input or upstream problems.
func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	if status >= 500 {
		slog.Error("request failed", "status", status, "error", err)
		writeErrorMsg(w, status, "internal server error")
		return
	}
	writeErrorMsg(w, status, err.Error())
}

func writeErrorMsg(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Package api wires together HTTP handlers for Ferrum's REST API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"ferrum/internal/auth"
	"ferrum/internal/connections"
	"ferrum/internal/digest"
	"ferrum/internal/events"
	"ferrum/internal/mcp"
	"ferrum/internal/needle"
	"ferrum/internal/notify"
	"ferrum/internal/pbs"
	"ferrum/internal/poller"
	"ferrum/internal/pve"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

type Server struct {
	db          *store.DB
	auth        *auth.Service
	secrets     *secrets.Box
	connections *connections.Resolver
	mcp         *mcp.Server
	needle      *needle.Manager
	options     ServerOptions
	webFS       fs.FS

	oidcMu sync.RWMutex
	oidc   *auth.OIDCClient // nil when SSO isn't configured; guarded because the settings UI can replace it at any time

	evaluator       *poller.AlertEvaluator // its SetNotifier is called when the notification settings are saved; nil until SetAlertEvaluator is called
	digestScheduler *digest.Scheduler      // its SetNotifier is called when the notification settings are saved; nil until SetDigestScheduler is called
	notify          *notify.Notifier       // never nil — Notify is a no-op when no channel is enabled

	events   *events.Bus               // in-process pub/sub backing SSE and outgoing webhooks; nil until SetEventBus is called (see cmd/ferrum/main.go)
	webhooks *notify.WebhookDispatcher // nil until SetWebhookDispatcher is called

	securityMu       sync.RWMutex
	require2FAAdmins bool // enforced by requireTOTPEnrolled below; toggled live from Settings

	corsMu      sync.RWMutex
	corsOrigins []string // admin-configured CORS allow-list (system_settings.cors_allowed_origins); nil/empty means CORS stays off

	logins      *loginLimiter
	aiChatLimit *requestLimiter
}

// SetCORSOrigins swaps in the live CORS allow-list, applied by
// applySystemRow at boot and on every Settings save.
func (s *Server) SetCORSOrigins(origins []string) {
	s.corsMu.Lock()
	s.corsOrigins = origins
	s.corsMu.Unlock()
}

func (s *Server) corsOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	s.corsMu.RLock()
	defer s.corsMu.RUnlock()
	for _, o := range s.corsOrigins {
		if o == origin || o == "*" {
			return true
		}
	}
	return false
}

// SetRequire2FAAdmins toggles whether admin accounts without TOTP enabled
// are blocked from everything except /auth/2fa/* and /auth/me|logout until
// they enroll — see requireTOTPEnrolled.
func (s *Server) SetRequire2FAAdmins(v bool) {
	s.securityMu.Lock()
	s.require2FAAdmins = v
	s.securityMu.Unlock()
}

func (s *Server) getRequire2FAAdmins() bool {
	s.securityMu.RLock()
	defer s.securityMu.RUnlock()
	return s.require2FAAdmins
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
	// NeedleBinPath locates the optional Needle 2 CLI binary backing the
	// built-in AI provider (see internal/needle). Empty disables it.
	NeedleBinPath string
}

// SetOIDC swaps in an SSO client built from the current admin-configured (or
// config.yaml/env, at startup) settings. Called once at boot and again every
// time the OIDC settings are saved from the UI — nil is the normal "SSO not
// configured" state and clears any previously active client.
func (s *Server) SetOIDC(client *auth.OIDCClient) {
	s.oidcMu.Lock()
	s.oidc = client
	s.oidcMu.Unlock()
}

func (s *Server) getOIDC() *auth.OIDCClient {
	s.oidcMu.RLock()
	defer s.oidcMu.RUnlock()
	return s.oidc
}

// Notifier exposes the server's Gotify/SMTP dispatcher so main.go can hand
// the same instance to the alert evaluator at startup.
func (s *Server) Notifier() *notify.Notifier {
	return s.notify
}

// SetAlertEvaluator lets the notification settings handlers push a newly
// saved Gotify/SMTP config into the running poller without a restart.
func (s *Server) SetAlertEvaluator(e *poller.AlertEvaluator) {
	s.evaluator = e
}

// SetEventBus attaches the shared in-process event bus (see internal/events)
// that the SSE endpoint reads from. Called once at startup.
func (s *Server) SetEventBus(b *events.Bus) {
	s.events = b
}

// SetWebhookDispatcher attaches the outgoing-webhook dispatcher used by the
// admin webhook settings endpoints ("send test event"). Called once at
// startup.
func (s *Server) SetWebhookDispatcher(d *notify.WebhookDispatcher) {
	s.webhooks = d
}

// SetDigestScheduler mirrors SetAlertEvaluator for the fleet digest
// scheduler — lets the notification settings handler push a newly saved
// Gotify/SMTP config into it, and lets the /settings/digest/send-now
// handler trigger an immediate send.
func (s *Server) SetDigestScheduler(d *digest.Scheduler) {
	s.digestScheduler = d
}

func New(db *store.DB, authSvc *auth.Service, secretBox *secrets.Box, opts ServerOptions) *Server {
	resolver := connections.New(db, secretBox)
	s := &Server{
		db: db, auth: authSvc, secrets: secretBox, options: opts,
		connections: resolver,
		notify:      notify.New(notify.Settings{}),
		logins:      newLoginLimiter(),
		aiChatLimit: newRequestLimiter(aiChatMaxPerMinute, time.Minute),
		needle:      needle.NewManager(opts.NeedleBinPath),
	}
	s.mcp = mcp.New(resolver, db,
		func(ctx context.Context, userID, action, category, target string) {
			s.auditEntry(ctx, userID, action, category, target, "mcp")
		},
		s.recordToolCall,
	)
	// Lets internal/needle render its --tools file from the same tool
	// catalog the MCP server and AI Assistant already share, without
	// needing to import internal/mcp itself (see needle.SetToolCatalog).
	needle.SetToolCatalog(func() []needle.ToolDef {
		defs := mcp.ToolDefinitions()
		out := make([]needle.ToolDef, len(defs))
		for i, t := range defs {
			out[i] = needle.ToolDef{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}
		}
		return out
	})
	s.seedBuiltinNeedleProvider(context.Background())
	return s
}

// Close releases resources the server owns beyond the DB connection (which
// callers already close separately) — currently just the Needle subprocess,
// if one was ever started. Called once at shutdown (see cmd/ferrum/main.go).
func (s *Server) Close() {
	s.needle.Close()
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
	r.Use(s.requestLogger)
	r.Use(s.cors)
	r.Use(s.securityHeaders)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Route("/api/v1", func(r chi.Router) {
		// Cross-origin request check — see originCheck. Mounted on the whole
		// /api/v1 group (login included) because every state-changing route
		// beneath it is protected by the session cookie.
		r.Use(s.originCheck)

		// Bound every JSON body; handlers never need more than a few KiB. A
		// multipart upload (ISO/template files) sets its own, much larger
		// bound itself — see uploadStorageContent.
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Body != nil && !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
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
				r.Route("/apikeys", func(r chi.Router) {
					r.Get("/", s.listAPIKeys)
					r.Post("/", s.createAPIKey)
					r.Delete("/{id}", s.revokeAPIKey)
				})
				r.Get("/agent-status", s.getAgentStatus)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.requireTOTPEnrolled)

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
					r.Get("/health-score", s.connectionHealthScore)

					// PBS (Proxmox Backup Server) remote — mounted on a
					// connection whose stored type is "pbs" rather than
					// "pve"; pbsClientFor rejects any other type.
					r.Route("/pbs", func(r chi.Router) {
						r.Get("/datastores", s.pbsListDatastores)
						r.Route("/datastores/{store}", func(r chi.Router) {
							r.Get("/namespaces", s.pbsListNamespaces)
							r.Get("/groups", s.pbsListGroups)
							r.Get("/snapshots", s.pbsListSnapshots)
							r.Post("/snapshots/protected", s.pbsSetSnapshotProtected)
							r.Post("/prune", s.pbsPrune)
							r.Post("/gc", s.pbsStartGC)
							r.Get("/gc", s.pbsGCStatus)
						})
						r.Route("/sync-jobs", func(r chi.Router) {
							r.Get("/", s.pbsListSyncJobs)
							r.Route("/{jobId}", func(r chi.Router) {
								r.Get("/", s.pbsGetSyncJob)
								r.Post("/run", s.pbsRunSyncJob)
							})
						})
						r.Route("/verify-jobs", func(r chi.Router) {
							r.Get("/", s.pbsListVerifyJobs)
							r.Route("/{jobId}", func(r chi.Router) {
								r.Get("/", s.pbsGetVerifyJob)
								r.Post("/run", s.pbsRunVerifyJob)
							})
						})
						r.Get("/tasks/{upid}/status", s.pbsTaskStatus)
						r.Get("/tasks/{upid}/log", s.pbsTaskLog)
					})
					r.With(s.requireAdmin).Get("/certificates", s.connectionCertificates)

					r.Route("/export", func(r chi.Router) {
						// Exporting the full live inventory as IaC is
						// sensitive infrastructure detail — admin-only
						// regardless of the requireAdminForMutations
						// read-passthrough this route tree otherwise allows.
						r.Use(s.requireAdmin)
						r.Get("/terraform", s.exportTerraform)
						r.Get("/ansible", s.exportAnsible)
					})

					r.Route("/guests/{type}/{node}/{vmid}", func(r chi.Router) {
						r.Post("/power/{action}", s.guestPowerAction)
						r.Post("/console", s.openGuestConsole)
						r.Post("/shell", s.openGuestShell)
						r.Post("/sendkey", s.guestSendKey)
						r.Get("/config", s.getGuestConfig)
						r.Put("/config", s.updateGuestConfig)
						r.Put("/tags", s.updateGuestTags)
						r.Post("/baseline", s.captureGuestBaseline)
						r.Delete("/baseline", s.clearGuestBaseline)
						r.Get("/drift", s.getGuestDrift)
						r.Post("/clone", s.cloneGuest)
						r.Post("/migrate", s.migrateGuest)
						r.Get("/migrate", s.migratePrecondition)
						r.Post("/remote-migrate", s.remoteMigrateGuest)
						r.Post("/resize", s.resizeGuestDisk)
						r.Post("/move-disk", s.moveGuestDisk)
						r.Post("/template", s.setGuestTemplate)
						r.Post("/unlock", s.unlockGuest)
						r.Delete("/", s.deleteGuest)
						r.Get("/firewall/rules", s.guestFirewallRules)
						r.Post("/firewall/rules", s.addGuestFirewallRule)
						r.Delete("/firewall/rules/{pos}", s.deleteGuestFirewallRule)
						r.Get("/firewall/options", s.guestFirewallOptions)
						r.Put("/firewall/options", s.updateGuestFirewallOptions)
						r.Get("/rrddata", s.guestRRDData)
						r.Get("/status", s.guestLiveStatus)
						r.Get("/backups", s.guestBackups)

						r.Route("/agent", func(r chi.Router) {
							r.Get("/network", s.guestAgentNetwork)
							r.Post("/ping", s.guestAgentPing)
							r.Post("/exec", s.guestAgentExec)
							r.With(s.requireAdmin).Get("/exec-status", s.guestAgentExecStatus)
							r.Post("/fsfreeze/{action}", s.guestAgentFsfreeze)
							r.Post("/shutdown", s.guestAgentShutdown)
							r.Post("/set-password", s.guestAgentSetPassword)
							r.Get("/osinfo", s.guestAgentOSInfo)
							r.Get("/fsinfo", s.guestAgentFSInfo)
							r.Get("/vcpus", s.guestAgentVCPUs)
							r.Get("/hostname", s.guestAgentHostname)
							r.Get("/timezone", s.guestAgentTimezone)
							r.Post("/file-read", s.guestAgentFileRead)
							r.Post("/file-write", s.guestAgentFileWrite)
						})

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
						r.Get("/forecast", s.nodeForecast)
						r.Post("/reboot", s.rebootNode)
						r.Post("/shutdown", s.shutdownNode)
						r.Post("/wakeonlan", s.wakeOnLan)
						r.Post("/startall", s.startAllGuests)
						r.Post("/stopall", s.stopAllGuests)
						r.Post("/shell", s.openNodeShell)
						r.Get("/network", s.nodeNetwork)
						r.Get("/syslog", s.nodeSyslog)
						r.With(s.requireAdmin).Get("/journal", s.nodeJournal)
						r.Get("/services", s.nodeServices)
						r.Get("/services/{service}/state", s.nodeServiceState)
						r.Post("/services/{service}/{action}", s.nodeServiceAction)
						r.Get("/firewall/rules", s.nodeFirewallRules)
						r.Post("/firewall/rules", s.addNodeFirewallRule)
						r.Delete("/firewall/rules/{pos}", s.deleteNodeFirewallRule)
						r.Get("/replication", s.nodeReplicationStatus)
						r.Get("/subscription", s.nodeSubscription)

						r.Get("/storage", s.nodeStorageList)
						r.Get("/storage/{storage}/content", s.storageContent)
						r.Delete("/storage/{storage}/content/{volid}", s.deleteStorageContent)
						r.Put("/storage/{storage}/content/{volid}/protected", s.setBackupProtected)
						r.With(s.requireAdmin).Get("/storage/{storage}/file-restore", s.fileRestoreList)
						r.With(s.requireAdmin).Get("/storage/{storage}/file-restore/download", s.fileRestoreDownload)
						r.Post("/storage/{storage}/upload", s.uploadStorageContent)
						r.Post("/storage/{storage}/download-url", s.downloadURLToStorage)

						r.Get("/disks", s.nodeDisks)
						r.Get("/disks/smart", s.diskSMART)
						r.Post("/disks/wipedisk", s.wipeDisk)
						r.Post("/disks/initgpt", s.initGPT)
						r.Get("/disks/zfs", s.nodeZFSPools)
						r.Post("/disks/zfs", s.createZFSPool)
						r.Get("/disks/lvm", s.nodeLVMGroups)
						r.Post("/disks/lvm", s.createLVMStorage)
						r.Get("/disks/lvmthin", s.nodeLVMThinPools)
						r.Post("/disks/lvmthin", s.createLVMThinStorage)
						r.Post("/disks/directory", s.createDirectoryStorage)

						r.Get("/scan/nfs", s.scanNFS)
						r.Post("/scan/cifs", s.scanCIFS)
						r.Get("/scan/iscsi", s.scanISCSI)
						r.Get("/scan/lvm", s.scanLVM)
						r.Get("/scan/zfs", s.scanZFS)
						r.Get("/scan/glusterfs", s.scanGlusterFS)

						r.Get("/apt/updates", s.aptUpdates)
						r.Post("/apt/refresh", s.aptRefresh)
						r.Post("/apt/upgrade", s.aptUpgrade)

						r.Get("/ceph/status", s.cephStatus)
						r.Get("/ceph/pools", s.cephPools)
						r.Get("/ceph/osds", s.cephOSDs)
						r.Get("/ceph/mon", s.cephMons)
						r.Post("/ceph/mon", s.createCephMon)
						r.Delete("/ceph/mon/{monid}", s.deleteCephMon)
						r.Get("/ceph/mgr", s.cephMgrs)
						r.Post("/ceph/mgr", s.createCephMgr)
						r.Delete("/ceph/mgr/{mgrid}", s.deleteCephMgr)
						r.Get("/ceph/fs", s.cephFilesystems)
						r.Post("/ceph/fs", s.createCephFilesystem)

						r.With(s.requireAdmin).Get("/dns", s.nodeDNS)
						r.Put("/dns", s.updateNodeDNS)
						r.With(s.requireAdmin).Get("/time", s.nodeTime)
						r.Put("/time", s.updateNodeTimezone)
						r.With(s.requireAdmin).Get("/hosts", s.nodeHosts)
						r.Put("/hosts", s.updateNodeHosts)

						r.With(s.requireAdmin).Get("/certificates", s.nodeCertificates)
						r.Post("/certificates", s.uploadNodeCertificate)
						r.Delete("/certificates", s.deleteNodeCertificate)
						r.Post("/certificates/acme", s.orderAcmeCertificate)
						r.Delete("/certificates/acme", s.revokeAcmeCertificate)

						r.Delete("/cluster-membership", s.removeClusterNode)

						r.Get("/tasks", s.nodeTasks)
						r.Get("/tasks/{upid}/status", s.taskStatus)
						r.Get("/tasks/{upid}/log", s.taskLog)
						r.Delete("/tasks/{upid}", s.cancelTask)

						r.Post("/replication/{repId}/run", s.scheduleReplicationNow)
					})

					r.Route("/cluster", func(r chi.Router) {
						r.Get("/status", s.clusterStatus)
						r.Get("/log", s.clusterLog)
						r.Get("/tasks", s.clusterTasks)
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
						r.Put("/ha/resources/{sid}", s.updateHAResource)
						r.Delete("/ha/resources/{sid}", s.removeHAResource)
						r.Get("/ha/groups", s.haGroups)
						r.Post("/ha/groups", s.createHAGroup)
						r.Put("/ha/groups/{group}", s.updateHAGroup)
						r.Delete("/ha/groups/{group}", s.deleteHAGroup)
						r.Get("/ha/rules", s.listHARules)
						r.Post("/ha/rules", s.createHARule)
						r.Put("/ha/rules/{ruleId}", s.updateHARule)
						r.Delete("/ha/rules/{ruleId}", s.deleteHARule)
						r.Get("/ha/status", s.haStatus)

						r.Get("/backup-jobs", s.backupJobs)
						r.Post("/backup-jobs", s.createBackupJob)
						r.Put("/backup-jobs/{jobId}", s.updateBackupJob)
						r.Delete("/backup-jobs/{jobId}", s.deleteBackupJob)
						r.Post("/backup-jobs/run", s.runBackupNow)
						r.Get("/replication-jobs", s.replicationJobs)
						r.Post("/replication-jobs", s.createReplicationJob)
						r.Put("/replication-jobs/{repId}", s.updateReplicationJob)
						r.Delete("/replication-jobs/{repId}", s.deleteReplicationJob)

						r.Route("/firewall/groups", func(r chi.Router) {
							r.Get("/", s.firewallSecurityGroups)
							r.Post("/", s.createFirewallSecurityGroup)
							r.Route("/{name}", func(r chi.Router) {
								r.Get("/", s.securityGroupRules)
								r.Delete("/", s.deleteFirewallSecurityGroup)
								r.Post("/rules", s.addSecurityGroupRule)
								r.Delete("/rules/{pos}", s.deleteSecurityGroupRule)
							})
						})

						r.Get("/options", s.datacenterOptions)
						r.Put("/options", s.updateDatacenterOptions)

						r.Get("/config/nodes", s.clusterConfigNodes)
						r.Get("/config/join", s.clusterJoinInfo)
						r.Post("/config", s.createCluster)
						r.Post("/config/join", s.joinCluster)

						r.Route("/sdn", func(r chi.Router) {
							r.Post("/apply", s.applySDNConfig)
							r.Route("/zones", func(r chi.Router) {
								r.Get("/", s.sdnZones)
								r.Post("/", s.createSDNZone)
								r.Put("/{zone}", s.updateSDNZone)
								r.Delete("/{zone}", s.deleteSDNZone)
							})
							r.Route("/vnets", func(r chi.Router) {
								r.Get("/", s.sdnVnets)
								r.Post("/", s.createSDNVnet)
								r.Delete("/{vnet}", s.deleteSDNVnet)
								r.Get("/{vnet}/subnets", s.sdnSubnets)
								r.Post("/{vnet}/subnets", s.createSDNSubnet)
								r.Delete("/{vnet}/subnets", s.deleteSDNSubnet)
							})
							r.Route("/controllers", func(r chi.Router) {
								r.Get("/", s.sdnControllers)
								r.Post("/", s.createSDNController)
								r.Put("/{controller}", s.updateSDNController)
								r.Delete("/{controller}", s.deleteSDNController)
							})
							r.Route("/ipams", func(r chi.Router) {
								r.Get("/", s.sdnIPAMs)
								r.Post("/", s.createSDNIPAM)
								r.Put("/{ipam}", s.updateSDNIPAM)
								r.Delete("/{ipam}", s.deleteSDNIPAM)
							})
						})
					})

					r.Route("/storage", func(r chi.Router) {
						r.Post("/", s.createStorage)
						r.Put("/{storage}", s.updateStorage)
						r.Delete("/{storage}", s.deleteStorage)
					})

					r.Route("/access", func(r chi.Router) {
						r.Get("/users", s.pveAccessUsers)
						r.Get("/roles", s.pveAccessRoles)
						r.Get("/acl", s.pveAccessACL)
						r.Get("/domains", s.pveAccessDomains)
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

			r.Route("/tags", func(r chi.Router) {
				r.Get("/", s.listTags)
			})
			r.Get("/search", s.globalSearch)
			r.Route("/bulk", func(r chi.Router) {
				r.With(s.requireAdmin).Post("/guests/action", s.bulkGuestAction)
			})

			r.Get("/health-score", s.fleetHealthScore)

			r.Route("/forecast", func(r chi.Router) {
				r.Get("/capacity-warnings", s.fleetCapacityWarnings)
			})

			r.Route("/dashboards", func(r chi.Router) {
				r.Get("/", s.listDashboards)
				r.Post("/", s.createDashboard)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getDashboard)
					r.Put("/", s.updateDashboard)
					r.Delete("/", s.deleteDashboard)
				})
			})

			r.Route("/profile/sessions", func(r chi.Router) {
				r.Get("/", s.listMySessions)
				r.Delete("/", s.revokeMyOtherSessions)
				r.Delete("/{id}", s.revokeMySession)
			})

			r.Route("/users", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/", s.listUsers)
				r.Post("/", s.createUser)
				r.Route("/{id}", func(r chi.Router) {
					r.Put("/", s.updateUser)
					r.Delete("/", s.deleteUser)
					r.Route("/sessions", func(r chi.Router) {
						r.Get("/", s.listUserSessions)
						r.Delete("/", s.revokeUserSessions)
						r.Delete("/{sessionId}", s.revokeUserSession)
					})
				})
			})
			r.Route("/admin", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/roles", s.listRoles)
				r.Get("/audit", s.auditLog)

				r.Route("/settings", func(r chi.Router) {
					r.Get("/oidc", s.getOIDCSettings)
					r.Put("/oidc", s.putOIDCSettings)
					r.Get("/notifications", s.getNotificationSettings)
					r.Put("/notifications", s.putNotificationSettings)
					r.Post("/notifications/test", s.testNotification)
					r.Get("/security", s.getSecuritySettings)
					r.Put("/security", s.putSecuritySettings)
					r.Get("/defaults", s.getDefaultPreferences)
					r.Put("/defaults", s.putDefaultPreferences)
					r.Get("/agent", s.getAgentSettings)
					r.Put("/agent", s.putAgentSettings)
					r.Get("/system", s.getSystemSettings)
					r.Put("/system", s.putSystemSettings)
					r.Get("/digest", s.getDigestSettings)
					r.Put("/digest", s.putDigestSettings)
					r.Post("/digest/send-now", s.sendDigestNow)

					r.Route("/ai/providers", func(r chi.Router) {
						r.Get("/", s.listAIProviders)
						r.Post("/", s.createAIProvider)
						r.Post("/test", s.testAIProviderAdHoc)
						r.Route("/{id}", func(r chi.Router) {
							r.Put("/", s.updateAIProvider)
							r.Delete("/", s.deleteAIProvider)
							r.Post("/test", s.testAIProvider)
							r.Route("/models", func(r chi.Router) {
								r.Post("/", s.addAIModel)
								r.Put("/{modelId}", s.updateAIModel)
								r.Delete("/{modelId}", s.deleteAIModel)
							})
						})
					})
				})
				r.Get("/ai/activity", s.adminAIActivity)
			})

			r.Route("/ai", func(r chi.Router) {
				r.Get("/providers", s.listUsableAIProviders)
				r.With(s.rateLimitAIChat).Post("/chat", s.aiChat)
				r.Get("/activity", s.aiActivity)
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
				r.Put("/{id}", s.updateAlertRule)
				r.Delete("/{id}", s.deleteAlertRule)
			})
			r.Route("/alerts", func(r chi.Router) {
				r.Get("/", s.listAlerts)
				r.Get("/summary", s.alertsSummary)
				r.With(s.requireAdminForMutations).Post("/{id}/silence", s.silenceAlert)
				r.With(s.requireAdminForMutations).Post("/{id}/unsilence", s.unSilenceAlert)
			})
			r.Get("/connection-health", s.connectionHealth)

			// Server-Sent Events stream of bus activity (alert
			// triggers/resolutions, connection health, ...) — see
			// internal/api/events.go. The handler blocks on r.Context().Done()
			// for its lifetime; the router-wide middleware.Timeout above only
			// cancels that context, it doesn't itself cut the connection.
			r.Get("/events", s.streamEvents)

			r.Route("/settings/webhooks", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/", s.listWebhooks)
				r.Post("/", s.createWebhook)
				r.Route("/{id}", func(r chi.Router) {
					r.Put("/", s.updateWebhook)
					r.Delete("/", s.deleteWebhook)
					r.Post("/test", s.testWebhook)
					r.Get("/deliveries", s.listWebhookDeliveries)
				})
			})

			r.Route("/drift", func(r chi.Router) {
				r.Get("/summary", s.driftSummary)
			})

			r.Route("/lifecycle", func(r chi.Router) {
				r.Get("/actions", s.lifecycleActions)
			})
			r.Route("/settings/lifecycle", func(r chi.Router) {
				r.Use(s.requireAdminForMutations)
				r.Get("/", s.getLifecycleSettings)
				r.Put("/", s.putLifecycleSettings)
			})
		})
	})

	// WebSocket console proxy — auth is validated via the console session ticket, not the cookie.
	r.Get("/ws/console/{sessionId}", s.consoleWebSocket)

	// MCP (Model Context Protocol) endpoint for external tools like Claude —
	// these clients carry an API key, never a browser cookie, so this route
	// lives outside the cookie-based /api/v1 group and enforces its own
	// bearer-only auth (see mcpAuth).
	r.Post("/mcp", s.mcpAuth(s.mcpHandler))

	r.NotFound(s.serveSPA)

	return r
}

// health is a liveness+readiness probe: it reports "ok" only if the database
// is actually reachable, so an orchestrator restarting a wedged container
// (rather than just an alive-but-stuck process) can tell the difference.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": "database unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- context helpers ---

type ctxKey string

const userCtxKey ctxKey = "user"

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticateRequest(r)
		if err != nil {
			writeErrorMsg(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticateRequest resolves either the session cookie (browser SPA) or an
// API key (3rd-party apps, MCP clients — see internal/api/apikeys.go) into
// the user making the request. The cookie is checked first since it's the
// overwhelmingly common case and requires no header parsing.
func (s *Server) authenticateRequest(r *http.Request) (*auth.User, error) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		if user, err := s.auth.Authenticate(r.Context(), cookie.Value); err == nil {
			return user, nil
		}
	}
	if key := bearerAPIKey(r); key != "" {
		agentSettings, err := s.loadAgentSettings(r.Context())
		if err != nil {
			return nil, err
		}
		if !agentSettings.apiEnabled {
			return nil, auth.ErrInvalidCredentials
		}
		return s.auth.AuthenticateAPIKey(r.Context(), key)
	}
	return nil, auth.ErrInvalidCredentials
}

// bearerAPIKey extracts an API key from the Authorization header ("Bearer
// <key>") or the X-API-Key header, for clients that can't set Authorization.
func bearerAPIKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.Header.Get("X-API-Key")
}

// requireTOTPEnrolled blocks admin accounts without TOTP enabled from every
// route in the group it's mounted on, when the admin-configured "require
// 2FA for admins" setting is on. Deliberately mounted only on the general
// protected group (/connections, /users, /dashboards, ...) — the /auth
// route tree (2fa/enroll, /auth/me, /auth/logout) lives in a separate group
// specifically so a blocked admin can still reach enrollment and sign out.
func (s *Server) requireTOTPEnrolled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)
		if s.getRequire2FAAdmins() && u != nil && u.IsAdmin && !u.TOTPEnabled {
			writeErrorCode(w, http.StatusForbidden, "totp_required", "your administrator requires two-factor authentication — enable it on your profile to continue")
			return
		}
		next.ServeHTTP(w, r)
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

// requestLogger emits one structured access-log line per request (method,
// path, status, duration, request ID) via slog, so ad-hoc handler-level
// slog calls aren't the only record of what the server did.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
			"remote", r.RemoteAddr,
		)
	})
}

// cors applies the admin-configured CORS allow-list (system_settings.
// cors_allowed_origins — see SetCORSOrigins) so a browser-based 3rd-party
// app can call the REST API with a bearer token from another origin. Off by
// default: an empty allow-list means no Access-Control-* headers are set at
// all, identical to pre-existing same-origin-only behavior. The SPA itself
// is unaffected either way since it's always same-origin.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.corsOriginAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
			h.Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originCheck is CSRF defense for every mutating route under /api/v1,
// complementing the session cookie. The threat: a cross-site form POST —
// login CSRF above all, which SameSite=Lax does not prevent for top-level
// navigations — would otherwise ride the victim's ambient cookie (or plant
// an attacker-chosen session via a forged login). Browsers mark cross-site
// requests with an Origin header (all POSTs) or Sec-Fetch-Site; curl and
// API-key clients send neither and are unaffected, as are the bearer-only
// MCP endpoint (outside this group) and the /ws console (single-use
// tickets). A configured CORS origin is allowed so a 3rd-party browser app
// keeps working.
func (s *Server) originCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			switch {
			case err != nil || u.Host == "":
				writeErrorMsg(w, http.StatusForbidden, "cross-origin request blocked")
			case strings.EqualFold(u.Host, s.requestHost(r)) || s.corsOriginAllowed(origin):
				next.ServeHTTP(w, r)
			default:
				writeErrorMsg(w, http.StatusForbidden, "cross-origin request blocked")
			}
			return
		}
		// No Origin (some browsers omit it on navigations): Sec-Fetch-Site
		// is the newer and more explicit marker, sent on every fetch.
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeErrorMsg(w, http.StatusForbidden, "cross-origin request blocked")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestHost is the host the client believes it is talking to. Behind a
// proxy the proxy may rewrite Host, so X-Forwarded-Host — when the
// deployment declared one is in front — is the externally visible host an
// Origin should be compared against.
func (s *Server) requestHost(r *http.Request) string {
	if s.options.BehindProxy {
		if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
			if i := strings.Index(fh, ","); i >= 0 { // multi-hop proxies append; the client-facing host comes first
				fh = fh[:i]
			}
			return strings.TrimSpace(fh)
		}
	}
	return r.Host
}

// rateLimitAIChat caps how often the authenticated caller can start a new
// /ai/chat completion — see aiChatMaxPerMinute. Keyed by user ID, so it
// applies the same whether the caller is the browser SPA (session cookie)
// or a 3rd-party app using an API key.
func (s *Server) rateLimitAIChat(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromContext(r)
		key := r.RemoteAddr
		if u != nil {
			key = u.ID
		}
		if allowed, retryAfter := s.aiChatLimit.Allow(key); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
			writeErrorMsg(w, http.StatusTooManyRequests, "too many AI chat requests — slow down and try again shortly")
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
		h.Set("Content-Security-Policy", s.csp(r))
		if s.cookieSecure(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// csp builds the per-request Content-Security-Policy. connect-src pins the
// WebSocket schemes to the request's own Host instead of a protocol-wide
// wildcard — the only WS client is the console proxy dialing back to the
// origin the page was served from. The Host is sanitized to a conservative
// charset before embedding so a crafted Host header can't smuggle extra CSP
// directives (header injection); with nothing safe to pin, it falls back to
// the old `ws: wss:` wildcards.
func (s *Server) csp(r *http.Request) string {
	wsSrc := "ws: wss:"
	if host := sanitizeCSPHost(s.requestHost(r)); host != "" {
		wsSrc = "ws://" + host + " wss://" + host
	}
	return "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob:; " +
		// data: is needed for the Topology page's SVG export
		// (html-to-image inlines @font-face as base64 data URIs so
		// the exported file is self-contained) — without it the
		// browser blocks that inlined @font-face while rendering
		// the export's foreignObject content.
		"font-src 'self' data:; " +
		"connect-src 'self' " + wsSrc + "; " +
		"object-src 'none'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"
}

// sanitizeCSPHost keeps only characters a real Host can contain (letters,
// digits, dots, hyphens, colons for the port, brackets for IPv6 literals) —
// anything else means the value isn't safe to embed in a response header.
func sanitizeCSPHost(host string) string {
	if host == "" {
		return ""
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == ':', r == '[', r == ']':
		default:
			return ""
		}
	}
	return host
}

func userFromContext(r *http.Request) *auth.User {
	u, _ := r.Context().Value(userCtxKey).(*auth.User)
	return u
}

// audit records a best-effort audit log entry; failures are swallowed since
// auditing must never block the primary action.
func (s *Server) audit(r *http.Request, action, category, target string) {
	u := userFromContext(r)
	var userID string
	if u != nil {
		userID = u.ID
	}
	s.auditEntry(r.Context(), userID, action, category, target, r.RemoteAddr)
}

// auditEntry is the shared insert behind both audit (REST handlers, which
// have an *http.Request for the IP) and the MCP server's AuditFunc (which
// doesn't — see mcp.New below, wired with ip="mcp").
func (s *Server) auditEntry(ctx context.Context, userID, action, category, target, ip string) {
	var userIDCol *string
	if userID != "" {
		userIDCol = &userID
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, user_id, action, category, target, ip, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), userIDCol, action, category, target, ip, time.Now().UTC().Format(time.RFC3339),
	)
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError maps an error to a client response. Upstream Proxmox 4xx
// rejections carry an actionable message ("bad prune options", an unknown
// parameter) — their status and message pass through so the user can fix
// what they sent. Upstream 5xx and any local 5xx stay sanitized to a generic
// message: an upstream 500 body can be a proxy error page or stack trace,
// and local errors can carry SQL fragments, file paths, or driver errors —
// the details are logged server-side instead. An upstream 401 also keeps the
// caller's status: the dashboard frontend signs the user out on any 401, and
// an expired PBS/PVE ticket must read as a broken connection, not log them
// out of Ferrum.
func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	var pveErr *pve.StatusError
	if errors.As(err, &pveErr) {
		s.writeUpstreamError(w, status, pveErr.StatusCode, pveErr.Message(), err)
		return
	}
	var pbsErr *pbs.StatusError
	if errors.As(err, &pbsErr) {
		s.writeUpstreamError(w, status, pbsErr.StatusCode, pbsErr.Message(), err)
		return
	}
	if status >= 500 {
		slog.Error("request failed", "status", status, "error", err)
		writeErrorMsg(w, status, "internal server error")
		return
	}
	writeErrorMsg(w, status, err.Error())
}

// writeUpstreamError responds for an upstream Proxmox StatusError: 4xx gets
// the upstream status and message (except 401, which must not surface as a
// dashboard-session 401 — see writeError), everything else stays sanitized.
func (s *Server) writeUpstreamError(w http.ResponseWriter, status, upstreamStatus int, upstreamMsg string, err error) {
	if upstreamStatus < 400 || upstreamStatus >= 500 {
		slog.Error("request failed", "status", status, "error", err)
		writeErrorMsg(w, status, "internal server error")
		return
	}
	if upstreamStatus == http.StatusUnauthorized {
		writeErrorMsg(w, status, upstreamMsg)
		return
	}
	writeErrorMsg(w, upstreamStatus, upstreamMsg)
}

func writeErrorMsg(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeErrorCode is writeErrorMsg plus a machine-readable code the frontend
// can switch on (e.g. "totp_required" to redirect to the enrollment page)
// without parsing the human-readable message.
func writeErrorCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

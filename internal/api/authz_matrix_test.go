package api

import (
	"net/http"
	"testing"
)

// This file is the authorization regression matrix: EVERY mutating
// (POST/PUT/PATCH/DELETE) route in the authenticated /api/v1 tree, table-
// driven from Router()'s declarations in server.go, asserted against a
// non-admin session. "Denied" routes must answer exactly 403; "allowed"
// routes must answer anything BUT 403 — handler-level 400/401/404/429 are
// fine, since only the authorization layer is under test here (empty JSON
// bodies keep the allowed routes from doing anything destructive).
//
// Not in scope: GET routes (mutations only here), the public pre-auth
// POSTs /auth/setup, /auth/login, /auth/login/totp, and the bearer-only
// POST /mcp which lives outside /api/v1 and never sees a session cookie.

type authzCase struct {
	method, path string
	body         any // nil = empty body; fine everywhere, handlers never run for denied routes
}

// userAllowed lists every mutating route any authenticated user may call.
// These were historically the source of over-restriction regressions, so
// they are asserted to NOT be admin-gated. /auth/logout is deliberately
// last: it revokes the session used for the other assertions.
func userAllowedRoutes() []authzCase {
	return []authzCase{
		// Per-account 2FA management (reachable even under the
		// requireTOTPEnrolled gate, which is off by default).
		{http.MethodPost, "/api/v1/auth/2fa/enroll", map[string]any{}},
		{http.MethodPost, "/api/v1/auth/2fa/confirm", map[string]any{}},
		{http.MethodPost, "/api/v1/auth/2fa/disable", map[string]any{}},
		// Own API keys.
		{http.MethodPost, "/api/v1/auth/apikeys/", map[string]any{}},
		{http.MethodDelete, "/api/v1/auth/apikeys/x", nil},
		// Own UI preferences.
		{http.MethodPut, "/api/v1/auth/me/preferences", map[string]any{}},
		// Own dashboards (per-user, no admin required).
		{http.MethodPost, "/api/v1/dashboards/", map[string]any{}},
		{http.MethodPut, "/api/v1/dashboards/x", map[string]any{}},
		{http.MethodDelete, "/api/v1/dashboards/x", nil},
		// Own sessions.
		{http.MethodDelete, "/api/v1/profile/sessions/", nil},
		{http.MethodDelete, "/api/v1/profile/sessions/x", nil},
		// AI chat: any user may ask; the per-user rate limiter may answer
		// 429, which still counts as "not an authorization failure".
		{http.MethodPost, "/api/v1/ai/chat", map[string]any{}},
		// Sign out — LAST, it kills the session.
		{http.MethodPost, "/api/v1/auth/logout", nil},
	}
}

// adminRequired lists every mutating route a non-admin must be refused on:
// the PVE/PBS proxy tree (requireAdminForMutations), user administration,
// instance settings, alert rules, alert silencing, webhooks, lifecycle
// settings, bulk actions. Path parameters use "x" — the 403 comes from the
// middleware before any handler logic (and therefore before any upstream
// PVE/PBS call), which is exactly what this matrix pins down.
func adminRequiredRoutes() []authzCase {
	const c = "/api/v1/connections/x" // parameterized connection id
	cases := []authzCase{
		// --- connection CRUD ---
		{http.MethodPost, "/api/v1/connections/", nil},
		{http.MethodPost, "/api/v1/connections/test", nil},
		{http.MethodPut, c + "/", nil},
		{http.MethodDelete, c + "/", nil},

		// --- guest/VM provisioning ---
		{http.MethodPost, c + "/vms", nil},
		{http.MethodPost, c + "/lxc", nil},

		// --- PBS remote mutations ---
		{http.MethodPost, c + "/pbs/datastores/x/snapshots/protected", nil},
		{http.MethodPost, c + "/pbs/datastores/x/prune", nil},
		{http.MethodPost, c + "/pbs/datastores/x/gc", nil},
		{http.MethodPost, c + "/pbs/datastores/x/sync-jobs/x/run", nil},
		{http.MethodPost, c + "/pbs/datastores/x/verify-jobs/x/run", nil},

		// --- guest operations ---
		{http.MethodPost, c + "/guests/x/x/x/power/x", nil},
		{http.MethodPost, c + "/guests/x/x/x/console", nil},
		{http.MethodPost, c + "/guests/x/x/x/shell", nil},
		{http.MethodPost, c + "/guests/x/x/x/sendkey", nil},
		{http.MethodPut, c + "/guests/x/x/x/config", nil},
		{http.MethodPut, c + "/guests/x/x/x/tags", nil},
		{http.MethodPost, c + "/guests/x/x/x/baseline", nil},
		{http.MethodDelete, c + "/guests/x/x/x/baseline", nil},
		{http.MethodPost, c + "/guests/x/x/x/clone", nil},
		{http.MethodPost, c + "/guests/x/x/x/migrate", nil},
		{http.MethodPost, c + "/guests/x/x/x/remote-migrate", nil},
		{http.MethodPost, c + "/guests/x/x/x/resize", nil},
		{http.MethodPost, c + "/guests/x/x/x/move-disk", nil},
		{http.MethodPost, c + "/guests/x/x/x/template", nil},
		{http.MethodPost, c + "/guests/x/x/x/unlock", nil},
		{http.MethodDelete, c + "/guests/x/x/x/", nil},
		{http.MethodPost, c + "/guests/x/x/x/firewall/rules", nil},
		{http.MethodDelete, c + "/guests/x/x/x/firewall/rules/x", nil},
		{http.MethodPut, c + "/guests/x/x/x/firewall/options", nil},
		{http.MethodPost, c + "/guests/x/x/x/snapshots/", nil},
		{http.MethodPost, c + "/guests/x/x/x/snapshots/x/rollback", nil},
		{http.MethodDelete, c + "/guests/x/x/x/snapshots/x", nil},

		// --- guest agent ---
		{http.MethodPost, c + "/guests/x/x/x/agent/ping", nil},
		{http.MethodPost, c + "/guests/x/x/x/agent/exec", nil},
		{http.MethodPost, c + "/guests/x/x/x/agent/fsfreeze/x", nil},
		{http.MethodPost, c + "/guests/x/x/x/agent/shutdown", nil},
		{http.MethodPost, c + "/guests/x/x/x/agent/set-password", nil},
		{http.MethodPost, c + "/guests/x/x/x/agent/file-read", nil},
		{http.MethodPost, c + "/guests/x/x/x/agent/file-write", nil},

		// --- node operations ---
		{http.MethodPost, c + "/nodes/x/reboot", nil},
		{http.MethodPost, c + "/nodes/x/shutdown", nil},
		{http.MethodPost, c + "/nodes/x/wakeonlan", nil},
		{http.MethodPost, c + "/nodes/x/startall", nil},
		{http.MethodPost, c + "/nodes/x/stopall", nil},
		{http.MethodPost, c + "/nodes/x/shell", nil},
		{http.MethodPost, c + "/nodes/x/services/x/x", nil},
		{http.MethodPost, c + "/nodes/x/firewall/rules", nil},
		{http.MethodDelete, c + "/nodes/x/firewall/rules/x", nil},
		{http.MethodDelete, c + "/nodes/x/storage/x/content/x", nil},
		{http.MethodPut, c + "/nodes/x/storage/x/content/x/protected", nil},
		{http.MethodPost, c + "/nodes/x/storage/x/upload", nil},
		{http.MethodPost, c + "/nodes/x/storage/x/download-url", nil},
		{http.MethodPost, c + "/nodes/x/disks/wipedisk", nil},
		{http.MethodPost, c + "/nodes/x/disks/initgpt", nil},
		{http.MethodPost, c + "/nodes/x/disks/zfs", nil},
		{http.MethodPost, c + "/nodes/x/disks/lvm", nil},
		{http.MethodPost, c + "/nodes/x/disks/lvmthin", nil},
		{http.MethodPost, c + "/nodes/x/disks/directory", nil},
		{http.MethodPost, c + "/nodes/x/scan/cifs", nil},
		{http.MethodPost, c + "/nodes/x/apt/refresh", nil},
		{http.MethodPost, c + "/nodes/x/apt/upgrade", nil},
		{http.MethodPost, c + "/nodes/x/ceph/mon", nil},
		{http.MethodDelete, c + "/nodes/x/ceph/mon/x", nil},
		{http.MethodPost, c + "/nodes/x/ceph/mgr", nil},
		{http.MethodDelete, c + "/nodes/x/ceph/mgr/x", nil},
		{http.MethodPost, c + "/nodes/x/ceph/fs", nil},
		{http.MethodPut, c + "/nodes/x/dns", nil},
		{http.MethodPut, c + "/nodes/x/time", nil},
		{http.MethodPut, c + "/nodes/x/hosts", nil},
		{http.MethodPost, c + "/nodes/x/certificates", nil},
		{http.MethodDelete, c + "/nodes/x/certificates", nil},
		{http.MethodPost, c + "/nodes/x/certificates/acme", nil},
		{http.MethodDelete, c + "/nodes/x/certificates/acme", nil},
		{http.MethodDelete, c + "/nodes/x/cluster-membership", nil},
		{http.MethodDelete, c + "/nodes/x/tasks/x", nil},
		{http.MethodPost, c + "/nodes/x/replication/x/run", nil},

		// --- cluster operations ---
		{http.MethodPost, c + "/cluster/firewall/rules", nil},
		{http.MethodDelete, c + "/cluster/firewall/rules/x", nil},
		{http.MethodPost, c + "/cluster/firewall/aliases", nil},
		{http.MethodDelete, c + "/cluster/firewall/aliases/x", nil},
		{http.MethodPost, c + "/cluster/firewall/ipsets", nil},
		{http.MethodDelete, c + "/cluster/firewall/ipsets/x", nil},
		{http.MethodPost, c + "/cluster/firewall/ipsets/x/entries", nil},
		{http.MethodDelete, c + "/cluster/firewall/ipsets/x/entries", nil},
		{http.MethodPut, c + "/cluster/firewall/options", nil},
		{http.MethodPost, c + "/cluster/firewall/groups/", nil},
		{http.MethodDelete, c + "/cluster/firewall/groups/x", nil},
		{http.MethodPost, c + "/cluster/firewall/groups/x/rules", nil},
		{http.MethodDelete, c + "/cluster/firewall/groups/x/rules/x", nil},
		{http.MethodPost, c + "/cluster/ha/resources", nil},
		{http.MethodPut, c + "/cluster/ha/resources/x", nil},
		{http.MethodDelete, c + "/cluster/ha/resources/x", nil},
		{http.MethodPost, c + "/cluster/ha/groups", nil},
		{http.MethodPut, c + "/cluster/ha/groups/x", nil},
		{http.MethodDelete, c + "/cluster/ha/groups/x", nil},
		{http.MethodPost, c + "/cluster/ha/rules", nil},
		{http.MethodPut, c + "/cluster/ha/rules/x", nil},
		{http.MethodDelete, c + "/cluster/ha/rules/x", nil},
		{http.MethodPost, c + "/cluster/backup-jobs", nil},
		{http.MethodPut, c + "/cluster/backup-jobs/x", nil},
		{http.MethodDelete, c + "/cluster/backup-jobs/x", nil},
		{http.MethodPost, c + "/cluster/backup-jobs/run", nil},
		{http.MethodPost, c + "/cluster/replication-jobs", nil},
		{http.MethodPut, c + "/cluster/replication-jobs/x", nil},
		{http.MethodDelete, c + "/cluster/replication-jobs/x", nil},
		{http.MethodPut, c + "/cluster/options", nil},
		{http.MethodPost, c + "/cluster/config", nil},
		{http.MethodPost, c + "/cluster/config/join", nil},
		{http.MethodPost, c + "/cluster/sdn/apply", nil},
		{http.MethodPost, c + "/cluster/sdn/zones/", nil},
		{http.MethodPut, c + "/cluster/sdn/zones/x", nil},
		{http.MethodDelete, c + "/cluster/sdn/zones/x", nil},
		{http.MethodPost, c + "/cluster/sdn/vnets/", nil},
		{http.MethodDelete, c + "/cluster/sdn/vnets/x", nil},
		{http.MethodPost, c + "/cluster/sdn/vnets/x/subnets", nil},
		{http.MethodDelete, c + "/cluster/sdn/vnets/x/subnets", nil},
		{http.MethodPost, c + "/cluster/sdn/controllers/", nil},
		{http.MethodPut, c + "/cluster/sdn/controllers/x", nil},
		{http.MethodDelete, c + "/cluster/sdn/controllers/x", nil},
		{http.MethodPost, c + "/cluster/sdn/ipams/", nil},
		{http.MethodPut, c + "/cluster/sdn/ipams/x", nil},
		{http.MethodDelete, c + "/cluster/sdn/ipams/x", nil},

		// --- storage & pools ---
		{http.MethodPost, c + "/storage/", nil},
		{http.MethodPut, c + "/storage/x", nil},
		{http.MethodDelete, c + "/storage/x", nil},
		{http.MethodPost, c + "/pools/", nil},
		{http.MethodPut, c + "/pools/x/members", nil},
		{http.MethodDelete, c + "/pools/x/", nil},

		// --- bulk actions ---
		{http.MethodPost, "/api/v1/bulk/guests/action", nil},

		// --- user administration ---
		{http.MethodPost, "/api/v1/users/", nil},
		{http.MethodPut, "/api/v1/users/x", nil},
		{http.MethodDelete, "/api/v1/users/x", nil},
		{http.MethodDelete, "/api/v1/users/x/sessions/", nil},
		{http.MethodDelete, "/api/v1/users/x/sessions/x", nil},

		// --- instance settings (admin group) ---
		{http.MethodPut, "/api/v1/admin/settings/oidc", nil},
		{http.MethodPut, "/api/v1/admin/settings/notifications", nil},
		{http.MethodPost, "/api/v1/admin/settings/notifications/test", nil},
		{http.MethodPut, "/api/v1/admin/settings/security", nil},
		{http.MethodPut, "/api/v1/admin/settings/defaults", nil},
		{http.MethodPut, "/api/v1/admin/settings/agent", nil},
		{http.MethodPut, "/api/v1/admin/settings/system", nil},
		{http.MethodPut, "/api/v1/admin/settings/digest", nil},
		{http.MethodPost, "/api/v1/admin/settings/digest/send-now", nil},
		{http.MethodPost, "/api/v1/admin/settings/ai/providers/", nil},
		{http.MethodPost, "/api/v1/admin/settings/ai/providers/test", nil},
		{http.MethodPut, "/api/v1/admin/settings/ai/providers/x", nil},
		{http.MethodDelete, "/api/v1/admin/settings/ai/providers/x", nil},
		{http.MethodPost, "/api/v1/admin/settings/ai/providers/x/test", nil},
		{http.MethodPost, "/api/v1/admin/settings/ai/providers/x/models/", nil},
		{http.MethodPut, "/api/v1/admin/settings/ai/providers/x/models/x", nil},
		{http.MethodDelete, "/api/v1/admin/settings/ai/providers/x/models/x", nil},

		// --- alert rules & silencing ---
		{http.MethodPost, "/api/v1/alert-rules/", nil},
		{http.MethodDelete, "/api/v1/alert-rules/x", nil},
		{http.MethodPost, "/api/v1/alerts/x/silence", nil},

		// --- webhooks ---
		{http.MethodPost, "/api/v1/settings/webhooks/", nil},
		{http.MethodPut, "/api/v1/settings/webhooks/x", nil},
		{http.MethodDelete, "/api/v1/settings/webhooks/x", nil},
		{http.MethodPost, "/api/v1/settings/webhooks/x/test", nil},

		// --- lifecycle settings ---
		{http.MethodPut, "/api/v1/settings/lifecycle/", nil},
	}
	return cases
}

// TestNonAdminAuthorizationMatrix seeds one admin and one non-admin session
// and asserts the full matrix above with the non-admin's cookie.
func TestNonAdminAuthorizationMatrix(t *testing.T) {
	e := newTestEnv(t)
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)
	_ = e.do(t, http.MethodPost, "/api/v1/users/", map[string]any{
		"username": "bob", "email": "bob@example.com", "password": "bob's own password",
	}, admin)
	bob := e.loginAs(t, "bob", "bob@example.com", "bob's own password", false)

	for _, tc := range adminRequiredRoutes() {
		rec := e.do(t, tc.method, tc.path, tc.body, bob)
		if rec.Code != http.StatusForbidden {
			t.Errorf("non-admin %s %s = %d, want 403 (body: %s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	for _, tc := range userAllowedRoutes() {
		rec := e.do(t, tc.method, tc.path, tc.body, bob)
		if rec.Code == http.StatusForbidden {
			t.Errorf("non-admin %s %s = 403, want the route to be user-allowed (got body: %s)", tc.method, tc.path, rec.Body.String())
		}
	}
}

// The mirror image: the same admin-required routes must NOT 403 for a real
// admin — a middleware regression that blocked everyone would otherwise
// still pass the non-admin half of the matrix.
func TestAdminPassesAdminRequiredRoutes(t *testing.T) {
	e := newTestEnv(t)
	admin := e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)

	for _, tc := range adminRequiredRoutes() {
		rec := e.do(t, tc.method, tc.path, tc.body, admin)
		if rec.Code == http.StatusForbidden {
			t.Errorf("admin %s %s = 403, want the authorization layer to allow admins (body: %s)", tc.method, tc.path, rec.Body.String())
		}
	}
}

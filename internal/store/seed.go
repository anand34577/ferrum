package store

import (
	"time"

	"github.com/google/uuid"
)

// permission is a built-in RBAC permission seeded on first run.
type permission struct {
	Key      string
	Category string
	Desc     string
}

var builtinPermissions = []permission{
	{"connections.manage", "connections", "Add, edit, and remove Proxmox connections"},
	{"vm.view", "vm", "View VM/LXC details"},
	{"vm.power", "vm", "Start, stop, shutdown, reset VMs/LXCs"},
	{"vm.migrate", "vm", "Migrate VMs/LXCs between nodes"},
	{"vm.console", "vm", "Open VNC/SPICE consoles"},
	{"vm.manage", "vm", "Create, clone, delete VMs/LXCs"},
	{"node.view", "node", "View node status and details"},
	{"node.manage", "node", "Manage node configuration"},
	{"storage.view", "storage", "View storage pools and content"},
	{"backup.view", "backup", "View backup jobs and history"},
	{"alerts.manage", "alerts", "Manage alert rules and silence alerts"},
	{"users.manage", "admin", "Manage users, roles, and permissions"},
	{"settings.manage", "admin", "Manage application settings"},
	{"audit.view", "admin", "View the audit log"},
}

type roleSeed struct {
	Name        string
	Description string
	Color       string
	Permissions []string // "*" = all
}

var builtinRoles = []roleSeed{
	{"Admin", "Full access to every module and setting", "#e11d48", []string{"*"}},
	{
		"Operator", "Day-to-day VM/node operations without user or settings management", "#2563eb",
		[]string{"vm.view", "vm.power", "vm.migrate", "vm.console", "vm.manage", "node.view", "storage.view", "backup.view", "alerts.manage"},
	},
	{
		"Viewer", "Read-only access across the fleet", "#6b7280",
		[]string{"vm.view", "vm.console", "node.view", "storage.view", "backup.view", "audit.view"},
	},
}

type alertRuleSeed struct {
	Name      string
	Metric    string
	Threshold float64
	Severity  string
}

var defaultAlertRules = []alertRuleSeed{
	{"Node CPU critical", "node_cpu", 90, "critical"},
	{"Node memory high", "node_mem", 90, "warning"},
	{"Node disk high", "node_disk", 90, "warning"},
	{"Guest CPU critical", "guest_cpu", 95, "critical"},
	{"Guest memory high", "guest_mem", 90, "warning"},
	{"Storage pool nearly full", "storage_usage", 90, "warning"},
}

// seedIfEmpty populates the built-in RBAC catalog and default roles on a fresh
// database. It does NOT create any user account — the first admin is always
// created interactively via the setup wizard, so no default credentials ever
// touch disk.
func seedIfEmpty(db *DB) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rbac_permissions`).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	permIDs := make(map[string]string, len(builtinPermissions))

	for _, p := range builtinPermissions {
		id := uuid.NewString()
		permIDs[p.Key] = id
		if _, err := tx.Exec(
			`INSERT INTO rbac_permissions (id, key, category, description) VALUES (?, ?, ?, ?)`,
			id, p.Key, p.Category, p.Desc,
		); err != nil {
			return false, err
		}
	}

	for _, r := range builtinRoles {
		roleID := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO rbac_roles (id, name, description, color, is_system, created_at) VALUES (?, ?, ?, ?, 1, ?)`,
			roleID, r.Name, r.Description, r.Color, now,
		); err != nil {
			return false, err
		}

		grant := r.Permissions
		if len(grant) == 1 && grant[0] == "*" {
			grant = make([]string, 0, len(builtinPermissions))
			for _, p := range builtinPermissions {
				grant = append(grant, p.Key)
			}
		}
		for _, key := range grant {
			if _, err := tx.Exec(
				`INSERT INTO rbac_role_permissions (role_id, permission_id) VALUES (?, ?)`,
				roleID, permIDs[key],
			); err != nil {
				return false, err
			}
		}
	}

	defaultSettings := map[string]string{
		"appearance.theme": "system",
		"app.name":         "Ferrum",
	}
	for k, v := range defaultSettings {
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`,
			k, v, now,
		); err != nil {
			return false, err
		}
	}

	for _, rule := range defaultAlertRules {
		if _, err := tx.Exec(
			`INSERT INTO alert_rules (id, name, metric, threshold, severity, enabled, created_at) VALUES (?, ?, ?, ?, ?, 1, ?)`,
			uuid.NewString(), rule.Name, rule.Metric, rule.Threshold, rule.Severity, now,
		); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

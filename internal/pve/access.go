package pve

import "context"

// AccessUser is one Proxmox account (/access/users) — distinct from this
// app's own local accounts, which only govern access to Ferrum itself.
type AccessUser struct {
	UserID  string `json:"userid"` // "user@realm"
	Enable  int    `json:"enable,omitempty"`
	Expire  int64  `json:"expire,omitempty"`
	Email   string `json:"email,omitempty"`
	First   string `json:"firstname,omitempty"`
	Last    string `json:"lastname,omitempty"`
	Comment string `json:"comment,omitempty"`
	Groups  string `json:"groups,omitempty"`

	// Tokens is populated only when AccessUsers is called with full=true —
	// each of the user's API tokens (id, not secret; PVE never returns a
	// token's secret after creation).
	Tokens []AccessUserToken `json:"tokens,omitempty"`
}

// AccessUserToken is one API token belonging to a user
// (/access/users/{userid}/token), embedded in AccessUser when listed with
// full=true.
type AccessUserToken struct {
	TokenID string `json:"tokenid"`
	Comment string `json:"comment,omitempty"`
	Expire  int64  `json:"expire,omitempty"`
	Privsep int    `json:"privsep,omitempty"`
}

// AccessUsers lists every Proxmox account. full also fetches each user's API
// tokens in the same call (PVE's own "full" query param).
func (c *Client) AccessUsers(ctx context.Context, full bool) ([]AccessUser, error) {
	q := ""
	if full {
		q = "?full=1"
	}
	var out struct {
		Data []AccessUser `json:"data"`
	}
	if err := c.get(ctx, "/access/users"+q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// AccessRole is one PVE permission role (/access/roles) — a named bundle of
// privileges (e.g. "PVEAdmin", "PVEVMUser"), built-in or custom.
type AccessRole struct {
	RoleID string `json:"roleid"`
	// Raw holds every "Priv.Name": 1 entry PVE returns for this role, e.g.
	// "VM.Console", "VM.PowerMgmt" — the shape varies per role so it's kept
	// raw rather than named field-by-field.
	Raw map[string]any `json:"raw,omitempty"`
}

func (c *Client) AccessRoles(ctx context.Context) ([]AccessRole, error) {
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := c.get(ctx, "/access/roles", &out); err != nil {
		return nil, err
	}
	roles := make([]AccessRole, 0, len(out.Data))
	for _, row := range out.Data {
		roleID, _ := row["roleid"].(string)
		roles = append(roles, AccessRole{RoleID: roleID, Raw: row})
	}
	return roles, nil
}

// AccessACLEntry is one access-control list entry (/access/acl) — grants a
// role to a user/group/token on a path (e.g. "/vms/100", "/", "/pool/prod").
type AccessACLEntry struct {
	Path      string `json:"path"`
	RoleID    string `json:"roleid"`
	UGID      string `json:"ugid"` // user/group/token id the role is granted to
	Type      string `json:"type"` // "user" | "group" | "token"
	Propagate int    `json:"propagate,omitempty"`
}

func (c *Client) AccessACL(ctx context.Context) ([]AccessACLEntry, error) {
	var out struct {
		Data []AccessACLEntry `json:"data"`
	}
	if err := c.get(ctx, "/access/acl", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// AccessDomain is one authentication realm (/access/domains) — "pam", "pve",
// or a configured LDAP/AD/OIDC realm guests and users can authenticate against.
type AccessDomain struct {
	Realm   string `json:"realm"`
	Type    string `json:"type"` // "pam" | "pve" | "ldap" | "ad" | "openid" | ...
	Comment string `json:"comment,omitempty"`
	Default int    `json:"default,omitempty"`
}

func (c *Client) AccessDomains(ctx context.Context) ([]AccessDomain, error) {
	var out struct {
		Data []AccessDomain `json:"data"`
	}
	if err := c.get(ctx, "/access/domains", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

package pve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// HAResource is one guest under Proxmox's built-in HA management.
type HAResource struct {
	SID         string `json:"sid"` // "qemu:100" or "ct:101"
	Type        string `json:"type"`
	State       string `json:"state,omitempty"`
	Group       string `json:"group,omitempty"`
	MaxRestart  int    `json:"max_restart,omitempty"`
	MaxRelocate int    `json:"max_relocate,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

func (c *Client) HAResources(ctx context.Context) ([]HAResource, error) {
	var out struct {
		Data []HAResource `json:"data"`
	}
	if err := c.get(ctx, "/cluster/ha/resources", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) AddHAResource(ctx context.Context, sid, group string, maxRestart, maxRelocate int) error {
	form := url.Values{"sid": {sid}}
	if group != "" {
		form.Set("group", group)
	}
	if maxRestart > 0 {
		form.Set("max_restart", fmt.Sprintf("%d", maxRestart))
	}
	if maxRelocate > 0 {
		form.Set("max_relocate", fmt.Sprintf("%d", maxRelocate))
	}
	return c.post(ctx, "/cluster/ha/resources", form, nil)
}

func (c *Client) RemoveHAResource(ctx context.Context, sid string) error {
	return c.delete(ctx, "/cluster/ha/resources/"+url.PathEscape(sid), nil, nil)
}

// UpdateHAResource edits an existing HA-managed guest's group/restart/relocate
// settings in place (PUT /cluster/ha/resources/{sid}) — previously the only
// way to change these was to remove and re-add the resource.
func (c *Client) UpdateHAResource(ctx context.Context, sid, group string, maxRestart, maxRelocate int, comment string) error {
	form := url.Values{}
	if group != "" {
		form.Set("group", group)
	}
	if maxRestart >= 0 {
		form.Set("max_restart", fmt.Sprintf("%d", maxRestart))
	}
	if maxRelocate >= 0 {
		form.Set("max_relocate", fmt.Sprintf("%d", maxRelocate))
	}
	if comment != "" {
		form.Set("comment", comment)
	}
	return c.put(ctx, "/cluster/ha/resources/"+url.PathEscape(sid), form, nil)
}

// HAGroup is a named set of preferred nodes for HA resource placement.
type HAGroup struct {
	Group      string `json:"group"`
	Nodes      string `json:"nodes"`
	Restricted int    `json:"restricted,omitempty"`
	Nofailback int    `json:"nofailback,omitempty"`
	Comment    string `json:"comment,omitempty"`
}

// HAGroups lists HA groups. Proxmox 9 retired groups in favor of HA rules
// and the legacy endpoint answers 500 "ha groups have been migrated to
// rules" — that's a permanent absence of groups, not a fault, so it reads
// as an empty list (the UI explains the migration where it matters).
func (c *Client) HAGroups(ctx context.Context) ([]HAGroup, error) {
	var out struct {
		Data []HAGroup `json:"data"`
	}
	if err := c.get(ctx, "/cluster/ha/groups", &out); err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.StatusCode == 500 && strings.Contains(se.Body, "migrated to rules") {
			return []HAGroup{}, nil
		}
		return nil, err
	}
	return out.Data, nil
}

// HAStatus is one row of /cluster/ha/status/current — manager/CRM/LRM and
// per-resource HA state.
type HAStatus struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status,omitempty"`
	Node   string `json:"node,omitempty"`
}

func (c *Client) HAStatusCurrent(ctx context.Context) ([]HAStatus, error) {
	var out struct {
		Data []HAStatus `json:"data"`
	}
	if err := c.get(ctx, "/cluster/ha/status/current", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CreateHAGroup defines a new HA group (PVE 8 only — see HAGroups). nodes is
// PVE's own comma-separated "node[:priority],..." syntax, passed through
// verbatim.
func (c *Client) CreateHAGroup(ctx context.Context, group, nodes string, restricted, nofailback bool, comment string) error {
	form := url.Values{"group": {group}, "nodes": {nodes}}
	if restricted {
		form.Set("restricted", "1")
	}
	if nofailback {
		form.Set("nofailback", "1")
	}
	if comment != "" {
		form.Set("comment", comment)
	}
	return c.post(ctx, "/cluster/ha/groups", form, nil)
}

// UpdateHAGroup edits an existing HA group's fields. opts is passed straight
// through as form values (nodes/restricted/nofailback/comment/delete) since
// only the caller knows which fields actually changed.
func (c *Client) UpdateHAGroup(ctx context.Context, group string, opts map[string]string) error {
	form := url.Values{}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.put(ctx, "/cluster/ha/groups/"+url.PathEscape(group), form, nil)
}

func (c *Client) DeleteHAGroup(ctx context.Context, group string) error {
	return c.delete(ctx, "/cluster/ha/groups/"+url.PathEscape(group), nil, nil)
}

// HARule is one row of /cluster/ha/rules — Proxmox 9's replacement for HA
// groups. A rule's Type ("node-affinity", "resource-affinity", ...)
// determines which extra fields apply (resources, nodes, affinity, strict,
// ...), so those live in Raw rather than as named struct fields.
type HARule struct {
	RuleID  string `json:"rule"`
	Type    string `json:"type"`
	Comment string `json:"comment,omitempty"`
	Disable int    `json:"disable,omitempty"`

	// Raw carries every rule-type-specific field (resources, nodes,
	// affinity, strict, ...) PVE returned but this struct doesn't name.
	Raw map[string]any `json:"raw,omitempty"`
}

// HARules lists HA rules. This endpoint doesn't exist before Proxmox 9 —
// older servers answer 501, which the transport already turns into a
// NotAvailableError (see isNotAvailable in client.go) — that's a permanent
// absence of rules on that server, not a fault, so it reads as an empty
// list, mirroring how HAGroups() treats groups having been migrated away.
func (c *Client) HARules(ctx context.Context) ([]HARule, error) {
	var out struct {
		Data []HARule `json:"data"`
	}
	if err := c.get(ctx, "/cluster/ha/rules", &out); err != nil {
		if IsNotAvailable(err) {
			return []HARule{}, nil
		}
		return nil, err
	}
	return out.Data, nil
}

// CreateHARule creates a rule. opts must include "type" plus whatever
// fields that type needs (e.g. "resources"/"nodes" for node-affinity,
// "resources"/"affinity" for resource-affinity) — passed through as a raw
// form rather than a fixed struct since fields vary by rule type.
func (c *Client) CreateHARule(ctx context.Context, id string, opts map[string]string) error {
	form := url.Values{"rule": {id}}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.post(ctx, "/cluster/ha/rules", form, nil)
}

func (c *Client) UpdateHARule(ctx context.Context, id string, opts map[string]string) error {
	form := url.Values{}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.put(ctx, "/cluster/ha/rules/"+url.PathEscape(id), form, nil)
}

func (c *Client) DeleteHARule(ctx context.Context, id string) error {
	return c.delete(ctx, "/cluster/ha/rules/"+url.PathEscape(id), nil, nil)
}

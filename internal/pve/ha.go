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

package pve

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// FirewallRule is one rule from a cluster/node/guest firewall rule set.
type FirewallRule struct {
	Pos     int    `json:"pos"`
	Type    string `json:"type"` // "in" | "out" | "group"
	Action  string `json:"action"`
	Enable  int    `json:"enable,omitempty"`
	Source  string `json:"source,omitempty"`
	Dest    string `json:"dest,omitempty"`
	Proto   string `json:"proto,omitempty"`
	Dport   string `json:"dport,omitempty"`
	Sport   string `json:"sport,omitempty"`
	Comment string `json:"comment,omitempty"`
	Macro   string `json:"macro,omitempty"`
}

func (c *Client) ClusterFirewallRules(ctx context.Context) ([]FirewallRule, error) {
	var out struct {
		Data []FirewallRule `json:"data"`
	}
	if err := c.get(ctx, "/cluster/firewall/rules", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) NodeFirewallRules(ctx context.Context, node string) ([]FirewallRule, error) {
	var out struct {
		Data []FirewallRule `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/firewall/rules", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) GuestFirewallRules(ctx context.Context, guestType, node string, vmid int) ([]FirewallRule, error) {
	var out struct {
		Data []FirewallRule `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/%s/%d/firewall/rules", PathEscape(node), PathEscape(guestType), vmid), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// NewFirewallRule describes a rule to add. Type/Action are required;
// everything else is optional and only sent when non-empty.
type NewFirewallRule struct {
	Type    string // "in" | "out"
	Action  string // "ACCEPT" | "DROP" | "REJECT" | a security group name
	Source  string
	Dest    string
	Proto   string
	Dport   string
	Sport   string
	Macro   string
	Comment string
	Enable  bool
}

func (r NewFirewallRule) form() url.Values {
	form := url.Values{"type": {r.Type}, "action": {r.Action}}
	if r.Source != "" {
		form.Set("source", r.Source)
	}
	if r.Dest != "" {
		form.Set("dest", r.Dest)
	}
	if r.Proto != "" {
		form.Set("proto", r.Proto)
	}
	if r.Dport != "" {
		form.Set("dport", r.Dport)
	}
	if r.Sport != "" {
		form.Set("sport", r.Sport)
	}
	if r.Macro != "" {
		form.Set("macro", r.Macro)
	}
	if r.Comment != "" {
		form.Set("comment", r.Comment)
	}
	form.Set("enable", boolTo01(r.Enable))
	return form
}

func (c *Client) AddClusterFirewallRule(ctx context.Context, rule NewFirewallRule) error {
	return c.post(ctx, "/cluster/firewall/rules", rule.form(), nil)
}

func (c *Client) DeleteClusterFirewallRule(ctx context.Context, pos int) error {
	return c.delete(ctx, "/cluster/firewall/rules/"+strconv.Itoa(pos), nil, nil)
}

func (c *Client) AddNodeFirewallRule(ctx context.Context, node string, rule NewFirewallRule) error {
	return c.post(ctx, fmt.Sprintf("/nodes/%s/firewall/rules", PathEscape(node)), rule.form(), nil)
}

func (c *Client) DeleteNodeFirewallRule(ctx context.Context, node string, pos int) error {
	return c.delete(ctx, fmt.Sprintf("/nodes/%s/firewall/rules/%d", PathEscape(node), pos), nil, nil)
}

func (c *Client) AddGuestFirewallRule(ctx context.Context, guestType, node string, vmid int, rule NewFirewallRule) error {
	return c.post(ctx, fmt.Sprintf("/nodes/%s/%s/%d/firewall/rules", PathEscape(node), PathEscape(guestType), vmid), rule.form(), nil)
}

func (c *Client) DeleteGuestFirewallRule(ctx context.Context, guestType, node string, vmid int, pos int) error {
	return c.delete(ctx, fmt.Sprintf("/nodes/%s/%s/%d/firewall/rules/%d", PathEscape(node), PathEscape(guestType), vmid, pos), nil, nil)
}

func boolTo01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// FirewallAlias is a named IP/CIDR reusable across rules.
type FirewallAlias struct {
	Name    string `json:"name"`
	CIDR    string `json:"cidr"`
	Comment string `json:"comment,omitempty"`
}

func (c *Client) ClusterFirewallAliases(ctx context.Context) ([]FirewallAlias, error) {
	var out struct {
		Data []FirewallAlias `json:"data"`
	}
	if err := c.get(ctx, "/cluster/firewall/aliases", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) AddFirewallAlias(ctx context.Context, name, cidr, comment string) error {
	form := url.Values{"name": {name}, "cidr": {cidr}}
	if comment != "" {
		form.Set("comment", comment)
	}
	return c.post(ctx, "/cluster/firewall/aliases", form, nil)
}

func (c *Client) DeleteFirewallAlias(ctx context.Context, name string) error {
	return c.delete(ctx, "/cluster/firewall/aliases/"+url.PathEscape(name), nil, nil)
}

// FirewallIPSet is a named group of CIDRs.
type FirewallIPSet struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

func (c *Client) ClusterFirewallIPSets(ctx context.Context) ([]FirewallIPSet, error) {
	var out struct {
		Data []FirewallIPSet `json:"data"`
	}
	if err := c.get(ctx, "/cluster/firewall/ipset", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) AddFirewallIPSet(ctx context.Context, name, comment string) error {
	form := url.Values{"name": {name}}
	if comment != "" {
		form.Set("comment", comment)
	}
	return c.post(ctx, "/cluster/firewall/ipset", form, nil)
}

func (c *Client) DeleteFirewallIPSet(ctx context.Context, name string) error {
	return c.delete(ctx, "/cluster/firewall/ipset/"+url.PathEscape(name), nil, nil)
}

// FirewallIPSetEntry is one CIDR member of an IP set.
type FirewallIPSetEntry struct {
	CIDR    string `json:"cidr"`
	Comment string `json:"comment,omitempty"`
	Nomatch int    `json:"nomatch,omitempty"`
}

func (c *Client) FirewallIPSetEntries(ctx context.Context, name string) ([]FirewallIPSetEntry, error) {
	var out struct {
		Data []FirewallIPSetEntry `json:"data"`
	}
	if err := c.get(ctx, "/cluster/firewall/ipset/"+url.PathEscape(name), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) AddFirewallIPSetEntry(ctx context.Context, setName, cidr, comment string) error {
	form := url.Values{"cidr": {cidr}}
	if comment != "" {
		form.Set("comment", comment)
	}
	return c.post(ctx, "/cluster/firewall/ipset/"+url.PathEscape(setName), form, nil)
}

func (c *Client) DeleteFirewallIPSetEntry(ctx context.Context, setName, cidr string) error {
	return c.delete(ctx, "/cluster/firewall/ipset/"+url.PathEscape(setName)+"/"+url.PathEscape(cidr), nil, nil)
}

type FirewallOptions struct {
	Enable int `json:"enable"`
}

func (c *Client) ClusterFirewallOptions(ctx context.Context) (*FirewallOptions, error) {
	var out struct {
		Data FirewallOptions `json:"data"`
	}
	if err := c.get(ctx, "/cluster/firewall/options", &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// UpdateClusterFirewallOptions flips the cluster-wide firewall master
// switch. Rules configured anywhere (cluster/node/guest) are inert while
// this is off — a common source of "I added a rule and nothing happened"
// confusion if it's not surfaced anywhere.
func (c *Client) UpdateClusterFirewallOptions(ctx context.Context, enable bool) error {
	return c.put(ctx, "/cluster/firewall/options", url.Values{"enable": {boolTo01(enable)}}, nil)
}

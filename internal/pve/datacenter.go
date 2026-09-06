package pve

import (
	"context"
	"encoding/json"
	"net/url"
)

// DatacenterOptions is the writable subset of /cluster/options — cluster-wide
// defaults rather than per-node settings. Proxmox returns/accepts a sparse
// object here; omitted fields are simply unset on the cluster.
type DatacenterOptions struct {
	Keyboard    string `json:"keyboard,omitempty"`
	Language    string `json:"language,omitempty"`
	HTTPProxy   string `json:"http_proxy,omitempty"`
	Console     string `json:"console,omitempty"` // "applet" | "vv" | "html5" | "xtermjs"
	EmailFrom   string `json:"email_from,omitempty"`
	Description string `json:"description,omitempty"`
	MacPrefix   string `json:"mac_prefix,omitempty"`
	MaxWorkers  int    `json:"max_workers,omitempty"`

	// Raw holds every key PVE returned, unfiltered — options this struct
	// doesn't name (bwlimit, migration, u2f, next-id, tag style, ...).
	Raw map[string]any `json:"raw,omitempty"`
}

func (c *Client) DatacenterOptions(ctx context.Context) (*DatacenterOptions, error) {
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := c.get(ctx, "/cluster/options", &out); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out.Data)
	if err != nil {
		return nil, err
	}
	opts := DatacenterOptions{Raw: out.Data}
	if err := json.Unmarshal(raw, &opts); err != nil {
		return nil, err
	}
	return &opts, nil
}

// UpdateDatacenterOptions writes the named fields plus, via extra, any
// option PVE supports that DatacenterOptions doesn't model by name (e.g.
// "bwlimit", "u2f", "next-id", "tag-style") — set a key to "" (empty
// string, not omitted) to delete/reset that option, matching PVE's own
// delete-by-empty-value convention for this endpoint.
func (c *Client) UpdateDatacenterOptions(ctx context.Context, opts DatacenterOptions, extra map[string]string) error {
	form := url.Values{}
	setIfNonEmptyURL(form, "keyboard", opts.Keyboard)
	setIfNonEmptyURL(form, "language", opts.Language)
	setIfNonEmptyURL(form, "http_proxy", opts.HTTPProxy)
	setIfNonEmptyURL(form, "console", opts.Console)
	setIfNonEmptyURL(form, "email_from", opts.EmailFrom)
	setIfNonEmptyURL(form, "description", opts.Description)
	setIfNonEmptyURL(form, "mac_prefix", opts.MacPrefix)
	for k, v := range extra {
		form.Set(k, v)
	}
	if len(form) == 0 {
		return nil
	}
	return c.put(ctx, "/cluster/options", form, nil)
}

func setIfNonEmptyURL(form url.Values, key, value string) {
	if value != "" {
		form.Set(key, value)
	}
}

// Subscription is a node's subscription/support status (/nodes/{node}/subscription).
type Subscription struct {
	Status      string `json:"status"` // "Active" | "NotFound" | "New" | ...
	Level       string `json:"level,omitempty"`
	ProductName string `json:"productname,omitempty"`
	NextDueDate string `json:"nextduedate,omitempty"`
	Message     string `json:"message,omitempty"`
	ServerID    string `json:"serverid,omitempty"`
	SignedIn    bool   `json:"-"`
}

func (c *Client) NodeSubscription(ctx context.Context, node string) (*Subscription, error) {
	var out struct {
		Data Subscription `json:"data"`
	}
	if err := c.get(ctx, "/nodes/"+url.PathEscape(node)+"/subscription", &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

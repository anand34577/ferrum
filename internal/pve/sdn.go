package pve

import (
	"context"
	"net/url"
	"strconv"
)

// SDNZone is one software-defined-network zone (/cluster/sdn/zones) — the
// isolation domain a vnet belongs to (simple, vlan, qinq, vxlan, evpn, ...).
type SDNZone struct {
	Zone    string `json:"zone"`
	Type    string `json:"type"`
	Nodes   string `json:"nodes,omitempty"`
	MTU     int    `json:"mtu,omitempty"`
	Pending int    `json:"pending,omitempty"`

	// Raw carries every zone-type-specific field (bridge, tag, vlan-protocol,
	// controller, peers, ...) PVE returned but this struct doesn't name.
	Raw map[string]any `json:"raw,omitempty"`
}

func (c *Client) SDNZones(ctx context.Context) ([]SDNZone, error) {
	var out struct {
		Data []SDNZone `json:"data"`
	}
	if err := c.get(ctx, "/cluster/sdn/zones", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CreateSDNZone creates a zone. opts must include "type"; everything else is
// zone-type-specific (e.g. "bridge" for a simple/vlan zone, "peers" for
// vxlan) so it's passed through as a raw form rather than a fixed struct.
func (c *Client) CreateSDNZone(ctx context.Context, zone string, opts map[string]string) error {
	form := url.Values{"zone": {zone}}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.post(ctx, "/cluster/sdn/zones", form, nil)
}

func (c *Client) UpdateSDNZone(ctx context.Context, zone string, opts map[string]string) error {
	form := url.Values{}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.put(ctx, "/cluster/sdn/zones/"+url.PathEscape(zone), form, nil)
}

func (c *Client) DeleteSDNZone(ctx context.Context, zone string) error {
	return c.delete(ctx, "/cluster/sdn/zones/"+url.PathEscape(zone), nil, nil)
}

// SDNVnet is one virtual network (/cluster/sdn/vnets) — the L2 segment
// guests attach to, scoped to a zone.
type SDNVnet struct {
	Vnet      string `json:"vnet"`
	Zone      string `json:"zone"`
	Alias     string `json:"alias,omitempty"`
	Tag       int    `json:"tag,omitempty"`
	VLANAware int    `json:"vlanaware,omitempty"`
	Pending   int    `json:"pending,omitempty"`
}

func (c *Client) SDNVnets(ctx context.Context) ([]SDNVnet, error) {
	var out struct {
		Data []SDNVnet `json:"data"`
	}
	if err := c.get(ctx, "/cluster/sdn/vnets", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) CreateSDNVnet(ctx context.Context, vnet, zone, alias string, tag int, vlanAware bool) error {
	form := url.Values{"vnet": {vnet}, "zone": {zone}}
	if alias != "" {
		form.Set("alias", alias)
	}
	if tag > 0 {
		form.Set("tag", strconv.Itoa(tag))
	}
	if vlanAware {
		form.Set("vlanaware", "1")
	}
	return c.post(ctx, "/cluster/sdn/vnets", form, nil)
}

func (c *Client) DeleteSDNVnet(ctx context.Context, vnet string) error {
	return c.delete(ctx, "/cluster/sdn/vnets/"+url.PathEscape(vnet), nil, nil)
}

// SDNSubnet is one IP subnet (/cluster/sdn/vnets/{vnet}/subnets) attached to
// a vnet — gives guests on it a gateway/DHCP range instead of relying on an
// external router.
type SDNSubnet struct {
	Subnet  string `json:"subnet"` // CIDR, also the subnet's id
	Type    string `json:"type"`
	Gateway string `json:"gateway,omitempty"`
	SNAT    int    `json:"snat,omitempty"`
}

func (c *Client) SDNSubnets(ctx context.Context, vnet string) ([]SDNSubnet, error) {
	var out struct {
		Data []SDNSubnet `json:"data"`
	}
	if err := c.get(ctx, "/cluster/sdn/vnets/"+url.PathEscape(vnet)+"/subnets", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) CreateSDNSubnet(ctx context.Context, vnet, cidr, gateway string, snat bool) error {
	form := url.Values{"subnet": {cidr}, "type": {"subnet"}}
	if gateway != "" {
		form.Set("gateway", gateway)
	}
	if snat {
		form.Set("snat", "1")
	}
	return c.post(ctx, "/cluster/sdn/vnets/"+url.PathEscape(vnet)+"/subnets", form, nil)
}

func (c *Client) DeleteSDNSubnet(ctx context.Context, vnet, subnet string) error {
	return c.delete(ctx, "/cluster/sdn/vnets/"+url.PathEscape(vnet)+"/subnets/"+url.PathEscape(subnet), nil, nil)
}

// AddSDNDHCPRange adds a DHCP range to a subnet. PVE's "dhcp-range" property
// is repeatable (multiple ranges per subnet), so this appends via the same
// PUT used for any other subnet update rather than replacing the whole set.
func (c *Client) AddSDNDHCPRange(ctx context.Context, vnet, subnet, startAddr, endAddr string) error {
	form := url.Values{"dhcp-range": {"start-address=" + startAddr + ",end-address=" + endAddr}}
	return c.put(ctx, "/cluster/sdn/vnets/"+url.PathEscape(vnet)+"/subnets/"+url.PathEscape(subnet), form, nil)
}

// SDNController is one SDN controller (/cluster/sdn/controllers) — the
// routing daemon config (evpn/bgp/faucet/...) a zone references to actually
// exchange routes. Without one, an evpn/bgp zone is configured but inert.
type SDNController struct {
	Controller string `json:"controller"`
	Type       string `json:"type"`

	// Raw carries every controller-type-specific field (asn, peers,
	// gateway-nodes, ...) PVE returned but this struct doesn't name.
	Raw map[string]any `json:"raw,omitempty"`
}

func (c *Client) SDNControllers(ctx context.Context) ([]SDNController, error) {
	var out struct {
		Data []SDNController `json:"data"`
	}
	if err := c.get(ctx, "/cluster/sdn/controllers", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CreateSDNController creates a controller. opts must include "type";
// everything else is controller-type-specific (e.g. "asn"/"peers" for bgp,
// "asn"/"gateway-nodes" for evpn) so it's passed through as a raw form
// rather than a fixed struct. The id form key is "controller" — the name
// PVE's own Controllers.pm create handler extracts (not "id", unlike the
// zone/vnet/subnet siblings which use their resource's own key too).
func (c *Client) CreateSDNController(ctx context.Context, controller string, opts map[string]string) error {
	form := url.Values{"controller": {controller}}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.post(ctx, "/cluster/sdn/controllers", form, nil)
}

func (c *Client) UpdateSDNController(ctx context.Context, controller string, opts map[string]string) error {
	form := url.Values{}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.put(ctx, "/cluster/sdn/controllers/"+url.PathEscape(controller), form, nil)
}

func (c *Client) DeleteSDNController(ctx context.Context, controller string) error {
	return c.delete(ctx, "/cluster/sdn/controllers/"+url.PathEscape(controller), nil, nil)
}

// SDNIPAM is one IPAM plugin config (/cluster/sdn/ipams) — where PVE tracks
// subnet/IP allocations, either its own ("pve") or an external system
// (netbox, phpipam).
type SDNIPAM struct {
	Ipam string `json:"ipam"`
	Type string `json:"type"`

	// Raw carries every ipam-type-specific field (url, token, section, ...)
	// PVE returned but this struct doesn't name.
	Raw map[string]any `json:"raw,omitempty"`
}

func (c *Client) SDNIPAMs(ctx context.Context) ([]SDNIPAM, error) {
	var out struct {
		Data []SDNIPAM `json:"data"`
	}
	if err := c.get(ctx, "/cluster/sdn/ipams", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CreateSDNIPAM creates an IPAM. opts must include "type"; everything else
// is ipam-type-specific (e.g. "url"/"token"/"section" for netbox/phpipam)
// so it's passed through as a raw form rather than a fixed struct. The id
// form key is "ipam" — the name PVE's own Ipams.pm create handler extracts.
func (c *Client) CreateSDNIPAM(ctx context.Context, ipam string, opts map[string]string) error {
	form := url.Values{"ipam": {ipam}}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.post(ctx, "/cluster/sdn/ipams", form, nil)
}

func (c *Client) UpdateSDNIPAM(ctx context.Context, ipam string, opts map[string]string) error {
	form := url.Values{}
	for k, v := range opts {
		form.Set(k, v)
	}
	return c.put(ctx, "/cluster/sdn/ipams/"+url.PathEscape(ipam), form, nil)
}

func (c *Client) DeleteSDNIPAM(ctx context.Context, ipam string) error {
	return c.delete(ctx, "/cluster/sdn/ipams/"+url.PathEscape(ipam), nil, nil)
}

// ApplySDNConfig commits pending SDN zone/vnet/subnet changes to the
// running config — PVE stages SDN edits and requires this explicit apply
// step (mirrors the "Apply" button in its own SDN UI) before they take effect.
func (c *Client) ApplySDNConfig(ctx context.Context) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.put(ctx, "/cluster/sdn", url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

package pve

import (
	"context"
	"net/http"
	"net/url"
)

// ClusterConfigNode is one member listed in /cluster/config/nodes — distinct
// from ClusterStatus in that it reflects the corosync membership config
// (what the cluster believes its members are) rather than live online state.
type ClusterConfigNode struct {
	Name   string `json:"name"`
	NodeID int    `json:"nodeid,omitempty"`
	Votes  int    `json:"quorum_votes,omitempty"`
	// PveAddr is an IP address (e.g. "10.0.0.5"), not a number — declaring it
	// int here made every response with a populated pve_addr fail to
	// unmarshal (json: cannot unmarshal string into Go struct field), which
	// 502'd the whole "Members" list.
	PveAddr string `json:"pve_addr,omitempty"`
}

func (c *Client) ClusterConfigNodes(ctx context.Context) ([]ClusterConfigNode, error) {
	var out struct {
		Data []ClusterConfigNode `json:"data"`
	}
	if err := c.get(ctx, "/cluster/config/nodes", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// RemoveClusterNode deletes a node from the cluster's corosync config. Only
// valid for a node that's already offline/decommissioned — PVE refuses this
// for a live member.
func (c *Client) RemoveClusterNode(ctx context.Context, node string) error {
	return c.delete(ctx, "/cluster/config/nodes/"+url.PathEscape(node), nil, nil)
}

// CreateCluster turns this standalone node into a one-node cluster named
// clusterName, the prerequisite for other nodes to join it.
func (c *Client) CreateCluster(ctx context.Context, clusterName string) error {
	return c.post(ctx, "/cluster/config", url.Values{"clustername": {clusterName}}, nil)
}

// ClusterJoinInfo is what /cluster/config/join returns on an existing
// cluster member — the fingerprint and node list a new node needs to join.
type ClusterJoinInfo struct {
	Fingerprint string `json:"fingerprint"`
	Nodelist    []struct {
		Name    string `json:"name"`
		PveAddr string `json:"pve_addr,omitempty"`
	} `json:"nodelist,omitempty"`
	Preferred string `json:"preferred_node,omitempty"`
}

// ClusterJoinInfo fetches this cluster's join info (call this against an
// EXISTING member, then pass its Fingerprint plus that member's address to
// JoinCluster on the NEW node).
func (c *Client) ClusterJoinInfo(ctx context.Context) (*ClusterJoinInfo, error) {
	var out struct {
		Data ClusterJoinInfo `json:"data"`
	}
	if err := c.get(ctx, "/cluster/config/join", &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// JoinCluster makes this (currently standalone) node join an existing
// cluster reachable at hostIP, authenticating with that cluster's
// root@pam password and the fingerprint ClusterJoinInfo returned for it.
// Runs on the stream client: the join blocks until the node has synced
// corosync state from the existing members, which can outrun the default
// 15s whole-request timeout.
func (c *Client) JoinCluster(ctx context.Context, hostIP, fingerprint, password string) error {
	form := url.Values{
		"hostname":    {hostIP},
		"fingerprint": {fingerprint},
		"password":    {password},
	}
	return c.doOn(ctx, c.streamClient, http.MethodPost, "/cluster/config/join", form, nil)
}

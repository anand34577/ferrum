package pve

import (
	"context"
	"fmt"
	"net/url"
)

// Pool is a Proxmox resource pool (/pools) — a named grouping of guests and
// storage that spans nodes, used to organize a fleet by team/project/tenant
// independent of which node or cluster something happens to live on.
type Pool struct {
	PoolID  string `json:"poolid"`
	Comment string `json:"comment,omitempty"`
}

// PoolDetail is the expanded view (/pools/{poolid}) including member resources.
type PoolDetail struct {
	PoolID  string            `json:"poolid"`
	Comment string            `json:"comment,omitempty"`
	Members []ClusterResource `json:"members,omitempty"`
}

func (c *Client) Pools(ctx context.Context) ([]Pool, error) {
	var out struct {
		Data []Pool `json:"data"`
	}
	if err := c.get(ctx, "/pools", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) PoolDetail(ctx context.Context, poolID string) (*PoolDetail, error) {
	var out struct {
		Data struct {
			Comment string            `json:"comment,omitempty"`
			Members []ClusterResource `json:"members,omitempty"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/pools/"+url.PathEscape(poolID), &out); err != nil {
		return nil, err
	}
	return &PoolDetail{PoolID: poolID, Comment: out.Data.Comment, Members: out.Data.Members}, nil
}

func (c *Client) CreatePool(ctx context.Context, poolID, comment string) error {
	form := url.Values{"poolid": {poolID}}
	if comment != "" {
		form.Set("comment", comment)
	}
	return c.post(ctx, "/pools", form, nil)
}

func (c *Client) DeletePool(ctx context.Context, poolID string) error {
	return c.delete(ctx, fmt.Sprintf("/pools/%s", url.PathEscape(poolID)), nil, nil)
}

// SetPoolMembers adds or removes vmids from a pool. Proxmox's PUT /pools/{id}
// takes a comma-separated "vmid" list plus a "delete" flag to remove instead
// of add.
func (c *Client) SetPoolMembers(ctx context.Context, poolID string, vmids []int, remove bool) error {
	if len(vmids) == 0 {
		return nil
	}
	csv := ""
	for i, v := range vmids {
		if i > 0 {
			csv += ","
		}
		csv += fmt.Sprintf("%d", v)
	}
	form := url.Values{"vmid": {csv}}
	if remove {
		form.Set("delete", "1")
	}
	return c.put(ctx, fmt.Sprintf("/pools/%s", url.PathEscape(poolID)), form, nil)
}

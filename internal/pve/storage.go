package pve

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Storage is one storage backend as seen cluster-wide (/cluster/resources
// type=storage) or per-node.
type Storage struct {
	Storage string `json:"storage"`
	Node    string `json:"node,omitempty"`
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Shared  int    `json:"shared,omitempty"`
	Active  int    `json:"active,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Used    int64  `json:"used,omitempty"`
	Avail   int64  `json:"avail,omitempty"`
}

func (c *Client) NodeStorage(ctx context.Context, node string) ([]Storage, error) {
	var out struct {
		Data []Storage `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/storage", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// StorageContentItem is one volume/ISO/template/backup file on a storage.
type StorageContentItem struct {
	VolID   string `json:"volid"`
	Content string `json:"content"` // "images" | "iso" | "vztmpl" | "backup" | ...
	Format  string `json:"format,omitempty"`
	Size    int64  `json:"size,omitempty"`
	VMID    int    `json:"vmid,omitempty"`
	CTime   int64  `json:"ctime,omitempty"`
}

func (c *Client) StorageContent(ctx context.Context, node, storage string) ([]StorageContentItem, error) {
	var out struct {
		Data []StorageContentItem `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/storage/%s/content", PathEscape(node), PathEscape(storage)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) DeleteStorageContent(ctx context.Context, node, storage, volid string) error {
	return c.delete(ctx, fmt.Sprintf("/nodes/%s/storage/%s/content/%s", PathEscape(node), PathEscape(storage), url.PathEscape(volid)), nil, nil)
}

// --- Ceph ---

type CephStatus struct {
	Health struct {
		Status string `json:"status"`
	} `json:"health"`
	PgMap struct {
		BytesUsed  int64 `json:"bytes_used"`
		BytesTotal int64 `json:"bytes_total"`
		BytesAvail int64 `json:"bytes_avail"`
		NumPgs     int   `json:"num_pgs"`
	} `json:"pgmap"`
	OSDMap struct {
		NumOSDs   int `json:"num_osds"`
		NumUpOSDs int `json:"num_up_osds"`
		NumInOSDs int `json:"num_in_osds"`
	} `json:"osdmap"`
}

// cephUnavailable reports whether err means "this node has no usable
// Ceph". Besides the explicit cases (501 not-implemented, "binary not
// installed"), PVE's ceph endpoints answer a bare 500 {"data":null} on
// nodes without a Ceph cluster — no message to sniff, so any upstream 5xx
// on these three endpoints is treated as not-configured rather than an
// error worth surfacing.
func cephUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if IsNotAvailable(err) {
		return true
	}
	code := StatusCodeOf(err)
	return code == 500 || code == 501
}

// CephStatus fetches overall Ceph cluster health from any cluster member.
func (c *Client) CephClusterStatus(ctx context.Context, node string) (*CephStatus, error) {
	var out struct {
		Data CephStatus `json:"data"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/ceph/status", PathEscape(node)), &out)
	if cephUnavailable(err) {
		return nil, &NotAvailableError{Method: http.MethodGet, Path: "/nodes/" + node + "/ceph/status", Err: err}
	}
	if err != nil {
		return nil, err
	}
	return &out.Data, nil
}

type CephPool struct {
	PoolName string  `json:"pool_name"`
	Size     int     `json:"size"`
	MinSize  int     `json:"min_size"`
	PgNum    int     `json:"pg_num"`
	Bytes    int64   `json:"bytes_used,omitempty"`
	Percent  float64 `json:"percent_used,omitempty"`
}

func (c *Client) CephPools(ctx context.Context, node string) ([]CephPool, error) {
	var out struct {
		Data []CephPool `json:"data"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/ceph/pools", PathEscape(node)), &out)
	if cephUnavailable(err) {
		return nil, &NotAvailableError{Method: http.MethodGet, Path: "/nodes/" + node + "/ceph/pools", Err: err}
	}
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

type CephOSD struct {
	ID     int    `json:"id"`
	Host   string `json:"host,omitempty"`
	Status string `json:"status,omitempty"`
	In     int    `json:"in,omitempty"`
	Up     int    `json:"up,omitempty"`
	Type   string `json:"type"`
}

func (c *Client) CephOSDs(ctx context.Context, node string) ([]CephOSD, error) {
	var out struct {
		Data struct {
			Children []CephOSD `json:"children"`
		} `json:"data"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/ceph/osd", PathEscape(node)), &out)
	if cephUnavailable(err) {
		return nil, &NotAvailableError{Method: http.MethodGet, Path: "/nodes/" + node + "/ceph/osd", Err: err}
	}
	if err != nil {
		return nil, err
	}
	return out.Data.Children, nil
}

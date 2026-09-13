package pve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
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
	VolID     string `json:"volid"`
	Content   string `json:"content"` // "images" | "iso" | "vztmpl" | "backup" | ...
	Format    string `json:"format,omitempty"`
	Size      int64  `json:"size,omitempty"`
	VMID      int    `json:"vmid,omitempty"`
	CTime     int64  `json:"ctime,omitempty"`
	Protected int    `json:"protected,omitempty"` // 1 = exempt from prune-backups deletion
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

// UpdateStorageContent edits metadata on one existing storage volume/backup
// archive (e.g. "protected"). form carries whatever fields the caller wants
// to change.
func (c *Client) UpdateStorageContent(ctx context.Context, node, storage, volid string, form url.Values) error {
	return c.put(ctx, fmt.Sprintf("/nodes/%s/storage/%s/content/%s", PathEscape(node), PathEscape(storage), url.PathEscape(volid)), form, nil)
}

// UploadStorageContent uploads an ISO image or container template file to a
// storage's content area. content is "iso" or "vztmpl". The file is streamed
// into a multipart body via an io.Pipe rather than buffered whole into
// memory first — callers can pass an HTTP request body directly.
func (c *Client) UploadStorageContent(ctx context.Context, node, storage, content, filename string, file io.Reader) (string, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			if err := mw.WriteField("content", content); err != nil {
				return err
			}
			part, err := mw.CreateFormFile("filename", filename)
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, file); err != nil {
				return err
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()

	var out struct {
		Data string `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/storage/%s/upload", PathEscape(node), PathEscape(storage))
	if err := c.PostMultipart(ctx, path, mw.FormDataContentType(), pr, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// DownloadURLToStorage asks the node to fetch a file directly from url into a
// storage's content area (e.g. an ISO mirror) instead of routing the bytes
// through this app — content is "iso" or "vztmpl". Async, returns a UPID.
func (c *Client) DownloadURLToStorage(ctx context.Context, node, storage, content, filename, downloadURL, checksum, checksumAlgorithm string) (string, error) {
	form := url.Values{
		"content":  {content},
		"filename": {filename},
		"url":      {downloadURL},
	}
	if checksum != "" {
		form.Set("checksum", checksum)
	}
	if checksumAlgorithm != "" {
		form.Set("checksum-algorithm", checksumAlgorithm)
	}
	var out struct {
		Data string `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/storage/%s/download-url", PathEscape(node), PathEscape(storage))
	if err := c.post(ctx, path, form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// --- Storage configuration (cluster-wide /storage, distinct from the
// per-node /nodes/{node}/storage status view above) ---

// CreateStorageOptions models the writable fields shared by every storage
// plugin type PVE supports (dir, nfs, cifs, lvm, lvmthin, zfspool, rbd, ...).
// Extra holds any plugin-specific fields this struct doesn't name (e.g.
// "server"/"export" for NFS, "pool"/"monhost" for RBD) so callers aren't
// blocked on this list growing to cover every plugin.
type CreateStorageOptions struct {
	Storage string
	Type    string // "dir" | "nfs" | "cifs" | "lvm" | "lvmthin" | "zfspool" | "rbd" | ...
	Content string // comma-separated: "images,iso,vztmpl,backup,rootdir,snippets"
	Nodes   string // comma-separated node restriction, "" = all nodes
	Shared  bool
	Disable bool
	Extra   map[string]string
}

func storageForm(opts CreateStorageOptions) url.Values {
	form := url.Values{}
	if opts.Type != "" {
		form.Set("type", opts.Type)
	}
	if opts.Content != "" {
		form.Set("content", opts.Content)
	}
	if opts.Nodes != "" {
		form.Set("nodes", opts.Nodes)
	}
	if opts.Shared {
		form.Set("shared", "1")
	}
	if opts.Disable {
		form.Set("disable", "1")
	}
	for k, v := range opts.Extra {
		form.Set(k, v)
	}
	return form
}

// CreateStorage registers a new storage backend cluster-wide.
func (c *Client) CreateStorage(ctx context.Context, opts CreateStorageOptions) error {
	form := storageForm(opts)
	form.Set("storage", opts.Storage)
	return c.post(ctx, "/storage", form, nil)
}

// UpdateStorage edits an existing storage backend's config.
func (c *Client) UpdateStorage(ctx context.Context, storage string, opts CreateStorageOptions) error {
	return c.put(ctx, "/storage/"+url.PathEscape(storage), storageForm(opts), nil)
}

// DeleteStorage removes a storage backend's configuration (the underlying
// data is untouched — this only unregisters it from PVE).
func (c *Client) DeleteStorage(ctx context.Context, storage string) error {
	return c.delete(ctx, "/storage/"+url.PathEscape(storage), nil, nil)
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
// Ceph". The transport itself flags the explicit cases (501
// not-implemented, "binary not installed") as NotAvailableError; the one
// remaining shape PVE emits on nodes without a Ceph cluster is a bare
// 500 {"data":null} — no message to sniff, so the body is matched here.
// Every other 5xx is a genuine failure that surfaces as an error instead
// of being swallowed into "not configured".
func cephUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if IsNotAvailable(err) {
		return true
	}
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusInternalServerError {
		return false
	}
	trimmed := strings.TrimSpace(se.Body)
	if trimmed == "" {
		return true
	}
	var parsed struct {
		Data    any            `json:"data"`
		Message string         `json:"message"`
		Errors  map[string]any `json:"errors"`
	}
	return json.Unmarshal([]byte(trimmed), &parsed) == nil &&
		parsed.Data == nil && parsed.Message == "" && len(parsed.Errors) == 0
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

// CephMon is one Ceph monitor daemon (/nodes/{node}/ceph/mon).
type CephMon struct {
	Name   string `json:"name"`
	Host   string `json:"host,omitempty"`
	Addr   string `json:"addr,omitempty"`
	Quorum int    `json:"quorum,omitempty"`
}

func (c *Client) CephMons(ctx context.Context, node string) ([]CephMon, error) {
	var out struct {
		Data []CephMon `json:"data"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/ceph/mon", PathEscape(node)), &out)
	if cephUnavailable(err) {
		return nil, &NotAvailableError{Method: http.MethodGet, Path: "/nodes/" + node + "/ceph/mon", Err: err}
	}
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CreateCephMon adds a new monitor daemon on node.
func (c *Client) CreateCephMon(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/ceph/mon", PathEscape(node)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

func (c *Client) DeleteCephMon(ctx context.Context, node, monid string) error {
	return c.delete(ctx, fmt.Sprintf("/nodes/%s/ceph/mon/%s", PathEscape(node), url.PathEscape(monid)), nil, nil)
}

// CephMgr is one Ceph manager daemon (/nodes/{node}/ceph/mgr).
type CephMgr struct {
	Host   string `json:"host,omitempty"`
	Addr   string `json:"addr,omitempty"`
	Active int    `json:"active,omitempty"`
}

func (c *Client) CephMgrs(ctx context.Context, node string) ([]CephMgr, error) {
	var out struct {
		Data []CephMgr `json:"data"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/ceph/mgr", PathEscape(node)), &out)
	if cephUnavailable(err) {
		return nil, &NotAvailableError{Method: http.MethodGet, Path: "/nodes/" + node + "/ceph/mgr", Err: err}
	}
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) CreateCephMgr(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/ceph/mgr", PathEscape(node)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

func (c *Client) DeleteCephMgr(ctx context.Context, node, id string) error {
	return c.delete(ctx, fmt.Sprintf("/nodes/%s/ceph/mgr/%s", PathEscape(node), url.PathEscape(id)), nil, nil)
}

// CephFS is one CephFS filesystem (/nodes/{node}/ceph/fs).
type CephFS struct {
	Name string `json:"name"`
}

func (c *Client) CephFilesystems(ctx context.Context, node string) ([]CephFS, error) {
	var out struct {
		Data []CephFS `json:"data"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/ceph/fs", PathEscape(node)), &out)
	if cephUnavailable(err) {
		return nil, &NotAvailableError{Method: http.MethodGet, Path: "/nodes/" + node + "/ceph/fs", Err: err}
	}
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CreateCephFilesystem creates a new CephFS (its backing metadata/data
// pools are created automatically, named "<name>_metadata"/"<name>_data").
func (c *Client) CreateCephFilesystem(ctx context.Context, node, name string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/ceph/fs", PathEscape(node)), url.Values{"name": {name}}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// --- PBS file-level restore (browsing/downloading a single file out of a
// backup archive without restoring the whole guest) ---

// FileRestoreEntry is one entry (file or directory) inside a backup archive,
// as reported by a storage's file-restore browser. Only meaningful for a PBS
// (Proxmox Backup Server) storage — local vzdump archives don't support
// browsing individual files.
type FileRestoreEntry struct {
	FilePath string `json:"filepath"`
	Type     string `json:"type"` // "f" (file) | "d" (directory)
	Size     int64  `json:"size,omitempty"`
	Mtime    int64  `json:"mtime,omitempty"`
}

// FileRestoreList lists the contents of path inside volume (a backup volid,
// e.g. "pbs-storage:backup/vm/100/2024-01-01T00:00:00Z"). path defaults to
// "/" when empty.
func (c *Client) FileRestoreList(ctx context.Context, node, storage, volume, path string) ([]FileRestoreEntry, error) {
	if path == "" {
		path = "/"
	}
	q := url.Values{"volume": {volume}, "filepath": {path}}
	var out struct {
		Data []FileRestoreEntry `json:"data"`
	}
	reqPath := fmt.Sprintf("/nodes/%s/storage/%s/file-restore/list?%s", PathEscape(node), PathEscape(storage), q.Encode())
	if err := c.get(ctx, reqPath, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// FileRestoreDownload streams one file/directory (a directory comes back as
// a zip) out of a backup archive. The caller MUST close the returned
// response body — it's handed back unread so a handler can proxy it
// straight to an HTTP client instead of buffering the whole file in memory.
func (c *Client) FileRestoreDownload(ctx context.Context, node, storage, volume, path string) (*http.Response, error) {
	q := url.Values{"volume": {volume}, "filepath": {path}}
	reqPath := fmt.Sprintf("/nodes/%s/storage/%s/file-restore/download?%s", PathEscape(node), PathEscape(storage), q.Encode())
	return c.StreamGet(ctx, reqPath)
}

// --- Storage discovery/scan (/nodes/{node}/scan/...) — lets an admin browse
// what's actually reachable (NFS exports, CIFS shares, iSCSI targets, ...)
// before registering it as storage, instead of typing an export path/IQN
// from memory or looking it up in the real Proxmox UI first.

// NFSExport is one export offered by an NFS server (/nodes/{node}/scan/nfs).
type NFSExport struct {
	Path    string `json:"path"`
	Options string `json:"options,omitempty"`
}

// ScanNFS lists the exports an NFS server advertises.
func (c *Client) ScanNFS(ctx context.Context, node, server string) ([]NFSExport, error) {
	var out struct {
		Data []NFSExport `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/scan/nfs?server=%s", PathEscape(node), url.QueryEscape(server))
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CIFSShare is one share offered by a CIFS/SMB server (/nodes/{node}/scan/cifs).
type CIFSShare struct {
	Share       string `json:"share"`
	Description string `json:"description,omitempty"`
}

// ScanCIFS lists the shares a CIFS/SMB server advertises. username/password/
// domain are optional — omitted from the request when empty, since most
// servers list their shares to an anonymous/guest query.
func (c *Client) ScanCIFS(ctx context.Context, node, server, username, password, domain string) ([]CIFSShare, error) {
	q := url.Values{"server": {server}}
	if username != "" {
		q.Set("username", username)
	}
	if password != "" {
		q.Set("password", password)
	}
	if domain != "" {
		q.Set("domain", domain)
	}
	var out struct {
		Data []CIFSShare `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/scan/cifs?%s", PathEscape(node), q.Encode()), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ISCSITarget is one target exposed by an iSCSI portal (/nodes/{node}/scan/iscsi).
type ISCSITarget struct {
	Target string `json:"target"`
	Portal string `json:"portal,omitempty"`
}

// ScanISCSI lists the targets an iSCSI portal exposes.
func (c *Client) ScanISCSI(ctx context.Context, node, portal string) ([]ISCSITarget, error) {
	var out struct {
		Data []ISCSITarget `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/scan/iscsi?portal=%s", PathEscape(node), url.QueryEscape(portal))
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// LVMVolumeGroupScan is one LVM volume group visible to the node, as reported
// by the scan endpoint (/nodes/{node}/scan/lvm) — distinct from
// NodeLVMVolumeGroups in disks.go, which lists groups the disk manager has
// already turned into physical-disk-backed pools.
type LVMVolumeGroupScan struct {
	Name string `json:"vg"`
	Size int64  `json:"size,omitempty"`
	Free int64  `json:"free,omitempty"`
}

// ScanLVM lists LVM volume groups visible to the node.
func (c *Client) ScanLVM(ctx context.Context, node string) ([]LVMVolumeGroupScan, error) {
	var out struct {
		Data []LVMVolumeGroupScan `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/scan/lvm", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ZFSPoolScan is one ZFS pool visible to the node (/nodes/{node}/scan/zfs).
type ZFSPoolScan struct {
	Name string `json:"pool"`
	Size int64  `json:"size,omitempty"`
	Free int64  `json:"free,omitempty"`
}

// ScanZFS lists ZFS pools visible to the node.
func (c *Client) ScanZFS(ctx context.Context, node string) ([]ZFSPoolScan, error) {
	var out struct {
		Data []ZFSPoolScan `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/scan/zfs", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// GlusterVolume is one volume offered by a GlusterFS server (/nodes/{node}/scan/glusterfs).
type GlusterVolume struct {
	Volname string `json:"volname"`
}

// ScanGlusterFS lists the volumes a GlusterFS server advertises.
func (c *Client) ScanGlusterFS(ctx context.Context, node, server string) ([]GlusterVolume, error) {
	var out struct {
		Data []GlusterVolume `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/scan/glusterfs?server=%s", PathEscape(node), url.QueryEscape(server))
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

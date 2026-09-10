package pbs

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// GCStatus is a datastore's garbage-collection state, embedded in the
// datastore status listing and also returned standalone by GCStatus below.
type GCStatus struct {
	Status            string `json:"status,omitempty"`
	IndexFileCount    int64  `json:"index-file-count,omitempty"`
	DiskBytes         int64  `json:"disk-bytes,omitempty"`
	DiskChunks        int64  `json:"disk-chunks,omitempty"`
	RemovedBytes      int64  `json:"removed-bytes,omitempty"`
	RemovedChunks     int64  `json:"removed-chunks,omitempty"`
	PendingBytes      int64  `json:"pending-bytes,omitempty"`
	PendingChunks     int64  `json:"pending-chunks,omitempty"`
	PendingBadBytes   int64  `json:"still-bad-bytes,omitempty"`
	LastGoodChunkSize int64  `json:"last-good-chunk-size,omitempty"`
}

// Datastore is one row of GET /admin/datastore — a configured datastore's
// live capacity and last GC outcome.
type Datastore struct {
	Store    string    `json:"store"`
	Total    int64     `json:"total,omitempty"`
	Used     int64     `json:"used,omitempty"`
	Avail    int64     `json:"avail,omitempty"`
	GCStatus *GCStatus `json:"gc-status,omitempty"`
}

// ListDatastores returns every datastore configured on this PBS host, with
// its current usage.
func (c *Client) ListDatastores(ctx context.Context) ([]Datastore, error) {
	var out struct {
		Data []Datastore `json:"data"`
	}
	if err := c.get(ctx, "/admin/datastore", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Namespace is one entry under a datastore's namespace tree
// (/admin/datastore/{store}/namespace).
type Namespace struct {
	Namespace string `json:"ns"`
	Comment   string `json:"comment,omitempty"`
}

// ListNamespaces lists the namespaces under a datastore. parent scopes the
// listing to a sub-tree ("" lists from the root).
func (c *Client) ListNamespaces(ctx context.Context, store, parent string) ([]Namespace, error) {
	q := url.Values{}
	if parent != "" {
		q.Set("parent", parent)
	}
	var out struct {
		Data []Namespace `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/admin/datastore/%s/namespace", PathEscape(store)), q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// BackupGroup is one backup group (a distinct backup-type/backup-id pair,
// e.g. "vm/100") within a datastore — GET /admin/datastore/{store}/groups.
type BackupGroup struct {
	BackupType  string `json:"backup-type"`
	BackupID    string `json:"backup-id"`
	LastBackup  int64  `json:"last-backup,omitempty"`
	BackupCount int    `json:"backup-count,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

// ListGroups lists the backup groups in a datastore namespace.
func (c *Client) ListGroups(ctx context.Context, store, namespace string) ([]BackupGroup, error) {
	q := url.Values{}
	if namespace != "" {
		q.Set("ns", namespace)
	}
	var out struct {
		Data []BackupGroup `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/admin/datastore/%s/groups", PathEscape(store)), q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Snapshot is one backup snapshot — GET /admin/datastore/{store}/snapshots.
type Snapshot struct {
	BackupType string   `json:"backup-type"`
	BackupID   string   `json:"backup-id"`
	BackupTime int64    `json:"backup-time"`
	Size       int64    `json:"size,omitempty"`
	Protected  bool     `json:"protected,omitempty"`
	Comment    string   `json:"comment,omitempty"`
	Files      []string `json:"files,omitempty"`
	Owner      string   `json:"owner,omitempty"`
	// Verification carries the last verify job's outcome for this snapshot,
	// when one has run ({"state":"ok"|"failed", "upid":"..."}).
	Verification map[string]any `json:"verification,omitempty"`
}

// ListSnapshotsOptions filters a snapshot listing. All fields optional.
type ListSnapshotsOptions struct {
	Namespace  string
	BackupType string
	BackupID   string
}

// ListSnapshots lists the backup snapshots in a datastore (optionally scoped
// to one namespace and/or one backup group).
func (c *Client) ListSnapshots(ctx context.Context, store string, opts ListSnapshotsOptions) ([]Snapshot, error) {
	q := url.Values{}
	if opts.Namespace != "" {
		q.Set("ns", opts.Namespace)
	}
	if opts.BackupType != "" {
		q.Set("backup-type", opts.BackupType)
	}
	if opts.BackupID != "" {
		q.Set("backup-id", opts.BackupID)
	}
	var out struct {
		Data []Snapshot `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/admin/datastore/%s/snapshots", PathEscape(store)), q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// SetSnapshotProtected toggles a snapshot's protected flag (excludes it from
// prune) — PUT /admin/datastore/{store}/protected.
func (c *Client) SetSnapshotProtected(ctx context.Context, store string, opts ListSnapshotsOptions, backupTime int64, protected bool) error {
	form := url.Values{
		"backup-type": {opts.BackupType},
		"backup-id":   {opts.BackupID},
		"backup-time": {strconv.FormatInt(backupTime, 10)},
		"protected":   {boolStr(protected)},
	}
	if opts.Namespace != "" {
		form.Set("ns", opts.Namespace)
	}
	return c.put(ctx, fmt.Sprintf("/admin/datastore/%s/protected", PathEscape(store)), form, nil)
}

// PruneOptions configures a retention run against one backup group.
// Zero-value fields are left unset — PBS treats an absent keep-* option as
// "unlimited" for that bucket, same as leaving it blank in the UI.
type PruneOptions struct {
	Namespace  string
	BackupType string
	BackupID   string

	KeepLast    int
	KeepHourly  int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
	KeepYearly  int

	// DryRun asks PBS to report which snapshots it would remove without
	// actually deleting anything.
	DryRun bool
}

// PruneResult is one row of a prune run's report — the snapshot considered
// and whether it was (or would be) kept or removed.
type PruneResult struct {
	BackupTime int64 `json:"backup-time"`
	Keep       bool  `json:"keep"`
}

// Prune applies a retention policy to one backup group, deleting (or, with
// DryRun, only reporting) snapshots outside the keep-* windows.
func (c *Client) Prune(ctx context.Context, store string, opts PruneOptions) ([]PruneResult, error) {
	form := url.Values{
		"backup-type": {opts.BackupType},
		"backup-id":   {opts.BackupID},
	}
	if opts.Namespace != "" {
		form.Set("ns", opts.Namespace)
	}
	setIfPositive(form, "keep-last", opts.KeepLast)
	setIfPositive(form, "keep-hourly", opts.KeepHourly)
	setIfPositive(form, "keep-daily", opts.KeepDaily)
	setIfPositive(form, "keep-weekly", opts.KeepWeekly)
	setIfPositive(form, "keep-monthly", opts.KeepMonthly)
	setIfPositive(form, "keep-yearly", opts.KeepYearly)
	if opts.DryRun {
		form.Set("dry-run", "1")
	}
	var out struct {
		Data []PruneResult `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/admin/datastore/%s/prune", PathEscape(store)), form, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// StartGC starts a garbage-collection run on a datastore and returns its
// UPID (task ID) for status polling via the node task endpoints exposed
// through TaskStatus/TaskLog below.
func (c *Client) StartGC(ctx context.Context, store string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/admin/datastore/%s/gc", PathEscape(store)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// GCStatusFor returns a datastore's current/last garbage-collection status.
func (c *Client) GCStatusFor(ctx context.Context, store string) (*GCStatus, error) {
	var out struct {
		Data GCStatus `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/admin/datastore/%s/gc", PathEscape(store)), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

func setIfPositive(form url.Values, key string, value int) {
	if value > 0 {
		form.Set(key, strconv.Itoa(value))
	}
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

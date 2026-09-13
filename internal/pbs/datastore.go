package pbs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// GCStatus is a datastore's garbage-collection outcome (PBS type
// GarbageCollectionStatus): the gc-status object of
// GET /admin/datastore/{store}/status, also returned (flattened, plus
// schedule/next-run fields) by GET /admin/datastore/{store}/gc.
type GCStatus struct {
	UPID           string `json:"upid,omitempty"`
	IndexFileCount int64  `json:"index-file-count,omitempty"`
	IndexDataBytes int64  `json:"index-data-bytes,omitempty"`
	DiskBytes      int64  `json:"disk-bytes,omitempty"`
	DiskChunks     int64  `json:"disk-chunks,omitempty"`
	RemovedBytes   int64  `json:"removed-bytes,omitempty"`
	RemovedChunks  int64  `json:"removed-chunks,omitempty"`
	PendingBytes   int64  `json:"pending-bytes,omitempty"`
	PendingChunks  int64  `json:"pending-chunks,omitempty"`
	RemovedBad     int64  `json:"removed-bad,omitempty"`
	StillBad       int64  `json:"still-bad,omitempty"`
}

// Counts is a datastore's group/snapshot tally per backup type (PBS type
// Counts — filled by the status endpoint alongside gc-status).
type Counts struct {
	CT    *TypeCounts `json:"ct,omitempty"`
	Host  *TypeCounts `json:"host,omitempty"`
	VM    *TypeCounts `json:"vm,omitempty"`
	Other *TypeCounts `json:"other,omitempty"`
}

// TypeCounts is one backup type's group/snapshot tally (PBS type TypeCounts).
type TypeCounts struct {
	Groups    int64 `json:"groups"`
	Snapshots int64 `json:"snapshots"`
}

// Datastore is one row of GET /admin/datastore (PBS type DataStoreListItem:
// store, comment, maintenance) plus — only when filled in by
// DatastoreStatus/ListDatastoresWithUsage — the live usage from the per-store
// status endpoint, which is where PBS actually reports capacity.
type Datastore struct {
	Store       string `json:"store"`
	Comment     string `json:"comment,omitempty"`
	Maintenance string `json:"maintenance,omitempty"`

	// Usage fields — never populated by ListDatastores itself.
	Total    int64     `json:"total,omitempty"`
	Used     int64     `json:"used,omitempty"`
	Avail    int64     `json:"avail,omitempty"`
	GCStatus *GCStatus `json:"gc-status,omitempty"`
	Counts   *Counts   `json:"counts,omitempty"`

	// Error carries the per-store failure when usage couldn't be fetched for
	// this one datastore (ListDatastoresWithUsage attaches it rather than
	// failing the whole listing).
	Error string `json:"error,omitempty"`
}

// ListDatastores returns every datastore configured on this PBS host
// (store/comment/maintenance only — the list endpoint carries no usage
// figures). The signature is load-bearing: poller/digest probe it opaquely
// as a cheap authenticated call, so it must stay list-only.
func (c *Client) ListDatastores(ctx context.Context) ([]Datastore, error) {
	var out struct {
		Data []Datastore `json:"data"`
	}
	if err := c.get(ctx, "/admin/datastore", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// DatastoreStatus is the payload of GET /admin/datastore/{store}/status
// (PBS type DataStoreStatus) — live capacity plus the last GC outcome and
// per-type group/snapshot counts.
type DatastoreStatus struct {
	Total    int64     `json:"total"`
	Used     int64     `json:"used"`
	Avail    int64     `json:"avail"`
	GCStatus *GCStatus `json:"gc-status,omitempty"`
	Counts   *Counts   `json:"counts,omitempty"`
}

// DatastoreStatus fetches one datastore's live usage from
// /admin/datastore/{store}/status — the endpoint that actually carries
// total/used/avail (GET /admin/datastore returns only store, comment, and
// the maintenance flag).
func (c *Client) DatastoreStatus(ctx context.Context, store string) (*DatastoreStatus, error) {
	var out struct {
		Data DatastoreStatus `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/admin/datastore/%s/status", PathEscape(store)), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// datastoreStatusConcurrency bounds the per-store status fan-out in
// ListDatastoresWithUsage — a handful of concurrent upstream calls, not one
// goroutine (and TLS handshake) per configured datastore.
const datastoreStatusConcurrency = 4

// ListDatastoresWithUsage lists the datastores and attaches each one's live
// usage from /admin/datastore/{store}/status, fetched with a small bounded
// fan-out. A store whose status fetch fails keeps its listing row — the
// failure is attached to that store's Error field instead of failing the
// whole listing.
func (c *Client) ListDatastoresWithUsage(ctx context.Context) ([]Datastore, error) {
	stores, err := c.ListDatastores(ctx)
	if err != nil {
		return nil, err
	}
	sem := make(chan struct{}, datastoreStatusConcurrency)
	var wg sync.WaitGroup
	for i := range stores {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			status, err := c.DatastoreStatus(ctx, stores[i].Store)
			if err != nil {
				stores[i].Error = shortStatusError(err)
				return
			}
			stores[i].Total = status.Total
			stores[i].Used = status.Used
			stores[i].Avail = status.Avail
			stores[i].GCStatus = status.GCStatus
			stores[i].Counts = status.Counts
		}(i)
	}
	wg.Wait()
	return stores, nil
}

// shortStatusError renders a per-store failure as a compact message for the
// listing's Error field — a raw StatusError string would embed the whole
// upstream body (possibly a proxy error page) into a JSON row the UI shows.
func shortStatusError(err error) string {
	var se *StatusError
	if errors.As(err, &se) {
		return fmt.Sprintf("status %d: %s", se.StatusCode, se.Message())
	}
	return err.Error()
}

// Namespace is one entry under a datastore's namespace tree
// (/admin/datastore/{store}/namespace).
type Namespace struct {
	Namespace string `json:"ns"`
	Comment   string `json:"comment,omitempty"`
}

// ListNamespaces lists the namespaces under a datastore. parent scopes the
// listing to a sub-tree ("" lists from the root). On a PBS too old to know
// about namespaces (pre-2.2 — the endpoint answers 501 or rejects the
// request with an "unknown parameter"/"no such method" complaint) this
// degrades to an empty listing instead of an error: such a server has
// exactly one (implicit root) namespace, and failing the whole datastore
// page over it would be wrong. Real failures still surface.
func (c *Client) ListNamespaces(ctx context.Context, store, parent string) ([]Namespace, error) {
	q := url.Values{}
	if parent != "" {
		q.Set("parent", parent)
	}
	var out struct {
		Data []Namespace `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/admin/datastore/%s/namespace", PathEscape(store)), q, &out); err != nil {
		if IsNotAvailable(err) {
			return nil, nil
		}
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

// ListGroups lists the backup groups in a datastore namespace. On a PBS too
// old to know the ns parameter (pre-2.2), an explicitly-requested namespace
// degrades to an empty listing — showing the caller unfiltered root-ns
// groups instead would misrepresent where those backups live.
func (c *Client) ListGroups(ctx context.Context, store, namespace string) ([]BackupGroup, error) {
	q := url.Values{}
	if namespace != "" {
		q.Set("ns", namespace)
	}
	var out struct {
		Data []BackupGroup `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/admin/datastore/%s/groups", PathEscape(store)), q, &out); err != nil {
		if namespace != "" && IsNotAvailable(err) {
			return nil, nil
		}
		return nil, err
	}
	return out.Data, nil
}

// SnapshotVerification is a snapshot's last verify outcome (PBS type
// SnapshotVerifyState: upid + state, where state is "ok" or "failed").
// Decoding is tolerant: extra fields PBS may add are ignored, and a state
// we don't recognize still comes through as its raw string.
type SnapshotVerification struct {
	UPID  string `json:"upid,omitempty"`
	State string `json:"state,omitempty"`
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
	// when one has run ({"upid":"UPID:...","state":"ok"|"failed"}).
	Verification *SnapshotVerification `json:"verification,omitempty"`
}

// ListSnapshotsOptions filters a snapshot listing. All fields optional.
type ListSnapshotsOptions struct {
	Namespace  string
	BackupType string
	BackupID   string
}

// ListSnapshots lists the backup snapshots in a datastore (optionally scoped
// to one namespace and/or one backup group). Like ListGroups, an explicitly
// requested namespace on a pre-2.2 PBS (which rejects the ns parameter)
// degrades to an empty listing rather than unfiltered results.
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
		if opts.Namespace != "" && IsNotAvailable(err) {
			return nil, nil
		}
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

// pruneTimeout caps a synchronous prune run. The POST only answers once PBS
// has walked every snapshot in the group — minutes on a big one — so it
// can't live under the default 30s whole-request timeout, but it still gets
// a ceiling so a wedged upstream can't hang a request forever.
const pruneTimeout = 10 * time.Minute

// Prune applies a retention policy to one backup group, deleting (or, with
// DryRun, only reporting) snapshots outside the keep-* windows. The POST is
// synchronous — PBS reports the keep/remove decision per snapshot only after
// finishing the walk — so it runs on the longClient under its own
// 10-minute deadline instead of the default 30s request timeout.
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
	ctx, cancel := context.WithTimeout(ctx, pruneTimeout)
	defer cancel()
	var out struct {
		Data []PruneResult `json:"data"`
	}
	if err := c.postLong(ctx, fmt.Sprintf("/admin/datastore/%s/prune", PathEscape(store)), form, &out); err != nil {
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

// GCStatusFor returns a datastore's current/last garbage-collection status
// (GET /admin/datastore/{store}/gc — the same GarbageCollectionStatus fields
// as the status endpoint's gc-status object, plus the job's schedule and
// last-run metadata, which are not carried in GCStatus).
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

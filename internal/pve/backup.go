package pve

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// BackupJob is one scheduled vzdump job (/cluster/backup).
type BackupJob struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule,omitempty"`
	Storage  string `json:"storage,omitempty"`
	VMID     string `json:"vmid,omitempty"` // comma-separated list, or "all"
	Enabled  int    `json:"enabled,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Compress string `json:"compress,omitempty"`
	Comment  string `json:"comment,omitempty"`

	NotificationMode string `json:"notification-mode,omitempty"`
	MailTo           string `json:"mailto,omitempty"`
	MailNotification string `json:"mailnotification,omitempty"`
	BWLimit          int    `json:"bwlimit,omitempty"`
	Pigz             int    `json:"pigz,omitempty"`
}

func (c *Client) BackupJobs(ctx context.Context) ([]BackupJob, error) {
	var out struct {
		Data []BackupJob `json:"data"`
	}
	if err := c.get(ctx, "/cluster/backup", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// RunBackupNow triggers an immediate vzdump for one or more guests on a node.
func (c *Client) RunBackupNow(ctx context.Context, node, storage string, vmids []string, mode string) (string, error) {
	form := url.Values{"storage": {storage}}
	if len(vmids) > 0 {
		vmidCSV := ""
		for i, v := range vmids {
			if i > 0 {
				vmidCSV += ","
			}
			vmidCSV += v
		}
		form.Set("vmid", vmidCSV)
	}
	if mode != "" {
		form.Set("mode", mode)
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/vzdump", PathEscape(node)), form, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// CreateBackupJobOptions models the writable fields of a scheduled vzdump
// job (/cluster/backup). Schedule uses Proxmox's calendar-event syntax
// (e.g. "sat 02:00" or "*-*-* 02:00:00").
type CreateBackupJobOptions struct {
	Schedule string
	Storage  string
	VMIDs    string // comma-separated, or "" for "all guests"
	Mode     string // "snapshot" | "suspend" | "stop"
	Compress string // "0" | "lzo" | "gzip" | "zstd"
	Enabled  bool
	Comment  string
	Prune    int // keep-last count, 0 = unset

	NotificationMode string // "notification-system" | "legacy-sendmail", "" = unset
	MailTo           string // comma-separated emails, only used with NotificationMode "legacy-sendmail"
	MailNotification string // "always" | "failure" (legacy option, form key stays "mailnotification")
	BandwidthLimitKBps int  // vzdump --bwlimit in KiB/s, 0 = unset
	Pigz             *int  // parallel gzip threads; nil = unset (0 is itself a valid "disabled" value)
}

func backupJobForm(opts CreateBackupJobOptions) url.Values {
	form := url.Values{}
	if opts.Schedule != "" {
		form.Set("schedule", opts.Schedule)
	}
	if opts.Storage != "" {
		form.Set("storage", opts.Storage)
	}
	if opts.VMIDs != "" {
		form.Set("vmid", opts.VMIDs)
	} else {
		form.Set("all", "1")
	}
	if opts.Mode != "" {
		form.Set("mode", opts.Mode)
	}
	if opts.Compress != "" {
		form.Set("compress", opts.Compress)
	}
	if opts.Comment != "" {
		form.Set("comment", opts.Comment)
	}
	if opts.Prune > 0 {
		form.Set("prune-backups", fmt.Sprintf("keep-last=%d", opts.Prune))
	}
	if opts.NotificationMode != "" {
		form.Set("notification-mode", opts.NotificationMode)
	}
	if opts.MailTo != "" {
		form.Set("mailto", opts.MailTo)
	}
	if opts.MailNotification != "" {
		form.Set("mailnotification", opts.MailNotification)
	}
	if opts.BandwidthLimitKBps > 0 {
		form.Set("bwlimit", strconv.Itoa(opts.BandwidthLimitKBps))
	}
	if opts.Pigz != nil {
		form.Set("pigz", strconv.Itoa(*opts.Pigz))
	}
	form.Set("enabled", boolFlag(opts.Enabled))
	return form
}

func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// CreateBackupJob schedules a new cluster-wide vzdump job.
func (c *Client) CreateBackupJob(ctx context.Context, opts CreateBackupJobOptions) error {
	return c.post(ctx, "/cluster/backup", backupJobForm(opts), nil)
}

// UpdateBackupJob edits an existing scheduled job by its id (e.g. "backup-abc123").
func (c *Client) UpdateBackupJob(ctx context.Context, id string, opts CreateBackupJobOptions) error {
	return c.put(ctx, "/cluster/backup/"+url.PathEscape(id), backupJobForm(opts), nil)
}

// DeleteBackupJob removes a scheduled vzdump job.
func (c *Client) DeleteBackupJob(ctx context.Context, id string) error {
	return c.delete(ctx, "/cluster/backup/"+url.PathEscape(id), nil, nil)
}

// GuestBackups lists backup archives for one guest by inspecting the
// "backup" content of every configured storage on a node.
func (c *Client) GuestBackups(ctx context.Context, node, storage string, vmid int) ([]StorageContentItem, error) {
	items, err := c.StorageContent(ctx, node, storage)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, item := range items {
		if item.Content == "backup" && (vmid == 0 || item.VMID == vmid) {
			out = append(out, item)
		}
	}
	return out, nil
}

// SetBackupProtected marks (or clears) one backup archive as protected,
// which exempts it from prune-backups deletion until cleared.
func (c *Client) SetBackupProtected(ctx context.Context, node, storage, volid string, protected bool) error {
	return c.UpdateStorageContent(ctx, node, storage, volid, url.Values{"protected": {boolFlag(protected)}})
}

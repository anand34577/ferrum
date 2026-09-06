package pve

import (
	"context"
	"fmt"
	"net/url"
)

// ReplicationJob is one PVE storage replication (pvesr) job.
type ReplicationJob struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Source   string `json:"source,omitempty"`
	Target   string `json:"target"`
	Schedule string `json:"schedule,omitempty"`
	Guest    int    `json:"guest,omitempty"`
	Disable  int    `json:"disable,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

func (c *Client) ReplicationJobs(ctx context.Context) ([]ReplicationJob, error) {
	var out struct {
		Data []ReplicationJob `json:"data"`
	}
	if err := c.get(ctx, "/cluster/replication", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ReplicationStatus is one node's view of a replication job's last run.
type ReplicationStatus struct {
	ID        string  `json:"id"`
	LastSync  int64   `json:"last_sync,omitempty"`
	NextSync  int64   `json:"next_sync,omitempty"`
	Duration  float64 `json:"duration,omitempty"`
	Error     string  `json:"error,omitempty"`
	FailCount int     `json:"fail_count,omitempty"`
}

func (c *Client) NodeReplicationStatus(ctx context.Context, node string) ([]ReplicationStatus, error) {
	var out struct {
		Data []ReplicationStatus `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/replication", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) ScheduleReplicationNow(ctx context.Context, node, id string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/replication/%s/schedule_now", PathEscape(node), url.PathEscape(id)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// CreateReplicationJobOptions models the writable fields of a replication
// job (/cluster/replication). ID is "<vmid>-<jobnum>" (e.g. "100-0") —
// PVE's own naming scheme, chosen by the caller since there's no
// auto-numbering endpoint.
type CreateReplicationJobOptions struct {
	ID       string
	Guest    int
	Target   string // target node name
	Schedule string // e.g. "*/15" (every 15 min); PVE default is every 15 minutes if omitted
	Comment  string
	Disable  bool
}

func replicationJobForm(opts CreateReplicationJobOptions) url.Values {
	form := url.Values{"type": {"local"}}
	if opts.Target != "" {
		form.Set("target", opts.Target)
	}
	if opts.Schedule != "" {
		form.Set("schedule", opts.Schedule)
	}
	if opts.Comment != "" {
		form.Set("comment", opts.Comment)
	}
	form.Set("disable", boolFlag(opts.Disable))
	return form
}

// CreateReplicationJob schedules a new storage replication job for a guest.
func (c *Client) CreateReplicationJob(ctx context.Context, opts CreateReplicationJobOptions) error {
	form := replicationJobForm(opts)
	form.Set("id", opts.ID)
	if opts.Guest > 0 {
		form.Set("guest", fmt.Sprintf("%d", opts.Guest))
	}
	return c.post(ctx, "/cluster/replication", form, nil)
}

// UpdateReplicationJob edits an existing replication job's schedule/target/comment/enabled state.
func (c *Client) UpdateReplicationJob(ctx context.Context, id string, opts CreateReplicationJobOptions) error {
	return c.put(ctx, "/cluster/replication/"+url.PathEscape(id), replicationJobForm(opts), nil)
}

// DeleteReplicationJob removes a replication job. When force is true, PVE
// also removes the already-replicated disk images from the target node
// instead of leaving them behind.
func (c *Client) DeleteReplicationJob(ctx context.Context, id string, force bool) error {
	form := url.Values{}
	if force {
		form.Set("force", "1")
	}
	return c.delete(ctx, "/cluster/replication/"+url.PathEscape(id), form, nil)
}

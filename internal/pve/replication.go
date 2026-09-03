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

package pbs

import (
	"context"
	"fmt"
	"net/url"
)

// SyncJob is one configured sync job (GET /config/sync) — pulls backups from
// a remote PBS/PVE source into a local datastore on a schedule.
type SyncJob struct {
	ID             string `json:"id"`
	Store          string `json:"store"`
	Namespace      string `json:"ns,omitempty"`
	Remote         string `json:"remote,omitempty"`
	RemoteStore    string `json:"remote-store"`
	RemoteNS       string `json:"remote-ns,omitempty"`
	Schedule       string `json:"schedule,omitempty"`
	Comment        string `json:"comment,omitempty"`
	RemoveVanished bool   `json:"remove-vanished,omitempty"`
}

// ListSyncJobs returns every configured sync job.
func (c *Client) ListSyncJobs(ctx context.Context) ([]SyncJob, error) {
	var out struct {
		Data []SyncJob `json:"data"`
	}
	if err := c.get(ctx, "/config/sync", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// SyncJob fetches one configured sync job by id.
func (c *Client) GetSyncJob(ctx context.Context, id string) (*SyncJob, error) {
	var out struct {
		Data SyncJob `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/config/sync/%s", PathEscape(id)), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// RunSyncJob triggers a configured sync job immediately and returns its UPID.
func (c *Client) RunSyncJob(ctx context.Context, id string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/admin/sync/%s/run", PathEscape(id)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// VerifyJob is one configured verification job (GET /config/verify) — checks
// backup snapshot integrity in a datastore on a schedule.
type VerifyJob struct {
	ID             string `json:"id"`
	Store          string `json:"store"`
	Namespace      string `json:"ns,omitempty"`
	Schedule       string `json:"schedule,omitempty"`
	Comment        string `json:"comment,omitempty"`
	IgnoreVerified bool   `json:"ignore-verified,omitempty"`
	OutdatedAfter  int    `json:"outdated-after,omitempty"`
}

// ListVerifyJobs returns every configured verification job.
func (c *Client) ListVerifyJobs(ctx context.Context) ([]VerifyJob, error) {
	var out struct {
		Data []VerifyJob `json:"data"`
	}
	if err := c.get(ctx, "/config/verify", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// GetVerifyJob fetches one configured verification job by id.
func (c *Client) GetVerifyJob(ctx context.Context, id string) (*VerifyJob, error) {
	var out struct {
		Data VerifyJob `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/config/verify/%s", PathEscape(id)), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// RunVerifyJob triggers a configured verification job immediately and
// returns its UPID.
func (c *Client) RunVerifyJob(ctx context.Context, id string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/admin/verify/%s/run", PathEscape(id)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

// Task is a PBS background task's status — GET /nodes/localhost/tasks/{upid}/status,
// used to poll the UPID returned by StartGC/Prune-with-tasks/RunSyncJob/RunVerifyJob.
// PBS tasks always run on the "localhost" pseudo-node from the client's point
// of view (there is no multi-node concept like PVE clustering).
type Task struct {
	UPID       string `json:"upid"`
	Status     string `json:"status"`               // "running" | "stopped"
	ExitStatus string `json:"exitstatus,omitempty"` // "OK" | failure detail, once stopped
}

// TaskStatus polls one background task by UPID.
func (c *Client) TaskStatus(ctx context.Context, upid string) (*Task, error) {
	var out struct {
		Data Task `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/localhost/tasks/%s/status", PathEscape(upid)), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// TaskLog returns a task's log lines, one entry per line, in order.
func (c *Client) TaskLog(ctx context.Context, upid string) ([]string, error) {
	var out struct {
		Data []struct {
			N int    `json:"n"`
			T string `json:"t"`
		} `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/localhost/tasks/%s/log", PathEscape(upid)), nil, &out); err != nil {
		return nil, err
	}
	lines := make([]string, len(out.Data))
	for i, l := range out.Data {
		lines[i] = l.T
	}
	return lines, nil
}

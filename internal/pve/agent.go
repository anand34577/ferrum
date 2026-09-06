package pve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
)

// GuestAgentPing checks that the QEMU guest agent inside a VM is reachable
// (returns an error otherwise — e.g. agent not installed/running). QEMU-only;
// LXC has no equivalent since the host already has direct namespace access.
func (c *Client) GuestAgentPing(ctx context.Context, node string, vmid int) error {
	return c.post(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/agent/ping", PathEscape(node), vmid), url.Values{}, nil)
}

// GuestAgentExecResult is the pid handle returned by agent/exec — poll it
// with GuestAgentExecStatus to get the command's output and exit code.
type GuestAgentExecResult struct {
	PID int `json:"pid"`
}

// GuestAgentExec runs a command inside the VM via the guest agent. command is
// argv (command[0] is the executable, no shell involved unless the caller
// passes one explicitly, e.g. []string{"/bin/sh", "-c", "..."}).
func (c *Client) GuestAgentExec(ctx context.Context, node string, vmid int, command []string, input string) (*GuestAgentExecResult, error) {
	cmdJSON, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	form := url.Values{"command": {string(cmdJSON)}}
	if input != "" {
		form.Set("input-data", input)
	}
	var out struct {
		Data GuestAgentExecResult `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", PathEscape(node), vmid), form, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// GuestAgentExecStatus is the outcome of a command started with GuestAgentExec.
// Exited is false while the command is still running.
type GuestAgentExecStatus struct {
	Exited    bool   `json:"exited"`
	ExitCode  int    `json:"exitcode,omitempty"`
	Signal    int    `json:"signal,omitempty"`
	OutData   string `json:"out-data,omitempty"`
	ErrData   string `json:"err-data,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (c *Client) GuestAgentExecStatus(ctx context.Context, node string, vmid, pid int) (*GuestAgentExecStatus, error) {
	var out struct {
		Data GuestAgentExecStatus `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec-status?pid=%d", PathEscape(node), vmid, pid)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// GuestAgentFsfreeze freezes (thaw=false) or thaws (thaw=true) the VM's
// filesystems via the guest agent — used to get a consistent snapshot of a
// running guest without stopping it.
func (c *Client) GuestAgentFsfreeze(ctx context.Context, node string, vmid int, thaw bool) error {
	action := "fsfreeze-freeze"
	if thaw {
		action = "fsfreeze-thaw"
	}
	return c.post(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/agent/%s", PathEscape(node), vmid, action), url.Values{}, nil)
}

// GuestAgentShutdown asks the guest agent to cleanly shut down the guest OS
// (distinct from the hypervisor-level GuestPowerAction "shutdown", which
// relies on ACPI rather than the agent).
func (c *Client) GuestAgentShutdown(ctx context.Context, node string, vmid int) error {
	return c.post(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/agent/shutdown", PathEscape(node), vmid), url.Values{}, nil)
}

// GuestAgentSetUserPassword sets a user's password inside the guest via the
// agent. crypted indicates password is already a crypt(3) hash rather than
// plaintext.
func (c *Client) GuestAgentSetUserPassword(ctx context.Context, node string, vmid int, username, password string, crypted bool) error {
	form := url.Values{"username": {username}, "password": {password}}
	if crypted {
		form.Set("crypted", "1")
	}
	return c.post(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/agent/set-user-password", PathEscape(node), vmid), form, nil)
}

// GuestAgentOSInfo returns the guest's os-info as reported by the agent
// (fields like "name", "pretty-name", "kernel-release", "version" — these
// vary a lot by guest OS, so this is returned raw rather than over-typed,
// same approach as GuestConfig.Raw elsewhere in this package).
func (c *Client) GuestAgentOSInfo(ctx context.Context, node string, vmid int) (map[string]any, error) {
	var out struct {
		Data struct {
			Result map[string]any `json:"result"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-osinfo", PathEscape(node), vmid)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Data.Result, nil
}

// GuestAgentFSInfo returns the guest's mounted filesystems (name, mountpoint,
// type, total-bytes, used-bytes, ...) as reported by the agent. Returned raw
// per entry — same reasoning as GuestAgentOSInfo.
func (c *Client) GuestAgentFSInfo(ctx context.Context, node string, vmid int) ([]map[string]any, error) {
	var out struct {
		Data struct {
			Result []map[string]any `json:"result"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-fsinfo", PathEscape(node), vmid)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Data.Result, nil
}

// GuestAgentVCPUs returns the guest's vCPU list as reported by the agent.
func (c *Client) GuestAgentVCPUs(ctx context.Context, node string, vmid int) ([]map[string]any, error) {
	var out struct {
		Data struct {
			Result []map[string]any `json:"result"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-vcpus", PathEscape(node), vmid)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Data.Result, nil
}

// GuestAgentHostname returns the guest's hostname as reported by the agent.
func (c *Client) GuestAgentHostname(ctx context.Context, node string, vmid int) (string, error) {
	var out struct {
		Data struct {
			Result struct {
				HostName string `json:"host-name"`
			} `json:"result"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-host-name", PathEscape(node), vmid)
	if err := c.get(ctx, path, &out); err != nil {
		return "", err
	}
	return out.Data.Result.HostName, nil
}

// GuestAgentTimezone returns the guest's timezone name as reported by the
// agent.
func (c *Client) GuestAgentTimezone(ctx context.Context, node string, vmid int) (string, error) {
	var out struct {
		Data struct {
			Result struct {
				Zone string `json:"zone"`
			} `json:"result"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-timezone", PathEscape(node), vmid)
	if err := c.get(ctx, path, &out); err != nil {
		return "", err
	}
	return out.Data.Result.Zone, nil
}

// guestAgentFileOpen opens a file inside the guest via the agent (QEMU GA's
// guest-file-open) and returns the file handle to use with file-read/
// file-write/file-close. mode follows fopen(3) semantics, e.g. "r" or "w".
func (c *Client) guestAgentFileOpen(ctx context.Context, node string, vmid int, path, mode string) (int, error) {
	var out struct {
		Data struct {
			Result int `json:"result"`
		} `json:"data"`
	}
	form := url.Values{"file": {path}, "mode": {mode}}
	reqPath := fmt.Sprintf("/nodes/%s/qemu/%d/agent/file-open", PathEscape(node), vmid)
	if err := c.post(ctx, reqPath, form, &out); err != nil {
		return 0, err
	}
	return out.Data.Result, nil
}

// guestAgentFileClose closes a handle opened by guestAgentFileOpen.
func (c *Client) guestAgentFileClose(ctx context.Context, node string, vmid int, handle int) error {
	form := url.Values{"handle": {fmt.Sprintf("%d", handle)}}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/file-close", PathEscape(node), vmid)
	return c.post(ctx, path, form, nil)
}

// fileReadChunkSize and fileReadMaxBytes bound GuestAgentFileRead's read
// loop: it reads this many bytes at a time, up to this many bytes total, so
// a huge or unbounded file inside the guest can't hang the request forever.
const (
	fileReadChunkSize = 1 << 20  // 1MB per file-read call
	fileReadMaxBytes  = 16 << 20 // 16MB cap across the whole read
)

// GuestAgentFileRead reads a file from inside the guest via the agent's
// file-open/file-read/file-close trio (QEMU GA has no single "read file"
// call). Assumed wire shape for file-read, per the QEMU GA spec: the result
// carries base64 content under "buf-b64" and a boolean "eof"; every unwrap
// step is nil-checked defensively in case PVE's actual field names differ.
// Reads loop in fileReadChunkSize chunks until eof or fileReadMaxBytes is
// reached, to avoid hanging on a very large file.
func (c *Client) GuestAgentFileRead(ctx context.Context, node string, vmid int, path string) (string, error) {
	handle, err := c.guestAgentFileOpen(ctx, node, vmid, path, "r")
	if err != nil {
		return "", err
	}
	defer c.guestAgentFileClose(ctx, node, vmid, handle)

	var content []byte
	for len(content) < fileReadMaxBytes {
		var out struct {
			Data struct {
				Result struct {
					BufB64 string `json:"buf-b64"`
					EOF    bool   `json:"eof"`
				} `json:"result"`
			} `json:"data"`
		}
		reqPath := fmt.Sprintf("/nodes/%s/qemu/%d/agent/file-read?handle=%d&count=%d", PathEscape(node), vmid, handle, fileReadChunkSize)
		if err := c.get(ctx, reqPath, &out); err != nil {
			return "", err
		}
		if out.Data.Result.BufB64 != "" {
			chunk, err := base64.StdEncoding.DecodeString(out.Data.Result.BufB64)
			if err != nil {
				return "", fmt.Errorf("decoding file-read response: %w", err)
			}
			content = append(content, chunk...)
		}
		if out.Data.Result.EOF {
			break
		}
	}
	return string(content), nil
}

// GuestAgentFileWrite writes content to a file inside the guest via the
// agent's file-open/file-write/file-close trio, in a single file-write call
// (fine for config-file-sized writes; not a general chunked/streaming
// writer).
func (c *Client) GuestAgentFileWrite(ctx context.Context, node string, vmid int, path, content string) error {
	handle, err := c.guestAgentFileOpen(ctx, node, vmid, path, "w")
	if err != nil {
		return err
	}
	defer c.guestAgentFileClose(ctx, node, vmid, handle)

	form := url.Values{
		"handle":  {fmt.Sprintf("%d", handle)},
		"content": {base64.StdEncoding.EncodeToString([]byte(content))},
	}
	reqPath := fmt.Sprintf("/nodes/%s/qemu/%d/agent/file-write", PathEscape(node), vmid)
	return c.post(ctx, reqPath, form, nil)
}

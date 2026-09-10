// Package needle wraps Cactus Compute's Needle 2 (https://huggingface.co/Cactus-Compute/needle2)
// — a 45M-parameter, tool-calling-focused model that ships as a small
// self-contained CLI binary — as a zero-config, no-API-key "built-in" AI
// provider for Ferrum's AI Assistant and MCP tool-calling loop.
//
// Needle 2 is Apache-2.0, so its official CLI binary (bundled_*.go, one
// per platform, go:embed'd behind build tags) ships baked into the Ferrum
// binary itself for Windows/Linux/macOS on amd64 or arm64 — no download, no
// FERRUM_NEEDLE_BIN, no manual step; resolveBinPath extracts it to a cache
// file on first use. On any other platform (32-bit, RISC-V, Windows/ARM64,
// ...) Ferrum still never fetches executable content from the network on
// its own: an operator there must download the CLI binary themselves and
// point FERRUM_NEEDLE_BIN at it — see README "Built-in LLM (Needle 2)" for
// the exact steps. Available() reports false, and every call fails with a
// clear "not installed" error, until a binary (bundled or configured) is in
// place.
//
// Needle's own HTTP server (`needle --serve`) is NOT OpenAI-compatible: it
// takes a single `{"input": "..."}` string per request (no message history,
// no per-request tool list — tools are fixed for the process's lifetime via
// `--tools`) and returns a structured function-call/reasoning object, not a
// chat-completion. This package is the adapter between that shape and the
// OpenAI chat-completion shape the rest of Ferrum already speaks (see
// internal/api/ai_chat.go's callChatCompletion), so from the caller's point
// of view a Needle-backed provider behaves like any other.
package needle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BaseURL is the sentinel value stored in ai_providers.base_url to mean
// "route through the built-in Needle manager" instead of making a real HTTP
// call — chosen so the existing provider schema (name/baseURL/apiKey/model)
// needs no new column or migration to carry a fourth, local provider kind.
const BaseURL = "needle://local"

// IsBuiltin reports whether baseURL names the built-in Needle provider.
func IsBuiltin(baseURL string) bool { return baseURL == BaseURL }

// listenPort/listenAddr is where the Needle CLI's --serve mode listens.
// Needle defaults --serve to :8080, which collides with Ferrum's own
// default server.addr (also :8080, see config.example.yaml) — a Needle
// subprocess started after Ferrum would fail to bind and never become
// ready. `--port` (undocumented in the model card's README, but present in
// the actual CLI's --help) moves it out of the way; the chosen value just
// needs to avoid Ferrum's own port and other common local dev ports.
const listenPort = "58211"
const listenAddr = "127.0.0.1:" + listenPort

// startTimeout bounds how long ensureRunning waits for a freshly spawned
// process to answer its first request before giving up and reporting the
// binary as unusable this run. Generous because it isn't just a TCP-accept
// wait: Needle's "tool retrieval" (README) does a one-time embedding pass
// over every declared tool on the process's first request once the catalog
// exceeds 5 tools — true for Ferrum's real catalog, which is now around 60
// tools (see internal/mcp) — so cold start plus first-inference warmup can
// take a while even though the listener itself comes up almost immediately,
// especially on slower ARM SBC-class hardware. If it's dying outright rather
// than just running long, this won't help — see m.log's "needle process
// exited: signal: killed", which on Linux is almost always the kernel
// OOM-killer, not a timeout.
const startTimeout = 60 * time.Second

// Manager owns the lifecycle of at most one Needle CLI subprocess — "each
// component instance owns one conversation" per the model's own docs, and
// Ferrum's tool catalog (internal/mcp) is effectively static, so one
// long-lived process serving every caller is the right shape; a per-request
// or per-user process would only add startup latency for no real benefit.
type Manager struct {
	binPath string

	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
	log     syncBuffer // subprocess's combined stdout/stderr for its whole lifetime — see crashInfo.

	// reqMu serializes every /complete call against the subprocess — Needle
	// serves one request at a time ("each component instance owns one
	// conversation" per its own docs), and it is shared across every caller
	// (see the Manager doc comment above). Without this, two overlapping
	// chats — two users, or a client retry racing a still-in-flight request
	// — hit the same subprocess at once; observed in practice as the
	// subprocess crashing mid-response ("read: connection reset by peer")
	// rather than queuing or rejecting the second one.
	reqMu sync.Mutex
}

func NewManager(binPath string) *Manager {
	return &Manager{binPath: binPath}
}

// syncBuffer is bytes.Buffer plus a mutex, so it's safe as an exec.Cmd's
// Stdout/Stderr (written from the subprocess-reading goroutines the os/exec
// package spawns internally) while ensureRunning or crashInfo reads it via
// String() to build an error message — for the subprocess's entire
// lifetime, not just during startup.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}

// Available reports whether a usable binary exists — either the one an
// operator configured, or (see resolveBinPath) the one bundled for this
// platform — used to hide/disable the built-in provider in the UI and to
// fail fast with a clear message instead of an opaque connection error.
func (m *Manager) Available() bool {
	_, err := m.resolveBinPath()
	return err == nil
}

// resolveBinPath returns the binary path this Manager will actually run: an
// operator-configured FERRUM_NEEDLE_BIN wins outright (an explicit choice
// should never be silently overridden); otherwise, if this platform has one
// baked in via go:embed (see bundled_*.go — Windows/Linux/macOS on amd64 or
// arm64), it's written out to a cache file once and reused, so Ferrum works
// with zero manual download/config on the platforms it ships a binary for.
func (m *Manager) resolveBinPath() (string, error) {
	if m.binPath != "" {
		info, err := os.Stat(m.binPath)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("configured needle binary not found at %q", m.binPath)
		}
		return m.binPath, nil
	}
	if len(bundledBinary) == 0 {
		return "", fmt.Errorf("no needle binary bundled for this platform")
	}
	return extractBundled()
}

// extractBundled writes the embedded binary to a stable cache path once,
// skipping the write if a file of the exact same size is already there
// (cheap enough to check every call, avoids re-extracting ~15MB on every
// startup). Not content-hashed — ponytail: a corrupted cache file that
// happens to match the size would be reused as-is; delete the cache
// directory by hand if that's ever suspected, a byte-for-byte checksum isn't
// worth it for a file this binary only ever writes itself.
func extractBundled() (string, error) {
	dir := filepath.Join(os.TempDir(), "ferrum-needle-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("preparing needle cache dir: %w", err)
	}
	path := filepath.Join(dir, bundledName)
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(bundledBinary)) {
		return path, nil
	}
	if err := os.WriteFile(path, bundledBinary, 0o755); err != nil {
		return "", fmt.Errorf("extracting bundled needle binary: %w", err)
	}
	return path, nil
}

// Close stops the subprocess, if running. Safe to call even if it was never
// started. Called once at server shutdown (see cmd/ferrum/main.go).
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeLocked()
}

// closeLocked is Close's body, split out so ensureRunning (which already
// holds m.mu) can kill a process that failed to become ready without
// deadlocking on Close's own lock.
func (m *Manager) closeLocked() {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}
	m.running = false
	m.cmd = nil
}

// ensureRunning starts the Needle subprocess on first use and waits for it
// to accept requests. A dead/never-started process is (re)spawned; an
// already-healthy one is reused — see the mutex-guarded m.running flag.
//
// ponytail: no crash-loop/backoff supervision — if the process dies mid-run,
// the next call's health check fails, running is reset, and one fresh
// process is spawned. Add real supervision if Needle proves flaky in
// practice; nothing observed in its docs suggests it will be.
func (m *Manager) ensureRunning(ctx context.Context) error {
	binPath, err := m.resolveBinPath()
	if err != nil {
		return fmt.Errorf("%w — see README \"Built-in LLM (Needle 2)\" for install steps", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	toolsPath, err := writeToolsFile()
	if err != nil {
		return fmt.Errorf("writing needle tools file: %w", err)
	}

	cmd := exec.Command(binPath, "--tools", toolsPath, "--serve", "--port", listenPort)
	// Captured rather than discarded: without this, a subprocess that
	// starts and immediately exits (bad args, missing runtime dep, port
	// already taken from outside our own bookkeeping) fails completely
	// silently — ensureRunning just spins until startTimeout with no clue
	// why. Kept on the Manager (not a local var) so a crash well after
	// startup — see ChatCompletion's crashInfo check — can be explained too,
	// not just a startup failure; reset here for the new process.
	m.log = syncBuffer{}
	cmd.Stdout = &m.log
	cmd.Stderr = &m.log
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting needle: %w", err)
	}
	m.cmd = cmd
	m.running = true
	// Tracks the subprocess exiting on its own (crash, or killed externally)
	// so the next ensureRunning call notices and respawns instead of trying
	// to reuse a dead process — Signal(0)-style liveness checks aren't
	// reliably supported cross-platform (notably on Windows), but Wait() is.
	exited := make(chan struct{})
	go func() {
		waitErr := cmd.Wait()
		close(exited)
		m.mu.Lock()
		if m.cmd == cmd {
			m.running = false
			m.cmd = nil
		}
		m.mu.Unlock()
		if waitErr != nil {
			// Wait()'s error already renders signal deaths as e.g. "signal:
			// killed" on Unix (that's SIGKILL — on a memory-constrained host,
			// almost always the kernel OOM-killer) — appended to the log so
			// both the "exited immediately"/"did not become ready" messages
			// below and ChatCompletion's crashInfo (a later mid-request
			// death) explain *why* instead of leaving a bare connection
			// error as the only clue.
			m.log.Write([]byte("\n[needle process exited: " + waitErr.Error() + "]"))
		}
	}()

	// Two phases, deliberately not one poll loop of full requests: dialing a
	// bare TCP connection (no bytes sent) is cheap and safe to retry while
	// the listener is still coming up, but a real /complete round-trip is
	// not — Needle serves one request at a time, and a real request that
	// gets cancelled client-side keeps running server-side, so retrying
	// *those* on a short timeout just piles up work that never finishes
	// (see startTimeout's doc comment on why that first request is slow).
	// So: poll the bare socket until something answers, then send exactly
	// one real request and let it run.
	select {
	case <-waitForListener(ctx, listenAddr, startTimeout):
	case <-exited:
		return fmt.Errorf("needle exited immediately: %s", m.log.String())
	}

	pingCtx, cancel := context.WithTimeout(ctx, startTimeout)
	pingErr := make(chan error, 1)
	go func() {
		_, _, err := doComplete(pingCtx, "ping")
		pingErr <- err
	}()

	select {
	case err := <-pingErr:
		cancel()
		if err == nil {
			return nil
		}
		m.closeLocked()
		return fmt.Errorf("needle did not become ready within %s (%v): %s", startTimeout, err, m.log.String())
	case <-exited:
		cancel()
		// Crashed (or exited) before ever answering a request — no point
		// waiting out the rest of startTimeout. m.log carries whatever it
		// printed (its own error, a missing dependency, license/arch
		// mismatch, ...) so this isn't a bare "not ready".
		return fmt.Errorf("needle exited immediately: %s", m.log.String())
	}
}

// waitForListener returns a channel that closes as soon as addr accepts a
// bare TCP connection, or when timeout elapses (whichever first) — the
// caller distinguishes the two via the connection's own read/request
// afterward, so this never needs to report which happened. Only a raw dial
// is retried here; see ensureRunning for why a real request isn't.
func waitForListener(ctx context.Context, addr string, timeout time.Duration) <-chan struct{} {
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			d := net.Dialer{Timeout: 200 * time.Millisecond}
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err == nil {
				conn.Close()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	return ready
}

// --- request/response translation ---

// needleTool mirrors the JSON schema Needle's README documents for --tools:
// {"name", "description", "parameters"} — the same shape as the inner
// "function" object of an OpenAI tool definition, so converting Ferrum's
// existing tool catalog is a direct field copy.
type needleTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// writeToolsFile renders Ferrum's MCP tool catalog into the JSON file the
// Needle CLI expects via --tools, in the OS temp dir. The catalog is static
// for the process lifetime (see the Manager doc comment), so this only
// needs to happen once per subprocess start, not per request.
func writeToolsFile() (string, error) {
	tools := toolCatalog()
	// FERRUM_NEEDLE_MAX_TOOLS is an escape hatch for memory-constrained
	// hosts (small ARM SBCs in particular): Needle's one-time "tool
	// retrieval" embedding pass runs over the entire catalog on its first
	// request, and Ferrum's real catalog (internal/mcp) has grown to around
	// 60 tools — enough to OOM-kill the subprocess on a device with very
	// little RAM (see m.log's "signal: killed" in that case). The read-only
	// lookup tools are declared first in internal/mcp/tools.go, so
	// truncating here keeps the most broadly useful ones (list_nodes,
	// list_guests, cluster_status, ...) and drops the newer, more numerous
	// mutation tools first. Unset (the default) sends the full catalog,
	// unchanged from before this existed.
	if n := maxNeedleTools(); n > 0 && n < len(tools) {
		tools = tools[:n]
	}
	out := make([]needleTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, needleTool{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), "ferrum-needle-tools.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// maxNeedleTools reads FERRUM_NEEDLE_MAX_TOOLS — see writeToolsFile. Any
// unset/empty/non-positive/unparseable value means "no limit" (0), the
// pre-existing behavior.
func maxNeedleTools() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("FERRUM_NEEDLE_MAX_TOOLS")))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// toolCatalogFunc is overridden by tests (and set by internal/api at init)
// to avoid an import cycle with internal/mcp — this package only needs the
// name/description/schema triple, not mcp's handler wiring.
var toolCatalogFunc func() []ToolDef

// ToolDef is the subset of mcp.Tool this package needs.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// SetToolCatalog is called once at startup (internal/api/server.go) with
// mcp.ToolDefinitions adapted to ToolDef, so this package can render
// --tools.json without importing internal/mcp directly.
func SetToolCatalog(f func() []ToolDef) { toolCatalogFunc = f }

func toolCatalog() []ToolDef {
	if toolCatalogFunc == nil {
		return nil
	}
	return toolCatalogFunc()
}

type needleRequest struct {
	Input string `json:"input"`
}

// needleResponse is the shape documented in Needle's README for a completed
// /complete call — see the package doc comment. Fields beyond FunctionCalls/
// Reasoning/Error are accepted but unused.
type needleResponse struct {
	Type          string           `json:"type"`
	Success       *bool            `json:"success"`
	FunctionCalls []needleFuncCall `json:"function_calls"`
	Reasoning     string           `json:"reasoning"`
	Text          string           `json:"text"`
	Error         string           `json:"error"`
}

type needleFuncCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func doComplete(ctx context.Context, input string) (*needleResponse, int, error) {
	body, err := json.Marshal(needleRequest{Input: input})
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+listenAddr+"/complete", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var parsed needleResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decoding needle response: %w", err)
	}
	return &parsed, resp.StatusCode, nil
}

// flattenMessages reduces an OpenAI-shaped message history down to the
// single input string Needle's API accepts. Needle is a short-command
// tool-router, not a long-context chat model (45M parameters, no documented
// multi-turn API), so this is a lossy heuristic, not a real substitute for
// message-array context:
//
// ponytail: sends the system prompt plus every message flattened as
// "role: content" lines. If real usage shows Needle losing track of context
// on longer conversations, narrow this to (system prompt + last user
// message + recent tool results) instead of the full transcript — that
// matches how the model was actually designed to be used.
func flattenMessages(messages []map[string]any) string {
	lines := make([]string, 0, len(messages))
	for _, m := range messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		if content == "" {
			continue
		}
		lines = append(lines, role+": "+content)
	}
	return strings.Join(lines, "\n")
}

// toOpenAIMessage converts a parsed Needle response into the same
// role/content/tool_calls map shape internal/api/ai_chat.go already expects
// back from callChatCompletion, so the tool-calling loop needs no special
// case for this provider. nr.Reasoning — Needle's own explanation of what
// it's about to do or why — rides along as "reasoning_content" whenever it
// says something distinct from the final answer, the same field name a real
// streaming reasoning model's delta uses (see ai_chat.go's
// streamChunkDelta), so ai_chat.go's single check for that key covers both.
func toOpenAIMessage(nr *needleResponse) (map[string]any, error) {
	if nr.Error != "" {
		return nil, fmt.Errorf("needle: %s", nr.Error)
	}
	if len(nr.FunctionCalls) > 0 {
		calls := make([]any, 0, len(nr.FunctionCalls))
		for i, fc := range nr.FunctionCalls {
			args, err := json.Marshal(fc.Arguments)
			if err != nil {
				return nil, err
			}
			calls = append(calls, map[string]any{
				"id":   fmt.Sprintf("needle_call_%d", i),
				"type": "function",
				"function": map[string]any{
					"name":      fc.Name,
					"arguments": string(args),
				},
			})
		}
		msg := map[string]any{"role": "assistant", "content": nil, "tool_calls": calls}
		if nr.Reasoning != "" {
			msg["reasoning_content"] = nr.Reasoning
		}
		return msg, nil
	}
	if nr.Text != "" {
		msg := map[string]any{"role": "assistant", "content": nr.Text}
		if nr.Reasoning != "" && nr.Reasoning != nr.Text {
			msg["reasoning_content"] = nr.Reasoning
		}
		return msg, nil
	}
	// No text and no function calls: Reasoning (if any) IS the answer here,
	// not a distinct "thinking" aside — surfacing it twice would just show
	// the same sentence in both the thinking block and the reply.
	return map[string]any{"role": "assistant", "content": nr.Reasoning}, nil
}

// ChatCompletion is Needle's counterpart to internal/api/ai_chat.go's
// callChatCompletion — same (message, status, error) return shape (minus
// baseURL/apiKey/model, which are meaningless for a fixed local subprocess,
// and minus the streaming plumbing real HTTP providers get, since Needle has
// no token-streaming protocol of its own), so ai_chat.go can call whichever
// one applies with no other branching; its caller always treats the result
// as a complete, non-streamed response and types it out itself.
func (m *Manager) ChatCompletion(ctx context.Context, messages []map[string]any, _ []map[string]any) (map[string]any, int, error) {
	// Needle handles one request at a time (see reqMu's doc comment) — hold
	// this for the whole round-trip so a second caller queues behind this
	// one instead of racing it against the same subprocess.
	m.reqMu.Lock()
	defer m.reqMu.Unlock()

	if err := m.ensureRunning(ctx); err != nil {
		return nil, http.StatusBadGateway, err
	}
	input := flattenMessages(messages)
	resp, status, err := doComplete(ctx, input)
	if err != nil {
		if died, log := m.crashInfo(); died {
			detail := "no output captured"
			if log != "" {
				detail = log
			}
			return nil, http.StatusBadGateway, fmt.Errorf(
				"needle process exited unexpectedly while handling this request (%s) — on a memory-constrained host this usually means it was killed for using too much memory (OOM); check `dmesg`/`journalctl -k` for an oom-kill message: %w",
				detail, err)
		}
		return nil, status, err
	}
	msg, err := toOpenAIMessage(resp)
	if err != nil {
		return nil, status, err
	}
	return msg, http.StatusOK, nil
}

// crashInfo reports whether the subprocess has died since it was last
// confirmed healthy, plus whatever it printed before doing so — used by
// ChatCompletion to explain a request that failed with a bare network error
// (e.g. "connection reset by peer") that carries no hint of why on its own.
func (m *Manager) crashInfo() (died bool, log string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.running, m.log.String()
}

// TestConnection is the built-in provider's counterpart to
// testProviderConnection (internal/api/ai_providers.go) — used by the
// Settings "test" button and by model discovery, since there's no real
// GET /models endpoint to call.
func (m *Manager) TestConnection(ctx context.Context) ([]string, error) {
	if err := m.ensureRunning(ctx); err != nil {
		return nil, err
	}
	return []string{"needle2"}, nil
}

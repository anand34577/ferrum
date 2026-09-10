package needle

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestWriteToolsFileRespectsMaxToolsEnvVar(t *testing.T) {
	SetToolCatalog(func() []ToolDef {
		return []ToolDef{
			{Name: "a", InputSchema: map[string]any{"type": "object"}},
			{Name: "b", InputSchema: map[string]any{"type": "object"}},
			{Name: "c", InputSchema: map[string]any{"type": "object"}},
		}
	})
	t.Cleanup(func() { SetToolCatalog(nil) })

	readTools := func() []needleTool {
		path, err := writeToolsFile()
		if err != nil {
			t.Fatalf("writeToolsFile: %v", err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading tools file: %v", err)
		}
		var out []needleTool
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("unmarshaling tools file: %v", err)
		}
		return out
	}

	t.Setenv("FERRUM_NEEDLE_MAX_TOOLS", "2")
	if got := readTools(); len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("with FERRUM_NEEDLE_MAX_TOOLS=2, got %+v, want the first 2 tools", got)
	}

	t.Setenv("FERRUM_NEEDLE_MAX_TOOLS", "0")
	if got := readTools(); len(got) != 3 {
		t.Fatalf("with FERRUM_NEEDLE_MAX_TOOLS=0, got %d tools, want all 3 (no limit)", len(got))
	}

	t.Setenv("FERRUM_NEEDLE_MAX_TOOLS", "not-a-number")
	if got := readTools(); len(got) != 3 {
		t.Fatalf("with a garbage FERRUM_NEEDLE_MAX_TOOLS, got %d tools, want all 3 (no limit)", len(got))
	}
}

func TestCrashInfoReportsProcessDeath(t *testing.T) {
	m := NewManager("")

	// Never started: running defaults false, so crashInfo must say "died"
	// (there's nothing to be healthy) rather than silently returning false.
	if died, _ := m.crashInfo(); !died {
		t.Fatal("expected crashInfo to report died=true for a never-started manager")
	}

	m.running = true
	m.log.Write([]byte("panic: out of memory\n"))
	if died, log := m.crashInfo(); died {
		t.Fatalf("expected died=false while running=true, got died=%v log=%q", died, log)
	}

	m.running = false
	died, log := m.crashInfo()
	if !died {
		t.Fatal("expected died=true once running is false")
	}
	if log != "panic: out of memory" {
		t.Fatalf("log = %q, want the captured subprocess output", log)
	}
}

func TestIsBuiltin(t *testing.T) {
	if !IsBuiltin(BaseURL) {
		t.Fatal("expected the sentinel URL to be recognized as built-in")
	}
	if IsBuiltin("https://api.openai.com/v1") {
		t.Fatal("a real provider URL must not be mistaken for the built-in one")
	}
}

// withNoBundledBinary simulates running on a platform Ferrum doesn't embed
// a Needle binary for (see bundled_other.go), so tests of the "nothing
// available" path aren't at the mercy of which OS/arch actually runs them —
// every supported CI platform (windows/amd64, linux/amd64, linux/arm64,
// darwin/arm64) now has one baked in via go:embed.
func withNoBundledBinary(t *testing.T) {
	t.Helper()
	orig := bundledBinary
	bundledBinary = nil
	t.Cleanup(func() { bundledBinary = orig })
}

func TestAvailableFalseWhenBinaryMissing(t *testing.T) {
	withNoBundledBinary(t)
	m := NewManager("")
	if m.Available() {
		t.Fatal("expected Available() to be false with no configured path and no bundled binary")
	}
	m2 := NewManager("/does/not/exist/needle")
	if m2.Available() {
		t.Fatal("expected Available() to be false for a nonexistent configured path")
	}
}

func TestAvailableTrueWhenBundled(t *testing.T) {
	if len(bundledBinary) == 0 {
		t.Skip("no binary bundled for this platform")
	}
	m := NewManager("")
	if !m.Available() {
		t.Fatal("expected Available() to be true when a binary is bundled for this platform and no override is configured")
	}
}

func TestExplicitBinPathWinsOverBundled(t *testing.T) {
	m := NewManager("/does/not/exist/needle")
	if m.Available() {
		t.Fatal("an explicitly configured (but missing) path must not silently fall back to the bundled binary")
	}
}

func TestChatCompletionFailsClearlyWhenNotInstalled(t *testing.T) {
	withNoBundledBinary(t)
	m := NewManager("")
	_, status, err := m.ChatCompletion(context.Background(), []map[string]any{{"role": "user", "content": "hi"}}, nil)
	if err == nil {
		t.Fatal("expected an error when the binary isn't installed")
	}
	if status != 502 {
		t.Fatalf("status = %d, want 502 (bad gateway)", status)
	}
}

func TestTestConnectionFailsClearlyWhenNotInstalled(t *testing.T) {
	withNoBundledBinary(t)
	m := NewManager("")
	if _, err := m.TestConnection(context.Background()); err == nil {
		t.Fatal("expected an error when the binary isn't installed")
	}
}

func TestFlattenMessages(t *testing.T) {
	got := flattenMessages([]map[string]any{
		{"role": "system", "content": "You are helpful."},
		{"role": "user", "content": "list my nodes"},
	})
	want := "system: You are helpful.\nuser: list my nodes"
	if got != want {
		t.Fatalf("flattenMessages() = %q, want %q", got, want)
	}
}

func TestFlattenMessagesSkipsEmptyContent(t *testing.T) {
	got := flattenMessages([]map[string]any{
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "hello"},
	})
	if got != "user: hello" {
		t.Fatalf("flattenMessages() = %q, want %q", got, "user: hello")
	}
}

func TestToOpenAIMessageWithFunctionCalls(t *testing.T) {
	nr := &needleResponse{
		Type:    "call",
		Success: boolPtr(true),
		FunctionCalls: []needleFuncCall{
			{Name: "guest_power_action", Arguments: map[string]any{"vmid": float64(100), "action": "start"}},
		},
		Reasoning: "user asked to start the guest",
	}
	msg, err := toOpenAIMessage(nr)
	if err != nil {
		t.Fatalf("toOpenAIMessage: %v", err)
	}
	if msg["role"] != "assistant" {
		t.Fatalf("role = %v, want assistant", msg["role"])
	}
	calls, ok := msg["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls = %#v, want one call", msg["tool_calls"])
	}
	call := calls[0].(map[string]any)
	fn := call["function"].(map[string]any)
	if fn["name"] != "guest_power_action" {
		t.Fatalf("function name = %v, want guest_power_action", fn["name"])
	}
	if _, isString := fn["arguments"].(string); !isString {
		t.Fatalf("function arguments must be a JSON string (matching the OpenAI shape), got %T", fn["arguments"])
	}
	if msg["reasoning_content"] != "user asked to start the guest" {
		t.Fatalf("reasoning_content = %v, want the model's reasoning surfaced alongside the tool call", msg["reasoning_content"])
	}
}

func TestToOpenAIMessageFallsBackToReasoningWhenNoTextOrCalls(t *testing.T) {
	nr := &needleResponse{Reasoning: "nothing to call, just answering"}
	msg, err := toOpenAIMessage(nr)
	if err != nil {
		t.Fatalf("toOpenAIMessage: %v", err)
	}
	if msg["content"] != "nothing to call, just answering" {
		t.Fatalf("content = %v, want the reasoning fallback", msg["content"])
	}
}

func TestToOpenAIMessagePrefersTextAndSurfacesReasoningSeparately(t *testing.T) {
	nr := &needleResponse{Text: "the answer", Reasoning: "thinking about it"}
	msg, err := toOpenAIMessage(nr)
	if err != nil {
		t.Fatalf("toOpenAIMessage: %v", err)
	}
	if msg["content"] != "the answer" {
		t.Fatalf("content = %v, want %q", msg["content"], "the answer")
	}
	if msg["reasoning_content"] != "thinking about it" {
		t.Fatalf("reasoning_content = %v, want the distinct reasoning text", msg["reasoning_content"])
	}
}

func TestToOpenAIMessageOmitsReasoningWhenIdenticalToText(t *testing.T) {
	nr := &needleResponse{Text: "same", Reasoning: "same"}
	msg, err := toOpenAIMessage(nr)
	if err != nil {
		t.Fatalf("toOpenAIMessage: %v", err)
	}
	if _, ok := msg["reasoning_content"]; ok {
		t.Fatalf("reasoning_content = %v, want it omitted when identical to content", msg["reasoning_content"])
	}
}

func TestToOpenAIMessagePropagatesError(t *testing.T) {
	nr := &needleResponse{Error: "model load failed"}
	if _, err := toOpenAIMessage(nr); err == nil {
		t.Fatal("expected an error to be propagated")
	}
}

func TestSetToolCatalogFeedsWriteToolsFile(t *testing.T) {
	SetToolCatalog(func() []ToolDef {
		return []ToolDef{{Name: "list_nodes", Description: "list nodes", InputSchema: map[string]any{"type": "object"}}}
	})
	t.Cleanup(func() { SetToolCatalog(nil) })

	path, err := writeToolsFile()
	if err != nil {
		t.Fatalf("writeToolsFile: %v", err)
	}
	if path == "" {
		t.Fatal("expected a non-empty tools file path")
	}
}

func boolPtr(b bool) *bool { return &b }

package api

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatToolRoundAsAnswerRendersArrayResult(t *testing.T) {
	round := []toolRoundResult{
		{name: "list_nodes", result: `[{"node":"pve1","status":"online"},{"node":"pve2","status":"offline"}]`},
	}
	got := formatToolRoundAsAnswer(round)
	want := "**List Nodes**\n\n- node: pve1, status: online\n- node: pve2, status: offline"
	if got != want {
		t.Fatalf("formatToolRoundAsAnswer() = %q, want %q", got, want)
	}
}

func TestFormatToolRoundAsAnswerRendersError(t *testing.T) {
	round := []toolRoundResult{{name: "guest_power_action", result: "admin access required", isErr: true}}
	got := formatToolRoundAsAnswer(round)
	if got != "**Guest Power Action** failed: admin access required" {
		t.Fatalf("formatToolRoundAsAnswer() = %q", got)
	}
}

func TestFormatJSONResultFallsBackToRawTextForNonJSON(t *testing.T) {
	raw := `action "start" submitted for pve1/qemu/100 — task UPID:...`
	if got := formatJSONResult(raw); got != raw {
		t.Fatalf("formatJSONResult() = %q, want raw text unchanged", got)
	}
}

func TestFormatJSONResultRendersNestedObjects(t *testing.T) {
	// Mirrors get_node_status's real shape: top-level scalars plus nested
	// cpuinfo/memory objects — the case formatJSONValue exists for.
	raw := `{"cpu":0.05,"cpuinfo":{"cores":4,"model":"Intel"},"loadavg":["0.1","0.2"]}`
	got := formatJSONResult(raw)
	want := "- cpu: 0.05, cpuinfo: {cores: 4, model: Intel}, loadavg: [0.1, 0.2]"
	if got != want {
		t.Fatalf("formatJSONResult() = %q, want %q", got, want)
	}
}

func TestFormatJSONResultHandlesEmptyArray(t *testing.T) {
	if got := formatJSONResult(`[]`); got != "_(none)_" {
		t.Fatalf("formatJSONResult() = %q, want \"_(none)_\"", got)
	}
}

// sseBody builds a fake OpenAI-style streaming response body out of raw
// chunk JSON strings, terminated with the usual [DONE] sentinel.
func sseBody(chunks ...string) *strings.Reader {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString("data: " + c + "\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return strings.NewReader(b.String())
}

func TestParseChatCompletionStreamForwardsContentLive(t *testing.T) {
	rec := httptest.NewRecorder()
	body := sseBody(
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":", world"}}]}`,
	)

	msg, streamedLive, err := parseChatCompletionStream(body, rec, rec, true)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !streamedLive {
		t.Fatal("expected content to be flagged as streamed live")
	}
	if got := msg["content"]; got != "Hello, world" {
		t.Fatalf("expected reconstructed content %q, got %q", "Hello, world", got)
	}
	if !strings.Contains(rec.Body.String(), "Hello") {
		t.Fatalf("expected the recorder to have received streamed chunks, got %q", rec.Body.String())
	}
}

func TestParseChatCompletionStreamForwardsReasoningSeparatelyFromContent(t *testing.T) {
	rec := httptest.NewRecorder()
	body := sseBody(
		`{"choices":[{"delta":{"reasoning_content":"Let me check "}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"the nodes."}}]}`,
		`{"choices":[{"delta":{"content":"Here you go."}}]}`,
	)

	msg, _, err := parseChatCompletionStream(body, rec, rec, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := msg["content"]; got != "Here you go." {
		t.Fatalf("content = %q, want %q", got, "Here you go.")
	}
	if _, ok := msg["reasoning_content"]; ok {
		t.Fatal("reasoning must never land in the returned message — it would pollute conversation history")
	}
	body2 := rec.Body.String()
	if !strings.Contains(body2, `"ferrum_reasoning":"Let me check "`) || !strings.Contains(body2, `"ferrum_reasoning":"the nodes."`) {
		t.Fatalf("expected reasoning chunks forwarded as ferrum_reasoning envelopes, got %q", body2)
	}
}

func TestParseChatCompletionStreamReconstructsFragmentedToolCall(t *testing.T) {
	rec := httptest.NewRecorder()
	// A real provider streams a tool call's id/name/arguments across many
	// chunks, addressed by index — this is the exact shape that needs
	// reassembling before the rest of the tool-calling loop can use it.
	body := sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"list_","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"nodes","arguments":"{\"conn"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ectionId\":\"a\"}"}}]}}]}`,
	)

	msg, streamedLive, err := parseChatCompletionStream(body, rec, rec, true)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if streamedLive {
		t.Fatal("a tool-call-only response has no content to stream")
	}
	calls, _ := msg["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("expected exactly one reconstructed tool call, got %d", len(calls))
	}
	call := calls[0].(map[string]any)
	if call["id"] != "call_1" {
		t.Fatalf("expected id %q, got %v", "call_1", call["id"])
	}
	fn := call["function"].(map[string]any)
	if fn["name"] != "list_nodes" {
		t.Fatalf("expected reassembled name %q, got %v", "list_nodes", fn["name"])
	}
	if fn["arguments"] != `{"connectionId":"a"}` {
		t.Fatalf("expected reassembled arguments %q, got %v", `{"connectionId":"a"}`, fn["arguments"])
	}
}

func TestParseChatCompletionStreamSuppressesLeakedToolCallText(t *testing.T) {
	rec := httptest.NewRecorder()
	// A model that doesn't really support function-calling echoing a fake
	// call as plain content — must never reach the browser when leak
	// checking is enabled, even though it arrives as ordinary content deltas.
	body := sseBody(`{"choices":[{"delta":{"content":"{\"name\": \"list_nodes\", \"arguments\": {}}"}}]}`)

	msg, streamedLive, err := parseChatCompletionStream(body, rec, rec, true)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if streamedLive {
		t.Fatal("a leaked tool call must never be forwarded to the browser")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected nothing written to the client, got %q", rec.Body.String())
	}
	// The caller still needs the full text to run its retry/fallback logic.
	if !looksLikeLeakedToolCall(msg["content"].(string)) {
		t.Fatalf("expected the reconstructed content to still be available to the caller, got %v", msg["content"])
	}
}

// TestParseChatCompletionStreamSurfacesMidStreamError reproduces what a
// local LM Studio runtime sends when its model crashes/unloads partway
// through a response: a `data:` line carrying an "error" object instead of
// the usual "choices" — observed as `{"error":{"message":"terminated"}}`
// after a run of "reasoning_content"-only chunks. Before this test, that
// line had no "choices" key so it was silently skipped, and the caller got
// back an empty, no-error response — reported to the user as "ran out of
// tool-call iterations" instead of the real failure.
func TestParseChatCompletionStreamSurfacesMidStreamError(t *testing.T) {
	rec := httptest.NewRecorder()
	body := sseBody(
		`{"choices":[{"delta":{"reasoning_content":"thinking..."}}]}`,
		`{"error":{"message":"terminated"}}`,
	)

	_, _, err := parseChatCompletionStream(body, rec, rec, true)

	if err == nil {
		t.Fatal("expected the mid-stream error to be surfaced, got nil")
	}
	if !strings.Contains(err.Error(), "terminated") {
		t.Fatalf("expected the error to carry the provider's message, got %v", err)
	}
	var se *streamError
	if !errors.As(err, &se) {
		t.Fatalf("expected a *streamError (so the caller reports it as a provider error, not an unreachable one), got %T", err)
	}
}

// errReader fails every Read after emitting some valid bytes, simulating a
// dropped connection mid-stream.
type errReader struct {
	data []byte
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// TestParseChatCompletionStreamSurfacesScanError reproduces a connection
// dropping mid-stream: bufio.Scanner gives up silently (Scan returns false,
// no more lines) on a read error, which used to look identical to a clean
// [DONE] and hand back whatever partial answer had accumulated as if
// nothing had gone wrong.
func TestParseChatCompletionStreamSurfacesScanError(t *testing.T) {
	rec := httptest.NewRecorder()
	wantErr := errors.New("connection reset")
	body := &errReader{data: []byte(`data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n"), err: wantErr}

	_, _, err := parseChatCompletionStream(body, rec, rec, true)

	if err == nil {
		t.Fatal("expected the scan error to be surfaced, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the underlying read error to be wrapped in, got %v", err)
	}
}

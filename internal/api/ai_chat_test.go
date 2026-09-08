package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

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

	msg, streamedLive := parseChatCompletionStream(body, rec, rec, true)

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

	msg, streamedLive := parseChatCompletionStream(body, rec, rec, true)

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

	msg, streamedLive := parseChatCompletionStream(body, rec, rec, true)

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

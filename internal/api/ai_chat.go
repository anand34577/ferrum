package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ferrum/internal/mcp"
	"ferrum/internal/needle"
)

// systemPrompt scopes the assistant to Ferrum/Proxmox operations — it is
// prepended to every conversation server-side, never sent by the client, so
// there's no way for a chat message to override it into a general-purpose
// assistant. Real fleet data is fetched through the tool-calling loop below,
// never guessed.
const systemPrompt = `You are the Ferrum AI Assistant, built into the Ferrum fleet-control application for managing Proxmox VE infrastructure.

Scope: you help ONLY with this application and the user's Proxmox/Ferrum-managed infrastructure — nodes, VMs, containers, storage, backups, replication, high availability, firewall rules, cluster/SDN configuration, alerts, resource pools, and how to use Ferrum's own features. You are a specialized operations assistant, not a general-purpose chatbot: politely decline requests unrelated to this domain (general programming help, trivia, personal advice, creative writing, etc.) and steer the conversation back to fleet management.

Tools: you have tools to query the user's real, live infrastructure (connections, nodes, guests, storage, pools, alerts, cluster status) and — for administrators only — to start/stop/reboot a guest. ALWAYS call a tool to look up current data before answering a question about the user's actual infrastructure; never guess or invent connection IDs, node names, VMIDs, or statuses. If a mutating tool call is rejected for lacking admin rights, say so plainly rather than pretending it succeeded.

Style: be concise and technical. Use markdown — lists and fenced code blocks — when it improves clarity, but don't pad answers with filler.`

type chatMessage struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

type aiChatRequest struct {
	ModelID  string        `json:"modelId,omitempty"` // an ai_provider_models row id; falls back to the configured default model
	Messages []chatMessage `json:"messages"`
}

// toolCallEnvelope is a Ferrum-specific SSE event (not part of the
// OpenAI chunk format) that reports tool-calling activity as it happens, so
// the UI can render "Checking guest status…" style progress instead of a
// long silent wait during multi-step tool use.
type toolCallEnvelope struct {
	ToolCall *toolActivity `json:"ferrum_tool_call,omitempty"`
}
type toolResultEnvelope struct {
	ToolResult *toolActivity `json:"ferrum_tool_result,omitempty"`
}
type toolActivity struct {
	Name string `json:"name"`
	Args any    `json:"args,omitempty"`
	OK   bool   `json:"ok,omitempty"`
}

// aiChat runs the Ferrum-scoped assistant: a bounded tool-calling loop
// against the selected (or default) provider, streamed to the browser as
// SSE — both the tool-call/tool-result progress events and, once the model
// settles on a final answer, the answer itself (typed out in small chunks
// for a live feel; see streamText). Available to every authenticated user —
// provider *configuration* is admin-only, but using the assistant isn't.
func (s *Server) aiChat(w http.ResponseWriter, r *http.Request) {
	var req aiChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "expected a JSON object")
		return
	}
	if len(req.Messages) == 0 {
		writeErrorMsg(w, http.StatusBadRequest, "messages must not be empty")
		return
	}

	modelRowID := req.ModelID
	if modelRowID == "" {
		id, err := s.defaultModelRowID(r.Context())
		if err == sql.ErrNoRows {
			writeErrorMsg(w, http.StatusBadRequest, "no default AI model is configured — ask an admin to add one in Settings")
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		modelRowID = id
	}

	baseURL, apiKey, model, err := s.loadModelForChat(r.Context(), modelRowID)
	if err != nil {
		writeErrorMsg(w, http.StatusNotFound, "ai model not found, or its provider is disabled")
		return
	}

	agentSettings, err := s.loadAgentSettings(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}

	// A local model — or several tool round-trips against it — can easily
	// exceed the 30s global request timeout applied in Router(). Detach from
	// that inherited deadline (keeping request-scoped values like the
	// authenticated user) and apply a generous one of our own: this is a
	// long-poll-shaped endpoint by nature, not a typical CRUD call.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Minute)
	defer cancel()
	user := userFromContext(r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	messages := make([]map[string]any, 0, len(req.Messages)+1)
	messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	for _, m := range req.Messages {
		messages = append(messages, map[string]any{"role": m.Role, "content": m.Content})
	}

	tools := buildToolDefs()
	toolsSupported := true
	var finalContent string

	for i := 0; i < agentSettings.maxToolIterations; i++ {
		reqTools := tools
		if !toolsSupported {
			reqTools = nil
		}
		respMsg, status, err := s.callChatCompletion(ctx, baseURL, apiKey, model, messages, reqTools)
		if err != nil {
			writeSSEError(w, flusher, "could not reach AI provider: "+err.Error())
			return
		}
		if status >= 300 {
			// Some OpenAI-compatible runtimes (older LocalAI/LM Studio
			// builds) reject an unrecognized "tools" field outright — retry
			// once without it rather than failing the whole conversation.
			if toolsSupported && i == 0 {
				toolsSupported = false
				i--
				continue
			}
			writeSSEError(w, flusher, fmt.Sprintf("AI provider returned %d", status))
			return
		}

		toolCalls, _ := respMsg["tool_calls"].([]any)
		if len(toolCalls) == 0 {
			content, _ := respMsg["content"].(string)
			// Some models/runtimes accept a "tools" field but don't actually
			// implement structured function-calling — instead of populating
			// tool_calls, they echo a fake call as plain text. Never show
			// that to the user: disable tools and let the model answer in
			// plain language instead.
			if toolsSupported && i == 0 && looksLikeLeakedToolCall(content) {
				toolsSupported = false
				i--
				continue
			}
			finalContent = content
			break
		}

		messages = append(messages, respMsg)
		for _, raw := range toolCalls {
			tc, _ := raw.(map[string]any)
			id, _ := tc["id"].(string)
			fn, _ := tc["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)

			var argsParsed any
			_ = json.Unmarshal([]byte(argsStr), &argsParsed)
			writeSSEJSON(w, flusher, toolCallEnvelope{ToolCall: &toolActivity{Name: name, Args: argsParsed}})

			resultText, isErr := s.mcp.CallTool(ctx, user, "chat", name, json.RawMessage(argsStr))
			writeSSEJSON(w, flusher, toolResultEnvelope{ToolResult: &toolActivity{Name: name, OK: !isErr}})

			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": id, "content": resultText})
		}
	}

	if finalContent == "" {
		finalContent = fmt.Sprintf(
			"I wasn't able to finish this within the current limit of %d tool calls — try breaking your question into smaller steps, or ask an admin to raise the limit under Settings > Agent & MCP.",
			agentSettings.maxToolIterations,
		)
	} else if looksLikeLeakedToolCall(finalContent) {
		// The retry above already tried disabling tools once; if the model
		// is still emitting tool-call-shaped text, showing it as-is would
		// just put broken JSON in front of the user. Be honest instead.
		finalContent = "This model attempted to use a tool but doesn't reliably support function-calling, so I can't confirm real data from your infrastructure this way. Try rephrasing without asking it to \"use tools,\" or switch to a model/provider known to support function-calling (e.g. GPT-4o-mini, or a larger Qwen2.5/Llama 3.1 build) for questions that need live fleet data."
	}
	streamText(w, flusher, finalContent)
}

// leakedToolCallPattern matches the shapes observed in the wild when a model
// that doesn't truly support function-calling still tries to "use" a tool by
// writing something tool-call-shaped into plain content instead of the
// structured tool_calls field: <tool_call>/<tools> XML-ish wrappers, a bare
// {"type": "function", "function": {...}} object, or the simpler
// {"name": "...", "arguments": {...}} shape smaller models tend to imitate.
// Small/uncensored local models keep doing this even once "tools" is no
// longer offered — it's baked into how they were fine-tuned to respond to
// "use your tools" phrasing, not something a request shape can prevent.
var leakedToolCallPattern = regexp.MustCompile(`(?i)<tool_call>|</tools?>|"type"\s*:\s*"function"|"name"\s*:\s*"[a-zA-Z_]+"\s*,\s*"arguments"\s*:`)

func looksLikeLeakedToolCall(content string) bool {
	return leakedToolCallPattern.MatchString(content)
}

// buildToolDefs converts the shared MCP tool catalog into the OpenAI
// "tools" (function-calling) schema — one definition, two consumers.
func buildToolDefs() []map[string]any {
	defs := mcp.ToolDefinitions()
	out := make([]map[string]any, 0, len(defs))
	for _, t := range defs {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}
	return out
}

// callChatCompletion makes one non-streaming chat completion request and
// returns the assistant's message (role/content/tool_calls, as a generic
// map so tool_calls round-trip untouched back into the next request).
// baseURL == needle.BaseURL routes through the built-in Needle provider
// (internal/needle) instead of making a real HTTP call — see that
// package's doc comment for why it needs its own request/response shape.
func (s *Server) callChatCompletion(ctx context.Context, baseURL, apiKey, model string, messages []map[string]any, tools []map[string]any) (map[string]any, int, error) {
	if needle.IsBuiltin(baseURL) {
		return s.needle.ChatCompletion(ctx, messages, tools)
	}
	payload := map[string]any{"model": model, "messages": messages, "stream": false}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		slog.Warn("ai chat completion failed", "status", resp.StatusCode, "body", strings.TrimSpace(string(msg)))
		return nil, resp.StatusCode, nil
	}

	var parsed struct {
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, 0, err
	}
	if len(parsed.Choices) == 0 {
		return nil, 0, fmt.Errorf("provider returned no choices")
	}
	return parsed.Choices[0].Message, resp.StatusCode, nil
}

// streamText "types out" the final answer to the client in small chunks
// using the same OpenAI streaming-chunk shape the frontend already parses —
// used instead of true upstream streaming because the tool-calling loop
// above requires non-streaming round-trips (a model's tool_calls can't be
// known until its response is complete), but a long final answer landing
// all at once still reads worse than even a simulated typing effect.
func streamText(w http.ResponseWriter, flusher http.Flusher, text string) {
	const chunkRunes = 4
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkRunes {
		end := min(i+chunkRunes, len(runes))
		chunk := map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": string(runes[i:end])}}}}
		writeSSEJSON(w, flusher, chunk)
		time.Sleep(8 * time.Millisecond)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeSSEJSON(w http.ResponseWriter, flusher http.Flusher, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	flusher.Flush()
}

// writeSSEError reports a failure mid-stream — headers are already flushed
// by the time this can be called, so a normal writeErrorMsg JSON body isn't
// an option; the client's SSE reader watches for this same envelope shape.
func writeSSEError(w http.ResponseWriter, flusher http.Flusher, message string) {
	writeSSEJSON(w, flusher, map[string]any{"ferrum_error": message})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

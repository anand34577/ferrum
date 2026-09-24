package api

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
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

Scope: you help ONLY with this application and the user's Proxmox/Ferrum-managed infrastructure — nodes, VMs, containers, storage, backups, replication, high availability, firewall rules, cluster/SDN configuration, alerts, resource pools, and how to use Ferrum's own features. You are a specialized operations assistant, not a general-purpose chatbot.

If a request is unrelated to this domain — general programming help or code samples in a general-purpose language, trivia, personal advice, creative writing, math homework, etc. — do NOT answer it, not even briefly or as a courtesy before redirecting. Refuse immediately, in one short sentence, and say what you can help with instead. Never produce unrelated code, explanations, or examples "just this once."

Tools: you have tools to query the user's real, live infrastructure (connections, nodes, guests, storage, pools, alerts, cluster status) and — for administrators only — to start/stop/reboot a guest. ALWAYS call a tool to look up current data before answering a question about the user's actual infrastructure; never guess or invent connection IDs, node names, VMIDs, or statuses. If a mutating tool call is rejected for lacking admin rights, say so plainly rather than pretending it succeeded.

Style: be concise and technical. Use markdown — lists and fenced code blocks — when it improves clarity, but don't pad answers with filler.`

// offTopicLanguagePattern/offTopicSignalPattern/infraKeywordPattern back the
// system prompt's scope rule with an actual guarantee for the specific
// failure this was written for: a small/local model (the ones most likely
// to be run through an OpenAI-compatible provider here) answering a plain
// "write me a Java program" request anyway, sometimes with a token
// "by the way, I'm for Proxmox" note tacked on AFTER already answering it.
// System-prompt instructions alone are advisory — a model that doesn't
// reliably follow them just ignores this one too. This check runs before
// the model is ever called, so a match is refused deterministically
// regardless of what the model would have done.
//
// Deliberately narrow — a language name AND a generic "write me code"
// signal AND *no* infrastructure keyword at all — to avoid false-positives
// on real automation asks like "write a python script using proxmoxer to
// migrate these VMs" (infra keyword present, sails through untouched).
//
// ponytail: a regex heuristic, not a classifier. A phrasing it doesn't
// catch just falls through to the system prompt like before; upgrade this
// to a real moderation/classification call only if this list visibly stops
// catching real cases.
var (
	offTopicLanguagePattern = regexp.MustCompile(`(?i)\b(java|python|javascript|typescript|c\+\+|c#|golang|rust language|ruby|php|kotlin|swift)\b`)
	offTopicSignalPattern   = regexp.MustCompile(`(?i)\b(sample program|sample code|code example|example code|write (me |a )*program)\b|\bprogram\b`)
	infraKeywordPattern     = regexp.MustCompile(`(?i)\b(proxmox|ferrum|vms?|lxc|qemu|container|node|cluster|storage|backup|snapshot|firewall|\bha\b|resource pool|connection|guest|alert|sdn|webhook|replication|api key)\b`)
)

// looksLikeOffTopicCodeRequest is true only for the narrow, high-confidence
// case documented above.
func looksLikeOffTopicCodeRequest(text string) bool {
	return offTopicLanguagePattern.MatchString(text) && offTopicSignalPattern.MatchString(text) && !infraKeywordPattern.MatchString(text)
}

// lastUserMessageText is what looksLikeOffTopicCodeRequest checks — only
// the newest turn, so a stale off-topic message earlier in a long
// conversation's history never blocks an otherwise on-topic follow-up.
func lastUserMessageText(msgs []chatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

const offTopicRefusalMessage = "I'm scoped to Proxmox VE / Ferrum fleet management, so I can't help with general programming questions or code samples unrelated to your infrastructure. Ask me about nodes, guests, storage, backups, alerts, or anything else in your fleet instead."

type chatMessage struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

type aiChatRequest struct {
	ModelID  string        `json:"modelId,omitempty"` // an ai_provider_models row id; falls back to the configured default model
	Messages []chatMessage `json:"messages"`
	// ReasoningEffort maps straight through to the OpenAI-compatible
	// "reasoning_effort" request field (o-series/gpt-5-class reasoning
	// models) — "minimal" | "low" | "medium" | "high", or empty to omit it
	// and let the provider use its own default. Forwarded as-is; a runtime
	// that doesn't recognize the field just ignores it.
	//
	// It is ALSO mapped to chat_template_kwargs.enable_thinking (see
	// thinkingEnabled), the knob Qwen3-class models exposed through vLLM /
	// LM Studio / Ollama actually obey — those runtimes ignore
	// reasoning_effort entirely, so on its own the selector had no visible
	// effect there and "Reasoning off" still produced a thinking block.
	// Hosted APIs that reject the unknown field are handled by the
	// drop-and-retry in aiChat.
	//
	// ponytail: two knobs covers every runtime Ferrum talks to today.
	// Anthropic's thinking.budget_tokens would be a third — add it if a
	// native Anthropic provider ever lands.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// thinkingEnabled maps the OpenAI-style effort levels onto the binary
// on/off switch Qwen3-class chat templates expose. "" is the UI's
// "Reasoning off", and "minimal" is the closest thing to off that a
// graded model offers — both mean "don't think", everything else means
// "do".
func thinkingEnabled(reasoningEffort string) bool {
	return reasoningEffort != "" && reasoningEffort != "minimal"
}

var validReasoningEfforts = map[string]bool{"minimal": true, "low": true, "medium": true, "high": true}

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
	// ID is the provider's own tool_call_id — present so the client can
	// correlate a result back to its call even when the same tool name is
	// invoked twice in the same round (parallel tool calls), instead of
	// guessing via "most recent call with this name still running".
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Args   any    `json:"args,omitempty"`
	OK     bool   `json:"ok,omitempty"`
	Result string `json:"result,omitempty"`
}

// reasoningEnvelope carries a chunk of a "reasoning"/"thinking" model's
// chain-of-thought, kept out of "content" deltas so it never ends up in
// conversation history sent back to the model (see toOpenAIMessage and
// parseChatCompletionStream) — purely a display-time aside for the UI's
// collapsible "Thinking" block.
type reasoningEnvelope struct {
	Reasoning string `json:"ferrum_reasoning"`
}

// usageInfo is token accounting for one /ai/chat request — real numbers
// when the provider reports them (every OpenAI-compatible runtime that
// honors stream_options.include_usage, or any non-streaming response's
// "usage" object), estimated (Estimated=true, ~4 chars/token) when it
// doesn't — Needle in particular has no token accounting of its own.
type usageInfo struct {
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	ElapsedMs        int64   `json:"elapsedMs"`
	TokensPerSecond  float64 `json:"tokensPerSecond,omitempty"`
	Estimated        bool    `json:"estimated,omitempty"`
}
type usageEnvelope struct {
	Usage *usageInfo `json:"ferrum_usage,omitempty"`
}

// estimatedTokens is the ~4-chars-per-token rule of thumb used only when a
// provider gave us no real usage numbers to report instead.
func estimatedTokens(s string) int {
	return estimatedTokensFromLen(len(s))
}

func estimatedTokensFromLen(chars int) int {
	return (chars + 3) / 4
}

// toolResultDisplayLimit caps how much of a tool's result text is sent to
// the browser for display in the tool-activity pill — the full untruncated
// text still goes to the model via messages below; this only bounds what a
// human has to scroll through in the UI.
const toolResultDisplayLimit = 4000

func truncateForDisplay(s string) string {
	if len(s) <= toolResultDisplayLimit {
		return s
	}
	return s[:toolResultDisplayLimit] + "… (truncated)"
}

// toolRoundResult is one tool call's outcome, kept just long enough to build
// a fallback answer (see formatToolRoundAsAnswer) if the model never
// produces one of its own — unlike toolActivity, result here is the full
// text, not the display-truncated copy sent to the browser as a pill.
type toolRoundResult struct {
	name   string
	result string
	isErr  bool
}

// formatToolRoundAsAnswer turns a batch of tool results into a readable
// markdown summary, for providers — Needle chief among them, see
// internal/needle's doc comment — that call tools but never narrate the
// outcome in prose. It knows nothing about what any particular tool means:
// it just renders each tool's already-JSON result as a bullet list, so a
// newly added tool is summarized for free.
func formatToolRoundAsAnswer(round []toolRoundResult) string {
	var b strings.Builder
	for i, r := range round {
		if i > 0 {
			b.WriteString("\n\n")
		}
		label := humanizeToolName(r.name)
		if r.isErr {
			fmt.Fprintf(&b, "**%s** failed: %s", label, r.result)
			continue
		}
		fmt.Fprintf(&b, "**%s**\n\n%s", label, formatJSONResult(r.result))
	}
	return b.String()
}

// humanizeToolName renders a snake_case tool name ("list_guests") as a
// title-cased label ("List Guests") for display.
func humanizeToolName(name string) string {
	words := strings.Split(name, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// formatJSONResult renders a tool's raw result text as a markdown bullet
// list — one bullet per array element, or one list for a single object —
// falling back to the raw text unchanged when it isn't JSON at all (e.g.
// guest_power_action's plain confirmation sentence).
func formatJSONResult(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	switch val := v.(type) {
	case []any:
		if len(val) == 0 {
			return "_(none)_"
		}
		var b strings.Builder
		for _, item := range val {
			fmt.Fprintf(&b, "- %s\n", formatJSONItem(item))
		}
		return strings.TrimRight(b.String(), "\n")
	case map[string]any:
		return "- " + formatJSONItem(val)
	default:
		return raw
	}
}

// formatJSONItem renders one JSON object as "key: value, key: value" (keys
// sorted for stable output), or via formatJSONValue for anything else — used
// both for each element of an array result and, via formatJSONResult, for a
// single top-level object result.
func formatJSONItem(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return formatJSONValue(v)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, formatJSONValue(m[k])))
	}
	return strings.Join(parts, ", ")
}

// formatJSONValue renders one JSON value inline — recursing into nested
// objects/arrays instead of falling through to Go's raw "map[k:v]" %v
// syntax. Several real tool results nest structs a level or two deep
// (get_node_status's cpuinfo/memory/swap/rootfs, cluster_status's per-member
// fields), so without this they'd render unreadably despite the top level
// looking fine.
func formatJSONValue(v any) string {
	switch val := v.(type) {
	case map[string]any:
		return "{" + formatJSONItem(val) + "}"
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = formatJSONValue(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", val)
	}
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
	if req.ReasoningEffort != "" && !validReasoningEfforts[req.ReasoningEffort] {
		writeErrorMsg(w, http.StatusBadRequest, "reasoningEffort must be one of: minimal, low, medium, high")
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
	// exceed 30s, so Router() exempts this route from the global timeout and
	// we apply a generous bound of our own. r.Context() stays the parent, so
	// a client disconnect (Stop) cancels the tool loop at any point.
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	user := userFromContext(r)

	clearWriteDeadline(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Defense in depth: local/third-party OpenAI-compatible runtimes
	// (LM Studio, LocalAI, ...) are known to return malformed or
	// unexpected-shaped responses under load or after an error, and an
	// unrecovered panic here doesn't fail cleanly — net/http's own recovery
	// just kills the TCP connection mid-chunk, which the browser reports as
	// a bare, unhelpful "network error" (see the nil-map fix a few lines
	// down in callChatCompletion for one real instance of this). Turn any
	// panic into a proper SSE error instead of a silent connection death.
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("ai chat handler panicked", "recovered", rec)
			writeSSEError(w, flusher, "Something went wrong handling the AI provider's response — please try again.")
		}
	}()

	// Deterministic guardrail — see looksLikeOffTopicCodeRequest's doc
	// comment. Checked, and refused, before the model is ever called: a
	// small/local model that doesn't reliably follow the system prompt's
	// scope rule can't produce the off-topic answer if it's never asked.
	if looksLikeOffTopicCodeRequest(lastUserMessageText(req.Messages)) {
		streamText(w, flusher, offTopicRefusalMessage)
		return
	}

	messages := make([]map[string]any, 0, len(req.Messages)+1)
	messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	for _, m := range req.Messages {
		messages = append(messages, map[string]any{"role": m.Role, "content": m.Content})
	}

	start := time.Now()
	tools := buildToolDefs()
	toolsSupported := true
	var finalContent string
	var finalAlreadyStreamed bool
	// Token accounting across every round of this request's tool-calling
	// loop — see the usageInfo doc comment for why it's real numbers when
	// the provider reports them and an estimate otherwise.
	var totalPromptTokens, totalCompletionTokens int
	var haveRealUsage bool
	// lastToolRound holds the most recent batch of tool calls/results — used
	// as a fallback answer when a provider (Needle, in practice: it's a pure
	// tool-router with no narrative output of its own) finishes calling tools
	// but comes back with nothing to say. Overwritten each round rather than
	// accumulated across the whole conversation, so it only ever describes
	// "what just happened" going into the empty final reply.
	var lastToolRound []toolRoundResult
	ranOutOfIterations := true
	// chat_template_kwargs is understood by local runtimes and rejected with
	// a 400 by strict hosted APIs (OpenAI: "Unrecognized request argument").
	// Offer it, drop it on the first such rejection — same shape as the
	// tools fallback below.
	thinkingHint := true

	for i := 0; i < agentSettings.maxToolIterations; i++ {
		reqTools := tools
		if !toolsSupported {
			reqTools = nil
		}
		// The leak check only matters while tools are still on the table —
		// once we've fallen back to plain-text mode, JSON-shaped content is
		// just as likely to be a legitimate answer as a leaked tool call.
		allowLeakCheck := toolsSupported
		respMsg, status, streamedLive, err := s.callChatCompletion(ctx, w, flusher, baseURL, apiKey, model, messages, reqTools, allowLeakCheck, req.ReasoningEffort, thinkingHint)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return // real client disconnect — nothing left to report
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				// Our own 4-minute ceiling fired, typically after several slow
				// tool round-trips — was previously swallowed as a silent
				// stream close, which the browser's fetch reader surfaced as
				// a bare "network error" with no explanation.
				writeSSEError(w, flusher, "The AI provider took too long to respond across several tool calls and this request timed out — try a smaller question or fewer tool calls at once.")
				return
			}
			// A streamError means we connected fine and the provider was
			// already responding (possibly with some content already
			// flushed live — the browser keeps whatever arrived before this
			// point, see streamError's doc comment) before it failed; say
			// so instead of the misleading "could not reach" framing that
			// belongs to an actual connection failure below. Either way the
			// upstream's own error text never reaches the browser — it can
			// carry provider-side detail (model paths, auth state) that
			// isn't this user's to see; only the sanitized message goes out.
			var se *streamError
			if errors.As(err, &se) {
				slog.Error("ai chat: provider failed mid-stream", "error", se.Error())
				writeSSEError(w, flusher, "the AI provider rejected the request")
			} else {
				slog.Error("ai chat: could not reach the AI provider", "error", err.Error())
				writeSSEError(w, flusher, "could not reach the AI provider")
			}
			return
		}
		if status >= 300 {
			// Some OpenAI-compatible runtimes (older LocalAI/LM Studio
			// builds) reject an unrecognized "tools" field outright — retry
			// once without it rather than failing the whole conversation.
			// Checked before the tools fallback: a strict hosted API rejects
			// the whole request over chat_template_kwargs, so blaming tools
			// for that 400 would drop function-calling for the rest of the
			// conversation over an unrelated field.
			if thinkingHint && status == http.StatusBadRequest {
				thinkingHint = false
				i--
				continue
			}
			// Only a "you sent something I don't understand" rejection is worth
			// retrying without tools — a 429 or a 5xx says nothing about the
			// tools field, and retrying it immediately just spends another
			// request against a provider that already asked us to slow down.
			if toolsSupported && i == 0 && status < 500 && status != http.StatusTooManyRequests {
				toolsSupported = false
				i--
				continue
			}
			if status == http.StatusTooManyRequests {
				writeSSEError(w, flusher, "The AI provider is rate-limiting this key (429) — wait a moment and retry, or switch to another model/provider.")
				return
			}
			writeSSEError(w, flusher, fmt.Sprintf("AI provider returned %d", status))
			return
		}

		// Pulled off respMsg (not left in it) before anything below appends
		// respMsg into the running `messages` history — a raw *rawUsage value
		// there would otherwise get JSON-marshaled back to the provider as
		// part of an "assistant" history message on the next tool-call round.
		if u, _ := respMsg["usage"].(*rawUsage); u != nil {
			totalPromptTokens += u.PromptTokens
			totalCompletionTokens += u.CompletionTokens
			haveRealUsage = true
			delete(respMsg, "usage")
		}

		// Non-streaming providers (Needle) can only hand back reasoning once
		// the whole response is in; a real streaming provider's own
		// "reasoning_content" deltas (see parseChatCompletionStream) are
		// already forwarded live and never land in respMsg, so this is a
		// no-op for those.
		if reasoning, _ := respMsg["reasoning_content"].(string); reasoning != "" {
			streamReasoningText(w, flusher, reasoning)
		}

		toolCalls, _ := respMsg["tool_calls"].([]any)
		if len(toolCalls) == 0 {
			content, _ := respMsg["content"].(string)
			// Some models/runtimes accept a "tools" field but don't actually
			// implement structured function-calling — instead of populating
			// tool_calls, they echo a fake call as plain text. Never show
			// that to the user: disable tools and let the model answer in
			// plain language instead. Only possible to retry cleanly when
			// nothing has been streamed to the browser yet.
			if toolsSupported && i == 0 && !streamedLive && looksLikeLeakedToolCall(content) {
				toolsSupported = false
				i--
				continue
			}
			finalContent = content
			finalAlreadyStreamed = streamedLive
			ranOutOfIterations = false
			break
		}

		messages = append(messages, respMsg)
		lastToolRound = lastToolRound[:0]
		for _, raw := range toolCalls {
			tc, _ := raw.(map[string]any)
			id, _ := tc["id"].(string)
			fn, _ := tc["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)

			var argsParsed any
			_ = json.Unmarshal([]byte(argsStr), &argsParsed)
			// The live envelope gets the same redaction recordToolCall applies
			// to the persisted copy — args often carry credentials.
			writeSSEJSON(w, flusher, toolCallEnvelope{ToolCall: &toolActivity{ID: id, Name: name, Args: redactSensitive(argsParsed)}})

			resultText, isErr := s.mcp.CallTool(ctx, user, "chat", name, json.RawMessage(argsStr))
			writeSSEJSON(w, flusher, toolResultEnvelope{ToolResult: &toolActivity{ID: id, Name: name, OK: !isErr, Result: truncateForDisplay(resultText)}})
			lastToolRound = append(lastToolRound, toolRoundResult{name: name, result: resultText, isErr: isErr})

			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": id, "content": resultText})
		}
	}

	if finalContent == "" && !ranOutOfIterations && len(lastToolRound) > 0 {
		// The model completed its tool calls and then had nothing further to
		// say — expected from Needle (a tool-router, not a chat model; see
		// internal/needle's doc comment) and possible from any provider.
		// Rather than the misleading "ran out of tool calls" message below
		// (nothing ran out — this finished successfully), turn the tool
		// results themselves into the answer.
		finalContent = formatToolRoundAsAnswer(lastToolRound)
	} else if finalContent == "" {
		finalContent = fmt.Sprintf(
			"I wasn't able to finish this within the current limit of %d tool calls — try breaking your question into smaller steps, or ask an admin to raise the limit under Settings > Agent & MCP.",
			agentSettings.maxToolIterations,
		)
	} else if !finalAlreadyStreamed && looksLikeLeakedToolCall(finalContent) {
		// The retry above already tried disabling tools once; if the model
		// is still emitting tool-call-shaped text, showing it as-is would
		// just put broken JSON in front of the user. Be honest instead. Only
		// possible when nothing has reached the browser yet — real streaming
		// below can't be un-sent.
		finalContent = "This model attempted to use a tool but doesn't reliably support function-calling, so I can't confirm real data from your infrastructure this way. Try rephrasing without asking it to \"use tools,\" or switch to a model/provider known to support function-calling (e.g. GPT-4o-mini, or a larger Qwen2.5/Llama 3.1 build) for questions that need live fleet data."
	}

	elapsed := time.Since(start)
	usage := usageInfo{ElapsedMs: elapsed.Milliseconds()}
	if haveRealUsage {
		usage.PromptTokens = totalPromptTokens
		usage.CompletionTokens = totalCompletionTokens
	} else {
		usage.Estimated = true
		usage.CompletionTokens = estimatedTokens(finalContent)
		var promptChars int
		for _, m := range messages {
			if c, ok := m["content"].(string); ok {
				promptChars += len(c)
			}
		}
		usage.PromptTokens = estimatedTokensFromLen(promptChars)
	}
	if secs := elapsed.Seconds(); secs > 0 {
		usage.TokensPerSecond = float64(usage.CompletionTokens) / secs
	}
	writeSSEJSON(w, flusher, usageEnvelope{Usage: &usage})

	if finalAlreadyStreamed {
		// Already flushed to the browser token-by-token as the provider
		// produced it — just close out the stream.
		writeSSEDone(w, flusher)
		return
	}
	// No real streaming happened for this content — either it's the
	// synthesized "ran out of iterations"/leak-fallback message, or it came
	// from a provider that doesn't stream (Needle) or fell back to a plain
	// JSON response. Type it out for a live feel rather than dumping it all
	// at once.
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

// leakPrefixWindow bounds how much content callChatCompletion buffers
// before deciding whether an iteration's answer looks like a leaked tool
// call (see looksLikeLeakedToolCall) — large enough to catch the documented
// shapes (which all appear at or near the very start of a leak), small
// enough that the common case (a real answer) starts streaming to the
// browser almost immediately instead of waiting for the whole response.
const leakPrefixWindow = 96

// callChatCompletion makes one chat-completion round-trip and returns the
// assistant's message (role/content/tool_calls, as a generic map so
// tool_calls round-trip untouched back into the next request) — plus
// whether any of its content was already flushed live to the browser as it
// arrived (see streamedLive below).
//
// baseURL == needle.BaseURL routes through the built-in Needle provider
// (internal/needle) instead of making a real HTTP call — see that
// package's doc comment for why it needs its own request/response shape;
// Needle has no token-streaming protocol of its own, so its full response
// always comes back with streamedLive == false and the caller types it out
// afterward (see streamText).
//
// Every other provider is asked to stream ("stream": true) so a long final
// answer reaches the browser token-by-token as the model actually produces
// it, instead of the old behavior of waiting for the entire response and
// then just simulating a typing effect. Content is buffered only up to
// leakPrefixWindow runes (or the first newline) before flushing starts, so
// allowLeakCheck can still catch — and fully suppress — the tool-call-shaped
// text some non-function-calling models emit instead of real answers; once
// that checkpoint passes clean, the rest streams live and can no longer be
// retracted if a leak pattern somehow shows up later in the same answer
// (rare in practice: the documented shapes all start right at the top).
func (s *Server) callChatCompletion(
	ctx context.Context, w http.ResponseWriter, flusher http.Flusher,
	baseURL, apiKey, model string, messages []map[string]any, tools []map[string]any, allowLeakCheck bool, reasoningEffort string, thinkingHint bool,
) (respMsg map[string]any, status int, streamedLive bool, err error) {
	if needle.IsBuiltin(baseURL) {
		respMsg, status, err = s.needle.ChatCompletion(ctx, messages, tools)
		return respMsg, status, false, err
	}

	// stream_options.include_usage asks any OpenAI-compatible runtime that
	// supports it to append one final chunk carrying real prompt/completion
	// token counts (see parseChatCompletionStream) — ignored harmlessly by
	// runtimes that don't recognize the field.
	payload := map[string]any{"model": model, "messages": messages, "stream": true, "stream_options": map[string]any{"include_usage": true}}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}
	if reasoningEffort != "" {
		payload["reasoning_effort"] = reasoningEffort
	}
	if thinkingHint {
		payload["chat_template_kwargs"] = map[string]any{"enable_thinking": thinkingEnabled(reasoningEffort)}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, false, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, 0, false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		slog.Warn("ai chat completion failed", "status", resp.StatusCode, "body", strings.TrimSpace(string(msg)))
		return nil, resp.StatusCode, false, nil
	}

	// A runtime that doesn't actually honor "stream": true (some older
	// LocalAI/LM Studio builds) may just return one normal JSON body
	// instead of an event-stream — fall back to parsing it the plain way.
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		var parsed struct {
			Choices []struct {
				Message map[string]any `json:"message"`
			} `json:"choices"`
			Usage *rawUsage `json:"usage"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return nil, 0, false, err
		}
		if len(parsed.Choices) == 0 {
			return nil, 0, false, fmt.Errorf("provider returned no choices")
		}
		msg := parsed.Choices[0].Message
		if msg == nil {
			// A choice with a null/missing "message" — seen from local
			// runtimes (LM Studio, older LocalAI builds) when generation
			// errors out after a rough tool-call round. json.Unmarshal
			// leaves a `map[string]any` nil for a JSON null, and writing
			// into a nil map panics — which net/http "recovers" from by
			// abruptly killing the TCP connection mid-response (no clean
			// chunked terminator), which the browser's fetch reader then
			// reports as a bare "network error" with zero context. Treat it
			// as an empty (contentless) message instead.
			msg = map[string]any{}
		}
		if parsed.Usage != nil {
			msg["usage"] = parsed.Usage
		}
		return msg, resp.StatusCode, false, nil
	}

	respMsg, streamedLive, err = parseChatCompletionStream(resp.Body, w, flusher, allowLeakCheck)
	return respMsg, resp.StatusCode, streamedLive, err
}

// rawUsage mirrors the OpenAI-compatible "usage" object — present on every
// non-streaming response, and on a streaming response's final chunk only
// when the request set stream_options.include_usage (see
// callChatCompletion). Stashed verbatim onto respMsg["usage"] so aiChat can
// turn it into the real (non-estimated) half of usageInfo.
type rawUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// streamChunkDelta mirrors the OpenAI streaming-chunk "delta" shape enough
// to reconstruct both plain content and function-calling tool_calls, which
// arrive as index-addressed fragments (id/name/arguments each build up
// across multiple chunks) rather than as one complete object.
type streamChunkDelta struct {
	Content string `json:"content"`
	// ReasoningContent is a reasoning-capable model's chain-of-thought,
	// streamed separately from the final answer — the field name an
	// increasing number of OpenAI-compatible runtimes use for it (vLLM,
	// DeepSeek-R1-family models, LM Studio's reasoning models, ...).
	// Forwarded live to the browser (see parseChatCompletionStream) but
	// never mixed into Content/full, so it never reaches conversation
	// history sent back to the model on the next tool-call iteration.
	ReasoningContent string `json:"reasoning_content"`
	ToolCalls        []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// parseChatCompletionStream reads an OpenAI-shaped SSE response body,
// forwarding content deltas to the browser as they arrive (real streaming)
// while reconstructing any tool_calls from their streamed fragments, and
// returns the equivalent of a full, non-streaming response message plus
// whether any content actually reached the browser this way.
//
// A provider that fails mid-stream (crash, OOM, model unload) doesn't
// re-send an HTTP error status — headers are already committed to 200 by
// then — it instead emits one more `data:` line carrying an "error" object
// in place of the usual "choices" (observed from a local LM Studio runtime:
// `data: {"error":{"message":"terminated"}}`). That line has no "choices"
// key, so decoding it into the chunk struct below silently produces zero
// choices and used to just be skipped — the loop would then hit EOF and
// return an empty, no-error response, which the caller reported as "ran out
// of tool-call iterations". Detect it and report the real failure instead.
// streamError marks a failure that happened after the connection to the
// provider already succeeded — mid-stream, possibly after some content was
// already flushed live to the browser — as opposed to callChatCompletion's
// other error returns (dial/timeout/marshal failures), which mean the
// provider was never reached at all. The two need different wording to the
// user; see the check in aiChat.
type streamError struct{ err error }

func (e *streamError) Error() string { return e.err.Error() }
func (e *streamError) Unwrap() error { return e.err }

func parseChatCompletionStream(body io.Reader, w http.ResponseWriter, flusher http.Flusher, allowLeakCheck bool) (respMsg map[string]any, streamedLive bool, err error) {
	type accTool struct{ id, name, args strings.Builder }
	toolAcc := map[int]*accTool{}
	toolOrder := []int{}
	var full, pending strings.Builder
	flushing := false
	leakSuppressed := false
	var usage *rawUsage

	flush := func(s string) {
		if s == "" {
			return
		}
		streamedLive = true
		writeSSEJSON(w, flusher, map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": s}}}})
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta streamChunkDelta `json:"delta"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			// Usage arrives on its own final chunk (empty Choices) only when
			// the request set stream_options.include_usage — see
			// callChatCompletion.
			Usage *rawUsage `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		if chunk.Error != nil {
			msg := chunk.Error.Message
			if msg == "" {
				msg = "provider reported an error mid-stream"
			}
			return respMsg, streamedLive, &streamError{fmt.Errorf("%s", msg)}
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		for _, tc := range delta.ToolCalls {
			acc, ok := toolAcc[tc.Index]
			if !ok {
				acc = &accTool{}
				toolAcc[tc.Index] = acc
				toolOrder = append(toolOrder, tc.Index)
			}
			acc.id.WriteString(tc.ID)
			acc.name.WriteString(tc.Function.Name)
			acc.args.WriteString(tc.Function.Arguments)
		}
		if delta.ReasoningContent != "" {
			writeSSEJSON(w, flusher, reasoningEnvelope{Reasoning: delta.ReasoningContent})
		}
		if delta.Content == "" {
			continue
		}
		full.WriteString(delta.Content)
		switch {
		case flushing:
			flush(delta.Content)
		case leakSuppressed:
			// Already decided this iteration is a leak — keep accumulating
			// into `full` (the caller still needs it for the retry/honest-
			// message logic) but never show it to the user.
		default:
			pending.WriteString(delta.Content)
			if pending.Len() >= leakPrefixWindow || strings.Contains(pending.String(), "\n") {
				if allowLeakCheck && looksLikeLeakedToolCall(pending.String()) {
					leakSuppressed = true
				} else {
					flushing = true
					flush(pending.String())
				}
				pending.Reset()
			}
		}
	}
	// bufio.Scanner silently gives up (Scan returns false with no line) on a
	// read error or a line past its 1MB buffer — both would otherwise look
	// like a clean [DONE] and hand back a truncated answer as if nothing
	// went wrong. Surface it instead of pretending the stream finished.
	if scanErr := scanner.Err(); scanErr != nil {
		return respMsg, streamedLive, &streamError{fmt.Errorf("reading provider stream: %w", scanErr)}
	}
	// A short answer that never reached the flush checkpoint is still
	// sitting in `pending` — resolve it the same way now that the stream
	// has ended.
	if !flushing && !leakSuppressed && pending.Len() > 0 {
		if !(allowLeakCheck && looksLikeLeakedToolCall(pending.String())) {
			flush(pending.String())
		}
	}

	respMsg = map[string]any{"role": "assistant", "content": full.String()}
	if len(toolAcc) > 0 {
		calls := make([]any, 0, len(toolAcc))
		for _, idx := range toolOrder {
			acc := toolAcc[idx]
			calls = append(calls, map[string]any{
				"id":   acc.id.String(),
				"type": "function",
				"function": map[string]any{
					"name":      acc.name.String(),
					"arguments": acc.args.String(),
				},
			})
		}
		respMsg["tool_calls"] = calls
	}
	if usage != nil {
		respMsg["usage"] = usage
	}
	return respMsg, streamedLive, nil
}

// streamReasoningText "types out" a non-streaming provider's (Needle's)
// reasoning text via reasoningEnvelope chunks — same idea as streamText, but
// without streamText's own trailing [DONE]: this can run mid-loop, before
// any tool calls or the final answer, so the SSE stream must stay open.
func streamReasoningText(w http.ResponseWriter, flusher http.Flusher, text string) {
	const chunkRunes = 4
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkRunes {
		end := min(i+chunkRunes, len(runes))
		writeSSEJSON(w, flusher, reasoningEnvelope{Reasoning: string(runes[i:end])})
		time.Sleep(8 * time.Millisecond)
	}
}

// streamText "types out" text to the client in small chunks using the same
// OpenAI streaming-chunk shape the frontend already parses — used only for
// text that was never actually streamed by a provider: Needle's response
// (no streaming protocol of its own), a provider that fell back to a plain
// JSON response, or a message Ferrum synthesized itself (the tool-limit or
// leaked-tool-call fallback text above).
func streamText(w http.ResponseWriter, flusher http.Flusher, text string) {
	const chunkRunes = 4
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkRunes {
		end := min(i+chunkRunes, len(runes))
		chunk := map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": string(runes[i:end])}}}}
		writeSSEJSON(w, flusher, chunk)
		time.Sleep(8 * time.Millisecond)
	}
	writeSSEDone(w, flusher)
}

// clearWriteDeadline lifts the server's global WriteTimeout (see
// cmd/ferrum/main.go) for one response. That timeout is right for normal
// request/response endpoints but fatal for SSE: once it fires, net/http kills
// the TCP connection mid-chunk with no [DONE] and no error event, which the
// browser's fetch reader reports as a bare "network error" — the exact
// failure seen after a few slow tool round-trips against a local model, since
// the handler's own 4-minute ceiling could never be reached.
func clearWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
}

func writeSSEDone(w http.ResponseWriter, flusher http.Flusher) {
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
// This carries no separate signal for content-delta events sent before it —
// the browser doesn't discard them, it keeps whatever text already streamed
// in and appends this as a toast, same as hitting Stop mid-answer (see
// runCompletion's catch/finally in AIAssistantPage.tsx).
func writeSSEError(w http.ResponseWriter, flusher http.Flusher, message string) {
	writeSSEJSON(w, flusher, map[string]any{"ferrum_error": message})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

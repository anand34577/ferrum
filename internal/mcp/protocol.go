// Package mcp implements a minimal Model Context Protocol server — the
// "tools/list" + "tools/call" subset over the Streamable HTTP transport
// (JSON-RPC 2.0, single POST endpoint) — so Ferrum can be added as a remote
// MCP server in Claude Desktop/Claude Code. It's hand-rolled rather than
// built on an external SDK to keep the dependency surface (and offline build
// risk) at zero; the protocol subset used here is small and stable.
package mcp

import "encoding/json"

const ProtocolVersion = "2025-06-18"

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func errorResponse(id json.RawMessage, code int, msg string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

func resultResponse(id json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

// --- initialize ---

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// --- tools ---

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

func textResult(text string) toolCallResult {
	return toolCallResult{Content: []content{{Type: "text", Text: text}}}
}

func errorResult(text string) toolCallResult {
	return toolCallResult{Content: []content{{Type: "text", Text: text}}, IsError: true}
}

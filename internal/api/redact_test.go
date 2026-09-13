package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactSensitiveRedactsSecretKeys(t *testing.T) {
	args := json.RawMessage(`{
		"username": "root",
		"password": "hunter2",
		"CIPASSWORD": "ci-hunter2",
		"api_key": "sk-123",
		"apiKey": "sk-456",
		"token": "tok-789",
		"secret": "shh",
		"nested": {
			"Secret": ["a", "b"],
			"sshPublicKey": "ssh-ed25519 AAAAC3...",
			"deep": {"TOKEN": "leak"}
		}
	}`)

	var parsed any
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(redactSensitive(parsed))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := string(out)
	for _, banned := range []string{"hunter2", "ci-hunter2", "sk-123", "sk-456", "tok-789", "shh", "leak"} {
		if strings.Contains(got, banned) {
			t.Errorf("redacted output still contains %q:\n%s", banned, got)
		}
	}
	for _, want := range []string{`"username":"root"`, `"sshPublicKey":"ssh-ed25519 AAAAC3..."`, `"[redacted]"`, `"deep":{"TOKEN":"[redacted]"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("redacted output missing %q:\n%s", want, got)
		}
	}
}

func TestRedactToolArgsPreservesNonSensitiveArgs(t *testing.T) {
	args := json.RawMessage(`{"vmid":100,"node":"pve1","sshPublicKey":"ssh-ed25519 AAA"}`)
	got := string(redactToolArgs(args))
	if got != `{"node":"pve1","sshPublicKey":"ssh-ed25519 AAA","vmid":100}` {
		t.Fatalf("redactToolArgs() = %q, want the input with only key order normalized", got)
	}
}

func TestRedactToolArgsHandlesNonObjectInput(t *testing.T) {
	if got, want := string(redactToolArgs(json.RawMessage(`[1,2,3]`))), `[1,2,3]`; got != want {
		t.Fatalf("array args = %q, want %q", got, want)
	}
	// Not JSON at all — returned unchanged rather than dropped.
	raw := json.RawMessage(`not json`)
	if got := string(redactToolArgs(raw)); got != string(raw) {
		t.Fatalf("non-JSON args = %q, want %q", got, string(raw))
	}
	if got := string(redactToolArgs(nil)); got != "" {
		t.Fatalf("nil args = %q, want empty", got)
	}
}

// TestRecordToolCallPersistsRedactedArgs pins the recordToolCall side of the
// redaction contract: the ai_tool_calls row never carries a raw credential.
func TestRecordToolCallPersistsRedactedArgs(t *testing.T) {
	e := newTestEnv(t)
	e.loginAs(t, "admin", "admin@example.com", "correct horse battery", true)
	var userID string
	if err := e.db.QueryRow(`SELECT id FROM users WHERE username = 'admin'`).Scan(&userID); err != nil {
		t.Fatalf("finding admin user: %v", err)
	}

	e.server.recordToolCall(t.Context(), userID, "chat", "guest_agent_set_password",
		json.RawMessage(`{"node":"pve1","vmid":100,"cipassword":"super-secret"}`), true, "")

	var args string
	if err := e.db.QueryRow(`SELECT args FROM ai_tool_calls WHERE tool = 'guest_agent_set_password'`).Scan(&args); err != nil {
		t.Fatalf("reading tool call row: %v", err)
	}
	if strings.Contains(args, "super-secret") {
		t.Fatalf("ai_tool_calls row contains the raw password: %q", args)
	}
	if !strings.Contains(args, "[redacted]") {
		t.Fatalf("ai_tool_calls row missing the redaction marker: %q", args)
	}
}

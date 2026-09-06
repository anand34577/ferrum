package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"ferrum/internal/auth"
	"ferrum/internal/config"
	"ferrum/internal/connections"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	db, err := store.Open(config.DBConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	box, err := secrets.New("test-secret-that-is-long-enough")
	if err != nil {
		t.Fatalf("secrets.New: %v", err)
	}
	resolver := connections.New(db, box)
	return New(resolver, db, nil, nil)
}

func TestHandleInvalidJSON(t *testing.T) {
	s := newTestServer(t)
	resp := s.Handle(context.Background(), nil, []byte("not json"))
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("expected parse error, got %+v", resp)
	}
}

func TestHandleWrongJSONRPCVersion(t *testing.T) {
	s := newTestServer(t)
	resp := s.Handle(context.Background(), nil, []byte(`{"jsonrpc":"1.0","id":1,"method":"ping"}`))
	if resp.Error == nil || resp.Error.Code != -32600 {
		t.Fatalf("expected invalid request error, got %+v", resp)
	}
}

func TestHandleUnknownMethod(t *testing.T) {
	s := newTestServer(t)
	resp := s.Handle(context.Background(), nil, []byte(`{"jsonrpc":"2.0","id":1,"method":"nope"}`))
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected method not found, got %+v", resp)
	}
}

func TestHandleInitialize(t *testing.T) {
	s := newTestServer(t)
	resp := s.Handle(context.Background(), nil, []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	res, ok := resp.Result.(initializeResult)
	if !ok {
		t.Fatalf("expected initializeResult, got %T", resp.Result)
	}
	if res.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %q, want %q", res.ProtocolVersion, ProtocolVersion)
	}
}

func TestHandleNotificationHasNoResponseBody(t *testing.T) {
	s := newTestServer(t)
	resp := s.Handle(context.Background(), nil, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if resp.ID != nil || resp.Result != nil || resp.Error != nil {
		t.Fatalf("expected empty notification response, got %+v", resp)
	}
}

func TestHandleToolsList(t *testing.T) {
	s := newTestServer(t)
	resp := s.Handle(context.Background(), nil, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	res, ok := resp.Result.(toolsListResult)
	if !ok {
		t.Fatalf("expected toolsListResult, got %T", resp.Result)
	}
	if len(res.Tools) != len(toolDefinitions) {
		t.Fatalf("got %d tools, want %d", len(res.Tools), len(toolDefinitions))
	}
	// Every tool advertised must actually have a handler wired up, or a
	// client would see it in tools/list and then get "unknown tool" calling it.
	for _, tool := range res.Tools {
		if _, ok := toolHandlers[tool.Name]; !ok {
			t.Errorf("tool %q listed but has no handler", tool.Name)
		}
	}
}

func TestListConnectionsEmpty(t *testing.T) {
	s := newTestServer(t)
	params, _ := json.Marshal(toolCallParams{Name: "list_connections"})
	resp := s.Handle(context.Background(), &auth.User{ID: "u1"}, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(toolCallResult)
	if !ok {
		t.Fatalf("expected toolCallResult, got %T", resp.Result)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
}

func TestUnknownToolCall(t *testing.T) {
	s := newTestServer(t)
	params, _ := json.Marshal(toolCallParams{Name: "does_not_exist"})
	resp := s.Handle(context.Background(), &auth.User{ID: "u1"}, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`))
	result, ok := resp.Result.(toolCallResult)
	if !ok || !result.IsError {
		t.Fatalf("expected an error tool result, got %+v", resp)
	}
}

// toolGuestPowerAction is the one mutating, state-changing tool exposed over
// MCP — it must refuse a non-admin caller before ever touching a connection,
// the same rule the equivalent REST endpoint enforces (requireAdminForMutations).
func TestGuestPowerActionRequiresAdmin(t *testing.T) {
	s := newTestServer(t)
	args, _ := json.Marshal(powerActionArgs{ConnectionID: "c1", Node: "n1", Type: "qemu", VMID: 100, Action: "start"})

	cases := []struct {
		name string
		user *auth.User
	}{
		{"nil user", nil},
		{"non-admin user", &auth.User{ID: "u1", IsAdmin: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := toolGuestPowerAction(context.Background(), s, tc.user, args)
			if !result.IsError {
				t.Fatalf("expected admin-required error, got %+v", result)
			}
		})
	}
}

func TestGuestPowerActionRejectsUnknownAction(t *testing.T) {
	s := newTestServer(t)
	args, _ := json.Marshal(powerActionArgs{ConnectionID: "c1", Node: "n1", Type: "qemu", VMID: 100, Action: "explode"})
	result := toolGuestPowerAction(context.Background(), s, &auth.User{ID: "admin", IsAdmin: true}, args)
	if !result.IsError {
		t.Fatalf("expected invalid-action error, got %+v", result)
	}
}

func TestGuestPowerActionRejectsMissingConnection(t *testing.T) {
	s := newTestServer(t)
	// A well-formed, admin-authorized request against a connection ID that
	// doesn't exist must fail cleanly through resolver.ClientFor, not panic.
	args, _ := json.Marshal(powerActionArgs{ConnectionID: "does-not-exist", Node: "n1", Type: "qemu", VMID: 100, Action: "start"})
	result := toolGuestPowerAction(context.Background(), s, &auth.User{ID: "admin", IsAdmin: true}, args)
	if !result.IsError {
		t.Fatalf("expected an error for an unknown connection, got %+v", result)
	}
}

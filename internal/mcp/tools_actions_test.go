package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"ferrum/internal/auth"
)

// validSchemaPropTypes are the JSON-schema type strings buildSchema (and
// tools.go's schemaRequired/withIntProps) may ever emit. Needle's --tools
// file is generated straight from this schema with no further validation,
// so a typo here (e.g. "object" for what's actually a JSON array) reaches a
// real LLM's tool call instead of failing anywhere in Go — this test is the
// only thing that catches it.
var validSchemaPropTypes = map[string]bool{
	"string": true, "integer": true, "boolean": true, "object": true, "array": true,
}

// TestToolSchemasUseValidJSONSchemaTypes walks every registered tool's
// InputSchema and checks each property's declared "type" is one JSON schema
// actually defines — added after create_zfs_pool's "devices" (a JSON array)
// was mistakenly declared "object".
func TestToolSchemasUseValidJSONSchemaTypes(t *testing.T) {
	for _, tool := range toolDefinitions {
		props, _ := tool.InputSchema["properties"].(map[string]any)
		for propName, raw := range props {
			def, ok := raw.(map[string]string)
			if !ok {
				t.Errorf("tool %q property %q: schema entry is %T, want map[string]string", tool.Name, propName, raw)
				continue
			}
			if !validSchemaPropTypes[def["type"]] {
				t.Errorf("tool %q property %q: invalid JSON-schema type %q", tool.Name, propName, def["type"])
			}
		}
	}
}

// readOnlyTools are the tool_actions.go/tools.go handlers that don't mutate
// anything and so must NOT require admin — every other registered tool is
// assumed mutating and checked by TestMutatingToolsRequireAdmin below. Keep
// this list in sync when a new read-only tool is added; a mutating tool
// forgetting its admin check fails loudly here instead of only at review time.
var readOnlyTools = map[string]bool{
	"list_connections": true, "list_nodes": true, "get_node_status": true,
	"list_guests": true, "get_guest_status": true, "list_alerts": true,
	"cluster_status": true, "list_storage": true, "list_pools": true,
	"list_snapshots": true,
}

// TestMutatingToolsRequireAdmin guards the rule this whole tool catalog
// depends on: every tool that isn't in readOnlyTools must refuse a
// non-admin (and nil) user before doing anything else — same as the
// equivalent REST endpoints' requireAdminForMutations. Runs generically over
// toolHandlers so a newly added mutating tool that forgets the check fails
// this test instead of shipping a privilege-escalation bug.
func TestMutatingToolsRequireAdmin(t *testing.T) {
	s := newTestServer(t)
	for name, fn := range toolHandlers {
		if readOnlyTools[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			for _, user := range []*auth.User{nil, {ID: "u1", IsAdmin: false}} {
				result := fn(context.Background(), s, user, json.RawMessage("{}"))
				if !result.IsError {
					t.Fatalf("tool %q did not reject non-admin user %+v", name, user)
				}
			}
		})
	}
}

// TestNewToolsRejectMissingConnection exercises each newly added mutating
// tool end-to-end (admin check passes, argument validation passes, then
// resolver.ClientFor fails cleanly for an unknown connection) — catching
// wiring mistakes (wrong pve.Client method, mismatched arg names) that a
// pure admin-check test wouldn't.
func TestNewToolsRejectMissingConnection(t *testing.T) {
	s := newTestServer(t)
	admin := &auth.User{ID: "admin", IsAdmin: true}
	base := map[string]any{
		"connectionId": "does-not-exist", "node": "n1", "type": "qemu", "vmid": 100,
		"name": "x", "cores": 2, "memoryMb": 512, "template": "local:vztmpl/x.tar.zst",
		"newId": 101, "snapshotName": "snap1", "service": "pveproxy", "action": "start",
		"direction": "in", "cidr": "10.0.0.0/24", "setName": "set1", "groupName": "grp1",
		"jobId": "job1", "storage": "local", "sid": "vm:100", "group": "grp1", "nodes": "n1,n2",
		"id": "id1", "options": map[string]string{"k": "v"}, "vnet": "vnet1", "zone": "zone1",
		"devpath": "/dev/sdb", "devices": []string{"/dev/sdb"}, "raidLevel": "single",
		"guest": 100, "target": "n2", "schedule": "sat 02:00", "enable": true, "enabled": true,
	}
	args, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for name, fn := range toolHandlers {
		if readOnlyTools[name] || name == "guest_power_action" {
			continue // covered by their own existing tests
		}
		t.Run(name, func(t *testing.T) {
			result := fn(context.Background(), s, admin, args)
			if !result.IsError {
				t.Fatalf("tool %q against an unknown connection should fail, got %+v", name, result)
			}
			if len(result.Content) == 0 {
				t.Fatalf("tool %q returned no error content", name)
			}
		})
	}
}

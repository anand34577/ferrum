package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ferrum/internal/auth"
	"ferrum/internal/connections"
	"ferrum/internal/pve"
)

type toolFunc func(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult

var allowedPowerActions = map[string]bool{
	"start": true, "stop": true, "shutdown": true, "reset": true, "suspend": true, "resume": true,
}

var toolDefinitions = []Tool{
	{
		Name:        "list_connections",
		Description: "List every Proxmox VE connection (cluster/host) Ferrum manages.",
		InputSchema: schemaRequired(nil),
	},
	{
		Name:        "list_nodes",
		Description: "List the nodes in a connection, with basic status (CPU, memory, uptime).",
		InputSchema: schemaRequired(map[string]string{"connectionId": "The connection ID from list_connections."}, "connectionId"),
	},
	{
		Name:        "get_node_status",
		Description: "Get detailed status for one node (CPU, memory, disk, load average, kernel/PVE version, uptime).",
		InputSchema: schemaRequired(map[string]string{
			"connectionId": "The connection ID from list_connections.",
			"node":         "The node name.",
		}, "connectionId", "node"),
	},
	{
		Name:        "list_guests",
		Description: "List every VM and container in a connection, with status, node, and resource usage.",
		InputSchema: schemaRequired(map[string]string{"connectionId": "The connection ID from list_connections."}, "connectionId"),
	},
	{
		Name:        "get_guest_status",
		Description: "Get detailed status for one VM/container.",
		InputSchema: withIntProps(schemaRequired(map[string]string{
			"connectionId": "The connection ID from list_connections.",
			"node":         "The node the guest lives on.",
			"type":         "\"qemu\" or \"lxc\".",
			"vmid":         "The guest's numeric VMID.",
		}, "connectionId", "node", "type", "vmid"), "vmid"),
	},
	{
		Name:        "guest_power_action",
		Description: "Start, stop, shut down, reset, suspend, or resume a VM/container. Requires an admin API key.",
		InputSchema: withIntProps(schemaRequired(map[string]string{
			"connectionId": "The connection ID from list_connections.",
			"node":         "The node the guest lives on.",
			"type":         "\"qemu\" or \"lxc\".",
			"vmid":         "The guest's numeric VMID.",
			"action":       "One of: start, stop, shutdown, reset, suspend, resume.",
		}, "connectionId", "node", "type", "vmid", "action"), "vmid"),
	},
	{
		Name:        "list_alerts",
		Description: "List currently active alert instances across every connection.",
		InputSchema: schemaRequired(nil),
	},
	{
		Name:        "cluster_status",
		Description: "Get cluster membership and quorum status for a connection (single-node setups return a minimal one-node view).",
		InputSchema: schemaRequired(map[string]string{"connectionId": "The connection ID from list_connections."}, "connectionId"),
	},
	{
		Name:        "list_storage",
		Description: "List storage pools in a connection, with type and usage.",
		InputSchema: schemaRequired(map[string]string{"connectionId": "The connection ID from list_connections."}, "connectionId"),
	},
	{
		Name:        "list_pools",
		Description: "List resource pools in a connection.",
		InputSchema: schemaRequired(map[string]string{"connectionId": "The connection ID from list_connections."}, "connectionId"),
	},
}

func schemaRequired(props map[string]string, required ...string) map[string]any {
	p := map[string]any{}
	for name, desc := range props {
		p[name] = map[string]string{"type": "string", "description": desc}
	}
	s := map[string]any{"type": "object", "properties": p}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// withIntProps re-types the given property names from "string" to "integer"
// on an already-built schema — used for fields like vmid that are numeric in
// the Go struct (json.Unmarshal rejects a JSON string into an int field), so
// a spec-following caller sends the right JSON type instead of one that
// fails to unmarshal.
func withIntProps(schema map[string]any, names ...string) map[string]any {
	props, _ := schema["properties"].(map[string]any)
	for _, name := range names {
		if prop, ok := props[name].(map[string]string); ok {
			props[name] = map[string]string{"type": "integer", "description": prop["description"]}
		}
	}
	return schema
}

var toolHandlers = map[string]toolFunc{
	"list_connections":   toolListConnections,
	"list_nodes":         toolListNodes,
	"get_node_status":    toolGetNodeStatus,
	"list_guests":        toolListGuests,
	"get_guest_status":   toolGetGuestStatus,
	"guest_power_action": toolGuestPowerAction,
	"list_alerts":        toolListAlerts,
	"cluster_status":     toolClusterStatus,
	"list_storage":       toolListStorage,
	"list_pools":         toolListPools,
}

func toolClusterStatus(ctx context.Context, s *Server, _ *auth.User, args json.RawMessage) toolCallResult {
	var a connectionIDArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" {
		return errorResult("connectionId is required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	status, err := client.ClusterStatus(ctx)
	if err != nil {
		return errorResult(err.Error())
	}
	return jsonResult(status)
}

func toolListStorage(ctx context.Context, s *Server, _ *auth.User, args json.RawMessage) toolCallResult {
	var a connectionIDArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" {
		return errorResult("connectionId is required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	resources, err := client.ClusterResources(ctx)
	if err != nil {
		return errorResult(err.Error())
	}
	return jsonResult(filterResources(resources, "storage"))
}

func toolListPools(ctx context.Context, s *Server, _ *auth.User, args json.RawMessage) toolCallResult {
	var a connectionIDArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" {
		return errorResult("connectionId is required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	resources, err := client.ClusterResources(ctx)
	if err != nil {
		return errorResult(err.Error())
	}
	return jsonResult(filterResources(resources, "pool"))
}

func toolListConnections(ctx context.Context, s *Server, _ *auth.User, _ json.RawMessage) toolCallResult {
	list, err := s.resolver.List(ctx)
	if err != nil {
		return errorResult(err.Error())
	}
	if list == nil {
		list = []connections.Info{}
	}
	return jsonResult(list)
}

type connectionIDArgs struct {
	ConnectionID string `json:"connectionId"`
}

func filterResources(resources []pve.ClusterResource, types ...string) []pve.ClusterResource {
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	out := make([]pve.ClusterResource, 0, len(resources))
	for _, res := range resources {
		if want[res.Type] {
			out = append(out, res)
		}
	}
	return out
}

func toolListNodes(ctx context.Context, s *Server, _ *auth.User, args json.RawMessage) toolCallResult {
	var a connectionIDArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" {
		return errorResult("connectionId is required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	resources, err := client.ClusterResources(ctx)
	if err != nil {
		return errorResult(err.Error())
	}
	return jsonResult(filterResources(resources, "node"))
}

func toolListGuests(ctx context.Context, s *Server, _ *auth.User, args json.RawMessage) toolCallResult {
	var a connectionIDArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" {
		return errorResult("connectionId is required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	resources, err := client.ClusterResources(ctx)
	if err != nil {
		return errorResult(err.Error())
	}
	return jsonResult(filterResources(resources, "qemu", "lxc"))
}

type nodeArgs struct {
	ConnectionID string `json:"connectionId"`
	Node         string `json:"node"`
}

func toolGetNodeStatus(ctx context.Context, s *Server, _ *auth.User, args json.RawMessage) toolCallResult {
	var a nodeArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" {
		return errorResult("connectionId and node are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	status, err := client.NodeStatus(ctx, a.Node)
	if err != nil {
		return errorResult(err.Error())
	}
	return jsonResult(status)
}

type guestArgs struct {
	ConnectionID string `json:"connectionId"`
	Node         string `json:"node"`
	Type         string `json:"type"`
	VMID         int    `json:"vmid"`
}

func toolGetGuestStatus(ctx context.Context, s *Server, _ *auth.User, args json.RawMessage) toolCallResult {
	var a guestArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 {
		return errorResult("connectionId, node, type, and vmid are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	resources, err := client.ClusterResources(ctx)
	if err != nil {
		return errorResult(err.Error())
	}
	for _, res := range resources {
		if res.Type == a.Type && res.Node == a.Node && res.VMID == a.VMID {
			return jsonResult(res)
		}
	}
	return errorResult("guest not found")
}

type powerActionArgs struct {
	ConnectionID string `json:"connectionId"`
	Node         string `json:"node"`
	Type         string `json:"type"`
	VMID         int    `json:"vmid"`
	Action       string `json:"action"`
}

// toolGuestPowerAction is the one mutating tool exposed over MCP, so it
// enforces the same admin-only rule requireAdminForMutations applies to the
// equivalent REST endpoint — an API key minted by a non-admin user can read
// the fleet through MCP but not act on it.
func toolGuestPowerAction(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if user == nil || !user.IsAdmin {
		return errorResult("admin access required for this operation")
	}
	var a powerActionArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 || a.Action == "" {
		return errorResult("connectionId, node, type, vmid, and action are required")
	}
	if !allowedPowerActions[a.Action] {
		return errorResult("action must be one of: start, stop, shutdown, reset, suspend, resume")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.GuestPowerAction(ctx, a.Type, a.Node, a.VMID, a.Action)
	if err != nil {
		return errorResult(err.Error())
	}
	if s.audit != nil {
		s.audit(ctx, user.ID, "vm."+a.Action, "vm", fmt.Sprintf("%s/%s/%d (via MCP)", a.Node, a.Type, a.VMID))
	}
	return textResult(fmt.Sprintf("action %q submitted for %s/%s/%d — task %s", a.Action, a.Node, a.Type, a.VMID, upid))
}

type mcpAlert struct {
	ID             string  `json:"id"`
	ConnectionName string  `json:"connectionName"`
	ResourceName   string  `json:"resourceName"`
	Metric         string  `json:"metric"`
	Value          float64 `json:"value"`
	Threshold      float64 `json:"threshold"`
	Severity       string  `json:"severity"`
	TriggeredAt    string  `json:"triggeredAt"`
}

func toolListAlerts(ctx context.Context, s *Server, _ *auth.User, _ json.RawMessage) toolCallResult {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, connection_name, resource_name, metric, value, threshold, severity, triggered_at
		FROM alert_instances WHERE status = 'active' ORDER BY triggered_at DESC LIMIT 200`)
	if err != nil {
		return errorResult(err.Error())
	}
	defer rows.Close()

	out := []mcpAlert{}
	for rows.Next() {
		var a mcpAlert
		if err := rows.Scan(&a.ID, &a.ConnectionName, &a.ResourceName, &a.Metric, &a.Value, &a.Threshold, &a.Severity, &a.TriggeredAt); err != nil {
			return errorResult(err.Error())
		}
		out = append(out, a)
	}
	return jsonResult(out)
}

// ToolDefinitions exposes the tool catalog (name, description, JSON schema)
// for callers that need to build their own tool listing — the AI
// Assistant's function-calling loop (internal/api/ai_chat.go) advertises
// these same tools to the LLM instead of maintaining a second definition.
func ToolDefinitions() []Tool {
	return toolDefinitions
}

// CallTool executes one tool by name and flattens the result to plain text
// plus an error flag — used by the AI Assistant's function-calling loop
// (internal/api/ai_chat.go), so there is exactly one implementation of what
// each tool does and who's allowed to call it, shared with the MCP JSON-RPC
// dispatcher (Handle). source identifies the caller ("chat") for the
// activity trail — see RecordFunc.
func (s *Server) CallTool(ctx context.Context, user *auth.User, source, name string, args json.RawMessage) (text string, isError bool) {
	result := s.callTool(ctx, user, source, name, args)
	var sb strings.Builder
	for _, c := range result.Content {
		sb.WriteString(c.Text)
	}
	return sb.String(), result.IsError
}

func jsonResult(v any) toolCallResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(err.Error())
	}
	return textResult(string(b))
}

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"ferrum/internal/auth"
	"ferrum/internal/pve"
)

// This file adds every mutating tool that isn't a plain delete — guest
// lifecycle, node operations, firewall/security, and backup/replication/HA/
// storage/SDN management — to the catalog tools.go started with read-only
// lookups plus guest_power_action. Every tool here follows the same shape
// that established: a thin wrapper around an existing pve.Client method (no
// new business logic), admin-gated the same way guest_power_action already
// is, audited on success, and registered into the shared toolDefinitions/
// toolHandlers below via init().
//
// Deliberately left out (tracked as a follow-up, not an oversight): actions
// that are just as destructive as a delete even though they aren't one —
// migrate/resize/move-disk on a running guest, reboot/shutdown a node,
// wiping a disk, revoking a certificate, and removing a node from the
// cluster. node_service_action is included but restricted to start/restart/
// reload — "stop" is excluded for the same reason.

// requireAdmin is the guest_power_action admin check, repeated rather than
// abstracted — every mutating handler below needs the exact same two lines,
// and they're already the established pattern in this package.
func requireAdmin(user *auth.User) (toolCallResult, bool) {
	if user == nil || !user.IsAdmin {
		return errorResult("admin access required for this operation"), false
	}
	return toolCallResult{}, true
}

// prop is one JSON-schema property for buildSchema — tools.go's
// schemaRequired only ever builds all-string schemas; the tools below need a
// mix of string/integer/boolean/object properties.
type prop struct {
	Type        string
	Description string
}

func buildSchema(props map[string]prop, required ...string) map[string]any {
	p := map[string]any{}
	for name, def := range props {
		p[name] = map[string]string{"type": def.Type, "description": def.Description}
	}
	s := map[string]any{"type": "object", "properties": p}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

const (
	descConnID = "The connection ID from list_connections."
	descNode   = "The node name."
	descGType  = "\"qemu\" or \"lxc\"."
	descVMID   = "The guest's numeric VMID."
)

// --- Guest lifecycle ---

type createVMArgs struct {
	ConnectionID string `json:"connectionId"`
	Node         string `json:"node"`
	VMID         int    `json:"vmid,omitempty"` // 0 = auto-assign via NextID
	Name         string `json:"name,omitempty"`
	Cores        int    `json:"cores"`
	MemoryMB     int    `json:"memoryMb"`
	Storage      string `json:"storage,omitempty"`
	DiskGB       int    `json:"diskGb,omitempty"`
	ISO          string `json:"iso,omitempty"`
	Bridge       string `json:"bridge,omitempty"`
	CIUser       string `json:"ciUser,omitempty"`
	CIPassword   string `json:"ciPassword,omitempty"`
	SSHPublicKey string `json:"sshPublicKey,omitempty"`
	IPConfig     string `json:"ipConfig,omitempty"`
}

func toolCreateVM(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createVMArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Cores <= 0 || a.MemoryMB <= 0 {
		return errorResult("connectionId, node, cores, and memoryMb are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if a.VMID == 0 {
		if a.VMID, err = client.NextID(ctx); err != nil {
			return errorResult(err.Error())
		}
	}
	upid, err := client.CreateVM(ctx, pve.CreateVMOptions{
		VMID: a.VMID, Name: a.Name, Node: a.Node, Cores: a.Cores, Memory: a.MemoryMB,
		Storage: a.Storage, DiskGB: a.DiskGB, ISO: a.ISO, Bridge: a.Bridge,
		CIUser: a.CIUser, CIPassword: a.CIPassword, SSHPublicKey: a.SSHPublicKey, IPConfig: a.IPConfig,
	})
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "vm.create", "vm", fmt.Sprintf("%s/qemu/%d", a.Node, a.VMID))
	return textResult(fmt.Sprintf("VM %d created on %s — task %s", a.VMID, a.Node, upid))
}

type createLXCArgs struct {
	ConnectionID string `json:"connectionId"`
	Node         string `json:"node"`
	VMID         int    `json:"vmid,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	Cores        int    `json:"cores"`
	MemoryMB     int    `json:"memoryMb"`
	Storage      string `json:"storage,omitempty"`
	DiskGB       int    `json:"diskGb,omitempty"`
	Template     string `json:"template"`
	Bridge       string `json:"bridge,omitempty"`
	Password     string `json:"password,omitempty"`
	SSHPublicKey string `json:"sshPublicKey,omitempty"`
	IPConfig     string `json:"ipConfig,omitempty"`
	Unprivileged bool   `json:"unprivileged,omitempty"`
}

func toolCreateLXC(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createLXCArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Cores <= 0 || a.MemoryMB <= 0 || a.Template == "" {
		return errorResult("connectionId, node, cores, memoryMb, and template are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if a.VMID == 0 {
		if a.VMID, err = client.NextID(ctx); err != nil {
			return errorResult(err.Error())
		}
	}
	upid, err := client.CreateLXC(ctx, pve.CreateLXCOptions{
		VMID: a.VMID, Hostname: a.Hostname, Node: a.Node, Cores: a.Cores, Memory: a.MemoryMB,
		Storage: a.Storage, DiskGB: a.DiskGB, Template: a.Template, Bridge: a.Bridge,
		Password: a.Password, SSHPublicKey: a.SSHPublicKey, IPConfig: a.IPConfig, Unprivileged: a.Unprivileged,
	})
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "lxc.create", "vm", fmt.Sprintf("%s/lxc/%d", a.Node, a.VMID))
	return textResult(fmt.Sprintf("container %d created on %s — task %s", a.VMID, a.Node, upid))
}

type cloneGuestArgs struct {
	guestArgs
	NewID       int    `json:"newId"`
	Name        string `json:"name,omitempty"`
	TargetNode  string `json:"targetNode,omitempty"`
	Full        bool   `json:"full,omitempty"`
	Storage     string `json:"storage,omitempty"`
	Description string `json:"description,omitempty"`
}

func toolCloneGuest(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a cloneGuestArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 || a.NewID == 0 {
		return errorResult("connectionId, node, type, vmid, and newId are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.CloneGuest(ctx, a.Type, a.Node, a.VMID, pve.CloneOptions{
		NewID: a.NewID, Name: a.Name, TargetNode: a.TargetNode, Full: a.Full, Storage: a.Storage, Description: a.Description,
	})
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "vm.clone", "vm", fmt.Sprintf("%s/%s/%d", a.Node, a.Type, a.VMID))
	return textResult(fmt.Sprintf("clone of %s/%d to new VMID %d started — task %s", a.Type, a.VMID, a.NewID, upid))
}

func toolListSnapshots(ctx context.Context, s *Server, _ *auth.User, args json.RawMessage) toolCallResult {
	var a guestArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 {
		return errorResult("connectionId, node, type, and vmid are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	snaps, err := client.ListSnapshots(ctx, a.Type, a.Node, a.VMID)
	if err != nil {
		return errorResult(err.Error())
	}
	return jsonResult(snaps)
}

type createSnapshotArgs struct {
	guestArgs
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	IncludeState bool   `json:"includeState,omitempty"`
}

func toolCreateSnapshot(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createSnapshotArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 || a.Name == "" {
		return errorResult("connectionId, node, type, vmid, and name are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.CreateSnapshot(ctx, a.Type, a.Node, a.VMID, a.Name, a.Description, a.IncludeState)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "vm.snapshot.create", "vm", fmt.Sprintf("%s/%s/%d", a.Node, a.Type, a.VMID))
	return textResult(fmt.Sprintf("snapshot %q created — task %s", a.Name, upid))
}

type rollbackSnapshotArgs struct {
	guestArgs
	SnapshotName string `json:"snapshotName"`
}

func toolRollbackSnapshot(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a rollbackSnapshotArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 || a.SnapshotName == "" {
		return errorResult("connectionId, node, type, vmid, and snapshotName are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.RollbackSnapshot(ctx, a.Type, a.Node, a.VMID, a.SnapshotName)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "vm.snapshot.rollback", "vm", fmt.Sprintf("%s/%s/%d", a.Node, a.Type, a.VMID))
	return textResult(fmt.Sprintf("rolled back to snapshot %q — task %s", a.SnapshotName, upid))
}

func toolUnlockGuest(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a guestArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 {
		return errorResult("connectionId, node, type, and vmid are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.UnlockGuest(ctx, a.Type, a.Node, a.VMID); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "vm.unlock", "vm", fmt.Sprintf("%s/%s/%d", a.Node, a.Type, a.VMID))
	return textResult("unlocked")
}

func toolSetGuestTemplate(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a guestArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 {
		return errorResult("connectionId, node, type, and vmid are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.SetTemplate(ctx, a.Type, a.Node, a.VMID); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "vm.convert-template", "vm", fmt.Sprintf("%s/%s/%d", a.Node, a.Type, a.VMID))
	return textResult("converted to template")
}

// --- Node operations ---

func toolWakeOnLan(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a nodeArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" {
		return errorResult("connectionId and node are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	result, err := client.WakeOnLan(ctx, a.Node)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "node.wakeonlan", "node", a.Node)
	return textResult(result)
}

func toolStartAllGuests(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a nodeArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" {
		return errorResult("connectionId and node are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.StartAllGuests(ctx, a.Node)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "node.startall", "node", a.Node)
	return textResult("starting all guests on " + a.Node + " — task " + upid)
}

func toolStopAllGuests(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a nodeArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" {
		return errorResult("connectionId and node are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.StopAllGuests(ctx, a.Node)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "node.stopall", "node", a.Node)
	return textResult("stopping all guests on " + a.Node + " — task " + upid)
}

// allowedNodeServiceActions excludes "stop" — restarting/reloading a system
// service is recoverable within seconds; stopping one (pveproxy, pvedaemon,
// corosync, ...) can strand the node the same way shutdownNode would, which
// is exactly the class of action this tool catalog leaves out.
var allowedNodeServiceActions = map[string]bool{"start": true, "restart": true, "reload": true}

type nodeServiceArgs struct {
	nodeArgs
	Service string `json:"service"`
	Action  string `json:"action"`
}

func toolNodeServiceAction(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a nodeServiceArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Service == "" || a.Action == "" {
		return errorResult("connectionId, node, service, and action are required")
	}
	if !allowedNodeServiceActions[a.Action] {
		return errorResult("action must be one of: start, restart, reload (stop is not exposed here)")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.NodeServiceAction(ctx, a.Node, a.Service, a.Action); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "node.service."+a.Action, "node", a.Node+"/"+a.Service)
	return textResult(fmt.Sprintf("%s %sed on %s", a.Service, a.Action, a.Node))
}

func toolAptRefresh(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a nodeArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" {
		return errorResult("connectionId and node are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.RefreshAptIndex(ctx, a.Node)
	if err != nil {
		return errorResult(err.Error())
	}
	return textResult("package index refresh started — task " + upid)
}

func toolAptUpgrade(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a nodeArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" {
		return errorResult("connectionId and node are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.UpgradeNode(ctx, a.Node)
	if err != nil {
		if errors.Is(err, pve.ErrUpgradeNotSupported) {
			return errorResult("package upgrade is not supported on this node/platform")
		}
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "node.upgrade", "node", a.Node)
	return textResult("package upgrade started on " + a.Node + " — task " + upid)
}

// --- Firewall & security ---
//
// The rule's own direction ("in"/"out") is exposed as "direction" rather
// than "type" — guest-scoped tools already use "type" for the guest kind
// (qemu/lxc), and the two would collide in one flat argument object.

type firewallRuleArgs struct {
	Direction string `json:"direction"`
	Action    string `json:"action"`
	Source    string `json:"source,omitempty"`
	Dest      string `json:"dest,omitempty"`
	Proto     string `json:"proto,omitempty"`
	Dport     string `json:"dport,omitempty"`
	Sport     string `json:"sport,omitempty"`
	Macro     string `json:"macro,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Enable    bool   `json:"enable,omitempty"`
}

func (a firewallRuleArgs) toRule() pve.NewFirewallRule {
	return pve.NewFirewallRule{
		Type: a.Direction, Action: a.Action, Source: a.Source, Dest: a.Dest,
		Proto: a.Proto, Dport: a.Dport, Sport: a.Sport, Macro: a.Macro, Comment: a.Comment, Enable: a.Enable,
	}
}

type addGuestFirewallRuleArgs struct {
	guestArgs
	firewallRuleArgs
}

func toolAddGuestFirewallRule(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a addGuestFirewallRuleArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 || a.Direction == "" || a.Action == "" {
		return errorResult("connectionId, node, type, vmid, direction, and action are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.AddGuestFirewallRule(ctx, a.Type, a.Node, a.VMID, a.toRule()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.rule.create", "vm", fmt.Sprintf("%s/%s/%d", a.Node, a.Type, a.VMID))
	return textResult("firewall rule added")
}

type addNodeFirewallRuleArgs struct {
	nodeArgs
	firewallRuleArgs
}

func toolAddNodeFirewallRule(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a addNodeFirewallRuleArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Direction == "" || a.Action == "" {
		return errorResult("connectionId, node, direction, and action are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.AddNodeFirewallRule(ctx, a.Node, a.toRule()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.rule.create", "firewall", "node:"+a.Node)
	return textResult("firewall rule added")
}

type addClusterFirewallRuleArgs struct {
	ConnectionID string `json:"connectionId"`
	firewallRuleArgs
}

func toolAddClusterFirewallRule(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a addClusterFirewallRuleArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Direction == "" || a.Action == "" {
		return errorResult("connectionId, direction, and action are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.AddClusterFirewallRule(ctx, a.toRule()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.rule.create", "firewall", a.Action+" "+a.Source+"->"+a.Dest)
	return textResult("firewall rule added")
}

type aliasArgs struct {
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`
	CIDR         string `json:"cidr"`
	Comment      string `json:"comment,omitempty"`
}

func toolAddFirewallAlias(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a aliasArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Name == "" || a.CIDR == "" {
		return errorResult("connectionId, name, and cidr are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.AddFirewallAlias(ctx, a.Name, a.CIDR, a.Comment); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.alias.create", "firewall", a.Name)
	return textResult("alias created")
}

type ipsetArgs struct {
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`
	Comment      string `json:"comment,omitempty"`
}

func toolAddFirewallIPSet(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a ipsetArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Name == "" {
		return errorResult("connectionId and name are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.AddFirewallIPSet(ctx, a.Name, a.Comment); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.ipset.create", "firewall", a.Name)
	return textResult("IP set created")
}

type ipsetEntryArgs struct {
	ConnectionID string `json:"connectionId"`
	SetName      string `json:"setName"`
	CIDR         string `json:"cidr"`
	Comment      string `json:"comment,omitempty"`
}

func toolAddFirewallIPSetEntry(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a ipsetEntryArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.SetName == "" || a.CIDR == "" {
		return errorResult("connectionId, setName, and cidr are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.AddFirewallIPSetEntry(ctx, a.SetName, a.CIDR, a.Comment); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.ipset.add-entry", "firewall", a.SetName+"/"+a.CIDR)
	return textResult("IP set entry added")
}

func toolCreateFirewallSecurityGroup(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a ipsetArgs // same shape: connectionId, name, comment
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Name == "" {
		return errorResult("connectionId and name are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateFirewallSecurityGroup(ctx, a.Name, a.Comment); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.security-group.create", "firewall", a.Name)
	return textResult("security group created")
}

type securityGroupRuleArgs struct {
	ConnectionID string `json:"connectionId"`
	GroupName    string `json:"groupName"`
	firewallRuleArgs
}

func toolAddSecurityGroupRule(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a securityGroupRuleArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.GroupName == "" || a.Direction == "" || a.Action == "" {
		return errorResult("connectionId, groupName, direction, and action are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.AddSecurityGroupRule(ctx, a.GroupName, a.toRule()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.security-group.add-rule", "firewall", a.GroupName)
	return textResult("rule added to security group")
}

type guestFirewallOptionsArgs struct {
	guestArgs
	Enable bool `json:"enable"`
}

func toolUpdateGuestFirewallOptions(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a guestFirewallOptionsArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Type == "" || a.VMID == 0 {
		return errorResult("connectionId, node, type, and vmid are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.UpdateGuestFirewallOptions(ctx, a.Type, a.Node, a.VMID, a.Enable); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.options.update", "vm", fmt.Sprintf("%s/%s/%d", a.Node, a.Type, a.VMID))
	return textResult(fmt.Sprintf("guest firewall enable=%v", a.Enable))
}

type clusterFirewallOptionsArgs struct {
	ConnectionID string `json:"connectionId"`
	Enable       bool   `json:"enable"`
}

func toolUpdateClusterFirewallOptions(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a clusterFirewallOptionsArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" {
		return errorResult("connectionId is required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.UpdateClusterFirewallOptions(ctx, a.Enable); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "firewall.options.update", "firewall", fmt.Sprintf("enable=%v", a.Enable))
	return textResult(fmt.Sprintf("cluster firewall enable=%v", a.Enable))
}

// --- Backup & replication ---

type backupJobArgs struct {
	ConnectionID string `json:"connectionId"`
	JobID        string `json:"jobId,omitempty"` // required for update, ignored for create
	Schedule     string `json:"schedule"`
	Storage      string `json:"storage"`
	VMIDs        string `json:"vmids,omitempty"` // comma-separated, empty = all guests
	Mode         string `json:"mode,omitempty"`
	Compress     string `json:"compress,omitempty"`
	Enabled      bool   `json:"enabled,omitempty"`
	Comment      string `json:"comment,omitempty"`
	Prune        int    `json:"prune,omitempty"`
}

func (a backupJobArgs) toOptions() pve.CreateBackupJobOptions {
	return pve.CreateBackupJobOptions{
		Schedule: a.Schedule, Storage: a.Storage, VMIDs: a.VMIDs, Mode: a.Mode,
		Compress: a.Compress, Enabled: a.Enabled, Comment: a.Comment, Prune: a.Prune,
	}
}

func toolCreateBackupJob(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a backupJobArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Schedule == "" || a.Storage == "" {
		return errorResult("connectionId, schedule, and storage are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateBackupJob(ctx, a.toOptions()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "backup-job.create", "backup", a.Storage)
	return textResult("backup job created")
}

func toolUpdateBackupJob(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a backupJobArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.JobID == "" {
		return errorResult("connectionId and jobId are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.UpdateBackupJob(ctx, a.JobID, a.toOptions()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "backup-job.update", "backup", a.JobID)
	return textResult("backup job updated")
}

type runBackupArgs struct {
	nodeArgs
	Storage string   `json:"storage"`
	VMIDs   []string `json:"vmids,omitempty"`
	Mode    string   `json:"mode,omitempty"`
}

func toolRunBackupNow(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a runBackupArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Storage == "" {
		return errorResult("connectionId, node, and storage are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.RunBackupNow(ctx, a.Node, a.Storage, a.VMIDs, a.Mode)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "backup.run", "backup", a.Node+"/"+a.Storage)
	return textResult("backup started — task " + upid)
}

type replicationJobArgs struct {
	ConnectionID string `json:"connectionId"`
	JobID        string `json:"jobId,omitempty"` // required for create (as "id"); ignored for update
	Guest        int    `json:"guest,omitempty"`
	Target       string `json:"target"`
	Schedule     string `json:"schedule,omitempty"`
	Comment      string `json:"comment,omitempty"`
	Disable      bool   `json:"disable,omitempty"`
}

func (a replicationJobArgs) toOptions() pve.CreateReplicationJobOptions {
	return pve.CreateReplicationJobOptions{
		ID: a.JobID, Guest: a.Guest, Target: a.Target, Schedule: a.Schedule, Comment: a.Comment, Disable: a.Disable,
	}
}

func toolCreateReplicationJob(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a replicationJobArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.JobID == "" || a.Guest == 0 || a.Target == "" {
		return errorResult("connectionId, jobId, guest, and target are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateReplicationJob(ctx, a.toOptions()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "replication-job.create", "replication", a.JobID)
	return textResult("replication job created")
}

func toolUpdateReplicationJob(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a replicationJobArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.JobID == "" {
		return errorResult("connectionId and jobId are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.UpdateReplicationJob(ctx, a.JobID, a.toOptions()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "replication-job.update", "replication", a.JobID)
	return textResult("replication job updated")
}

type runReplicationArgs struct {
	nodeArgs
	JobID string `json:"jobId"`
}

func toolRunReplicationNow(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a runReplicationArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.JobID == "" {
		return errorResult("connectionId, node, and jobId are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.ScheduleReplicationNow(ctx, a.Node, a.JobID)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "replication.run", "node", a.Node+"/"+a.JobID)
	return textResult("replication started — task " + upid)
}

// --- HA ---

type addHAResourceArgs struct {
	ConnectionID string `json:"connectionId"`
	SID          string `json:"sid"`
	Group        string `json:"group,omitempty"`
	MaxRestart   int    `json:"maxRestart,omitempty"`
	MaxRelocate  int    `json:"maxRelocate,omitempty"`
}

func toolAddHAResource(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a addHAResourceArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.SID == "" {
		return errorResult("connectionId and sid are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.AddHAResource(ctx, a.SID, a.Group, a.MaxRestart, a.MaxRelocate); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "ha.add-resource", "ha", a.SID)
	return textResult("HA resource added")
}

// updateHAResourceArgs uses pointers (unlike addHAResourceArgs) so an
// omitted field means "leave as-is" — 0 is a legitimate explicit value for
// MaxRestart/MaxRelocate, so a plain int can't distinguish "unset" from
// "set to zero"; -1 is pve.Client's own sentinel for "leave as-is" here.
type updateHAResourceArgs struct {
	ConnectionID string  `json:"connectionId"`
	SID          string  `json:"sid"`
	Group        *string `json:"group,omitempty"`
	MaxRestart   *int    `json:"maxRestart,omitempty"`
	MaxRelocate  *int    `json:"maxRelocate,omitempty"`
	Comment      *string `json:"comment,omitempty"`
}

func toolUpdateHAResource(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a updateHAResourceArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.SID == "" {
		return errorResult("connectionId and sid are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	group, maxRestart, maxRelocate, comment := "", -1, -1, ""
	if a.Group != nil {
		group = *a.Group
	}
	if a.MaxRestart != nil {
		maxRestart = *a.MaxRestart
	}
	if a.MaxRelocate != nil {
		maxRelocate = *a.MaxRelocate
	}
	if a.Comment != nil {
		comment = *a.Comment
	}
	if err := client.UpdateHAResource(ctx, a.SID, group, maxRestart, maxRelocate, comment); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "ha.update-resource", "ha", a.SID)
	return textResult("HA resource updated")
}

type createHAGroupArgs struct {
	ConnectionID string `json:"connectionId"`
	Group        string `json:"group"`
	Nodes        string `json:"nodes"`
	Restricted   bool   `json:"restricted,omitempty"`
	NoFailback   bool   `json:"nofailback,omitempty"`
	Comment      string `json:"comment,omitempty"`
}

func toolCreateHAGroup(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createHAGroupArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Group == "" || a.Nodes == "" {
		return errorResult("connectionId, group, and nodes are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateHAGroup(ctx, a.Group, a.Nodes, a.Restricted, a.NoFailback, a.Comment); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "ha.group.create", "ha", a.Group)
	return textResult("HA group created")
}

// haOptionsArgs backs update_ha_group/create_ha_rule/update_ha_rule — all
// three take a free-form string map on the PVE side (pve.Client already
// accepts map[string]string for these), so there's no fixed field list to
// declare beyond the identifier.
type haOptionsArgs struct {
	ConnectionID string            `json:"connectionId"`
	ID           string            `json:"id"` // group name, or rule id
	Options      map[string]string `json:"options"`
}

func toolUpdateHAGroup(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a haOptionsArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.ID == "" {
		return errorResult("connectionId and id (the group name) are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.UpdateHAGroup(ctx, a.ID, a.Options); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "ha.group.update", "ha", a.ID)
	return textResult("HA group updated")
}

func toolCreateHARule(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a haOptionsArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.ID == "" {
		return errorResult("connectionId and id are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateHARule(ctx, a.ID, a.Options); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "ha.rule.create", "ha", a.ID)
	return textResult("HA rule created")
}

func toolUpdateHARule(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a haOptionsArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.ID == "" {
		return errorResult("connectionId and id are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.UpdateHARule(ctx, a.ID, a.Options); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "ha.rule.update", "ha", a.ID)
	return textResult("HA rule updated")
}

// --- Storage & SDN ---

type storageArgs struct {
	ConnectionID string            `json:"connectionId"`
	Storage      string            `json:"storage"`
	Type         string            `json:"type,omitempty"` // required for create; ignored for update
	Content      string            `json:"content,omitempty"`
	Nodes        string            `json:"nodes,omitempty"`
	Shared       bool              `json:"shared,omitempty"`
	Disable      bool              `json:"disable,omitempty"`
	Extra        map[string]string `json:"extra,omitempty"`
}

func (a storageArgs) toOptions() pve.CreateStorageOptions {
	return pve.CreateStorageOptions{
		Storage: a.Storage, Type: a.Type, Content: a.Content, Nodes: a.Nodes,
		Shared: a.Shared, Disable: a.Disable, Extra: a.Extra,
	}
}

func toolCreateStorage(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a storageArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Storage == "" || a.Type == "" {
		return errorResult("connectionId, storage, and type are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateStorage(ctx, a.toOptions()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "storage.create", "storage", a.Storage)
	return textResult("storage pool created")
}

func toolUpdateStorage(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a storageArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Storage == "" {
		return errorResult("connectionId and storage are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.UpdateStorage(ctx, a.Storage, a.toOptions()); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "storage.update", "storage", a.Storage)
	return textResult("storage pool updated")
}

type sdnMapArgs struct {
	ConnectionID string            `json:"connectionId"`
	Name         string            `json:"name"`
	Options      map[string]string `json:"options,omitempty"`
}

func toolCreateSDNZone(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a sdnMapArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Name == "" {
		return errorResult("connectionId and name (the zone id) are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateSDNZone(ctx, a.Name, a.Options); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "sdn.zone.create", "sdn", a.Name)
	return textResult("SDN zone created — call apply_sdn_config to activate it")
}

type createSDNVnetArgs struct {
	ConnectionID string `json:"connectionId"`
	Vnet         string `json:"vnet"`
	Zone         string `json:"zone"`
	Alias        string `json:"alias,omitempty"`
	Tag          int    `json:"tag,omitempty"`
	VLANAware    bool   `json:"vlanAware,omitempty"`
}

func toolCreateSDNVnet(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createSDNVnetArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Vnet == "" || a.Zone == "" {
		return errorResult("connectionId, vnet, and zone are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateSDNVnet(ctx, a.Vnet, a.Zone, a.Alias, a.Tag, a.VLANAware); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "sdn.vnet.create", "sdn", a.Vnet)
	return textResult("SDN vnet created — call apply_sdn_config to activate it")
}

type createSDNSubnetArgs struct {
	ConnectionID string `json:"connectionId"`
	Vnet         string `json:"vnet"`
	CIDR         string `json:"cidr"`
	Gateway      string `json:"gateway,omitempty"`
	SNAT         bool   `json:"snat,omitempty"`
}

func toolCreateSDNSubnet(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createSDNSubnetArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Vnet == "" || a.CIDR == "" {
		return errorResult("connectionId, vnet, and cidr are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateSDNSubnet(ctx, a.Vnet, a.CIDR, a.Gateway, a.SNAT); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "sdn.subnet.create", "sdn", a.Vnet+"/"+a.CIDR)
	return textResult("SDN subnet created — call apply_sdn_config to activate it")
}

func toolCreateSDNController(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a sdnMapArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Name == "" {
		return errorResult("connectionId and name (the controller id) are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateSDNController(ctx, a.Name, a.Options); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "sdn.controller.create", "sdn", a.Name)
	return textResult("SDN controller created — call apply_sdn_config to activate it")
}

func toolCreateSDNIPAM(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a sdnMapArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Name == "" {
		return errorResult("connectionId and name (the IPAM id) are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	if err := client.CreateSDNIPAM(ctx, a.Name, a.Options); err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "sdn.ipam.create", "sdn", a.Name)
	return textResult("SDN IPAM created — call apply_sdn_config to activate it")
}

func toolApplySDNConfig(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a connectionIDArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" {
		return errorResult("connectionId is required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.ApplySDNConfig(ctx)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "sdn.apply", "sdn", "")
	return textResult("SDN configuration applied — task " + upid)
}

// diskStorageArgs backs the create_zfs_pool/create_lvm_storage/
// create_lvm_thin_storage/create_directory_storage tools — they all take the
// same node+device+name+addStorage shape (see internal/pve/disks.go); ZFS
// additionally takes a device list and RAID level instead of one devpath.
type diskStorageArgs struct {
	nodeArgs
	Name       string `json:"name"`
	AddStorage bool   `json:"addStorage,omitempty"`
}

type createDiskBackedStorageArgs struct {
	diskStorageArgs
	Devpath string `json:"devpath"`
}

func toolCreateLVMStorage(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createDiskBackedStorageArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Devpath == "" || a.Name == "" {
		return errorResult("connectionId, node, devpath, and name are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.CreateLVMStorage(ctx, a.Node, a.Devpath, a.Name, a.AddStorage)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "storage.lvm.create", "storage", a.Name)
	return textResult("LVM storage creation started — task " + upid)
}

func toolCreateLVMThinStorage(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createDiskBackedStorageArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Devpath == "" || a.Name == "" {
		return errorResult("connectionId, node, devpath, and name are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.CreateLVMThinStorage(ctx, a.Node, a.Devpath, a.Name, a.AddStorage)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "storage.lvmthin.create", "storage", a.Name)
	return textResult("LVM-thin storage creation started — task " + upid)
}

func toolCreateDirectoryStorage(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createDiskBackedStorageArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Devpath == "" || a.Name == "" {
		return errorResult("connectionId, node, devpath, and name are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.CreateDirectoryStorage(ctx, a.Node, a.Devpath, a.Name, a.AddStorage)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "storage.directory.create", "storage", a.Name)
	return textResult("directory storage creation started — task " + upid)
}

type createZFSPoolArgs struct {
	diskStorageArgs
	Devices   []string `json:"devices"`
	RaidLevel string   `json:"raidLevel"`
	Ashift    int      `json:"ashift,omitempty"`
}

func toolCreateZFSPool(ctx context.Context, s *Server, user *auth.User, args json.RawMessage) toolCallResult {
	if r, ok := requireAdmin(user); !ok {
		return r
	}
	var a createZFSPoolArgs
	if err := json.Unmarshal(args, &a); err != nil || a.ConnectionID == "" || a.Node == "" || a.Name == "" || len(a.Devices) == 0 || a.RaidLevel == "" {
		return errorResult("connectionId, node, name, devices, and raidLevel are required")
	}
	client, err := s.resolver.ClientFor(ctx, a.ConnectionID)
	if err != nil {
		return errorResult(err.Error())
	}
	upid, err := client.CreateZFSPool(ctx, a.Node, a.Name, a.Devices, a.RaidLevel, a.Ashift, a.AddStorage)
	if err != nil {
		return errorResult(err.Error())
	}
	auditMCP(s, ctx, user, "storage.zfs.create", "storage", a.Name)
	return textResult("ZFS pool creation started — task " + upid)
}

// auditMCP records a mutating tool call the same way the REST handlers'
// s.audit(r, ...) does — MCP tools have no *http.Request to pull that from,
// so this is the ctx/user-based equivalent, skipped entirely (like
// toolGuestPowerAction already does) when no audit sink is wired up.
func auditMCP(s *Server, ctx context.Context, user *auth.User, action, category, target string) {
	if s.audit != nil {
		s.audit(ctx, user.ID, action, category, target+" (via MCP)")
	}
}

func init() {
	toolDefinitions = append(toolDefinitions,
		Tool{Name: "create_vm", Description: "Create a new VM on a node. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "vmid": {"integer", "Numeric VMID; omit to auto-assign."},
			"name": {"string", "Guest name."}, "cores": {"integer", "CPU core count."}, "memoryMb": {"integer", "Memory in MB."},
			"storage": {"string", "Target storage for the boot disk."}, "diskGb": {"integer", "Boot disk size in GB."},
			"iso": {"string", "Boot ISO volume, e.g. local:iso/ubuntu-24.04.iso."}, "bridge": {"string", "Network bridge, e.g. vmbr0."},
			"ciUser": {"string", "Cloud-init user (enables cloud-init when set)."}, "ciPassword": {"string", "Cloud-init password."},
			"sshPublicKey": {"string", "Cloud-init SSH public key."}, "ipConfig": {"string", "Cloud-init IP config, e.g. ip=dhcp."},
		}, "connectionId", "node", "cores", "memoryMb")},
		Tool{Name: "create_lxc", Description: "Create a new LXC container on a node. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "vmid": {"integer", "Numeric VMID; omit to auto-assign."},
			"hostname": {"string", "Container hostname."}, "cores": {"integer", "CPU core count."}, "memoryMb": {"integer", "Memory in MB."},
			"storage": {"string", "Target storage for the rootfs."}, "diskGb": {"integer", "Rootfs size in GB."},
			"template": {"string", "Template volume, e.g. local:vztmpl/ubuntu-24.04-standard_24.04-1_amd64.tar.zst."},
			"bridge": {"string", "Network bridge, e.g. vmbr0."}, "password": {"string", "Root password."},
			"sshPublicKey": {"string", "SSH public key for root."}, "ipConfig": {"string", "IP config, e.g. ip=dhcp."},
			"unprivileged": {"boolean", "Create as an unprivileged container (recommended)."},
		}, "connectionId", "node", "cores", "memoryMb", "template")},
		Tool{Name: "clone_guest", Description: "Clone a VM/container to a new VMID. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "type": {"string", descGType}, "vmid": {"integer", "The source guest's VMID."},
			"newId": {"integer", "The new guest's VMID."}, "name": {"string", "Name for the clone."}, "targetNode": {"string", "Node for the clone; omit for the same node."},
			"full": {"boolean", "Full clone (true) vs linked clone (false, only valid from a template)."}, "storage": {"string", "Target storage (full clones only)."},
			"description": {"string", "Description for the clone."},
		}, "connectionId", "node", "type", "vmid", "newId")},
		Tool{Name: "list_snapshots", Description: "List snapshots of one VM/container.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "type": {"string", descGType}, "vmid": {"integer", descVMID},
		}, "connectionId", "node", "type", "vmid")},
		Tool{Name: "create_snapshot", Description: "Create a snapshot of a VM/container. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "type": {"string", descGType}, "vmid": {"integer", descVMID},
			"name": {"string", "Snapshot name."}, "description": {"string", "Snapshot description."}, "includeState": {"boolean", "Include RAM state (VMs only)."},
		}, "connectionId", "node", "type", "vmid", "name")},
		Tool{Name: "rollback_snapshot", Description: "Roll a VM/container back to a snapshot, discarding changes made since. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "type": {"string", descGType}, "vmid": {"integer", descVMID},
			"snapshotName": {"string", "The snapshot to roll back to."},
		}, "connectionId", "node", "type", "vmid", "snapshotName")},
		Tool{Name: "unlock_guest", Description: "Clear a stuck lock on a VM/container. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "type": {"string", descGType}, "vmid": {"integer", descVMID},
		}, "connectionId", "node", "type", "vmid")},
		Tool{Name: "set_guest_template", Description: "Convert a VM/container into a template. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "type": {"string", descGType}, "vmid": {"integer", descVMID},
		}, "connectionId", "node", "type", "vmid")},

		Tool{Name: "wake_on_lan", Description: "Send a wake-on-LAN packet to power on a node. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode},
		}, "connectionId", "node")},
		Tool{Name: "start_all_guests", Description: "Start every guest on a node (respects each guest's configured startup order/delay). Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode},
		}, "connectionId", "node")},
		Tool{Name: "stop_all_guests", Description: "Stop every guest on a node. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode},
		}, "connectionId", "node")},
		Tool{Name: "node_service_action", Description: "Start, restart, or reload a system service on a node (stop is not exposed). Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "service": {"string", "The service name, e.g. pveproxy."},
			"action": {"string", "One of: start, restart, reload."},
		}, "connectionId", "node", "service", "action")},
		Tool{Name: "apt_refresh", Description: "Refresh a node's APT package index. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode},
		}, "connectionId", "node")},
		Tool{Name: "apt_upgrade", Description: "Upgrade all outdated packages on a node. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode},
		}, "connectionId", "node")},

		Tool{Name: "add_guest_firewall_rule", Description: "Add a firewall rule to one VM/container. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "type": {"string", descGType}, "vmid": {"integer", descVMID},
			"direction": {"string", "\"in\" or \"out\"."}, "action": {"string", "ACCEPT, DROP, REJECT, or a security group name."},
			"source": {"string", "Source address/CIDR/alias."}, "dest": {"string", "Destination address/CIDR/alias."},
			"proto": {"string", "Protocol, e.g. tcp."}, "dport": {"string", "Destination port(s)."}, "sport": {"string", "Source port(s)."},
			"macro": {"string", "A firewall macro name, e.g. SSH."}, "comment": {"string", "Rule comment."}, "enable": {"boolean", "Enable the rule immediately."},
		}, "connectionId", "node", "type", "vmid", "direction", "action")},
		Tool{Name: "add_node_firewall_rule", Description: "Add a firewall rule to one node. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode},
			"direction": {"string", "\"in\" or \"out\"."}, "action": {"string", "ACCEPT, DROP, REJECT, or a security group name."},
			"source": {"string", "Source address/CIDR/alias."}, "dest": {"string", "Destination address/CIDR/alias."},
			"proto": {"string", "Protocol, e.g. tcp."}, "dport": {"string", "Destination port(s)."}, "sport": {"string", "Source port(s)."},
			"macro": {"string", "A firewall macro name, e.g. SSH."}, "comment": {"string", "Rule comment."}, "enable": {"boolean", "Enable the rule immediately."},
		}, "connectionId", "node", "direction", "action")},
		Tool{Name: "add_cluster_firewall_rule", Description: "Add a cluster-wide firewall rule. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID},
			"direction": {"string", "\"in\" or \"out\"."}, "action": {"string", "ACCEPT, DROP, REJECT, or a security group name."},
			"source": {"string", "Source address/CIDR/alias."}, "dest": {"string", "Destination address/CIDR/alias."},
			"proto": {"string", "Protocol, e.g. tcp."}, "dport": {"string", "Destination port(s)."}, "sport": {"string", "Source port(s)."},
			"macro": {"string", "A firewall macro name, e.g. SSH."}, "comment": {"string", "Rule comment."}, "enable": {"boolean", "Enable the rule immediately."},
		}, "connectionId", "direction", "action")},
		Tool{Name: "add_firewall_alias", Description: "Add a cluster firewall alias (a named CIDR). Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "name": {"string", "Alias name."}, "cidr": {"string", "The address/CIDR it refers to."}, "comment": {"string", "Comment."},
		}, "connectionId", "name", "cidr")},
		Tool{Name: "add_firewall_ipset", Description: "Create a cluster firewall IP set. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "name": {"string", "IP set name."}, "comment": {"string", "Comment."},
		}, "connectionId", "name")},
		Tool{Name: "add_firewall_ipset_entry", Description: "Add a CIDR entry to an existing firewall IP set. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "setName": {"string", "The IP set's name."}, "cidr": {"string", "Address/CIDR to add."}, "comment": {"string", "Comment."},
		}, "connectionId", "setName", "cidr")},
		Tool{Name: "create_firewall_security_group", Description: "Create a reusable firewall security group. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "name": {"string", "Security group name."}, "comment": {"string", "Comment."},
		}, "connectionId", "name")},
		Tool{Name: "add_security_group_rule", Description: "Add a rule to a firewall security group. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "groupName": {"string", "The security group's name."},
			"direction": {"string", "\"in\" or \"out\"."}, "action": {"string", "ACCEPT, DROP, or REJECT."},
			"source": {"string", "Source address/CIDR/alias."}, "dest": {"string", "Destination address/CIDR/alias."},
			"proto": {"string", "Protocol, e.g. tcp."}, "dport": {"string", "Destination port(s)."}, "sport": {"string", "Source port(s)."},
			"macro": {"string", "A firewall macro name, e.g. SSH."}, "comment": {"string", "Rule comment."}, "enable": {"boolean", "Enable the rule immediately."},
		}, "connectionId", "groupName", "direction", "action")},
		Tool{Name: "update_guest_firewall_options", Description: "Turn a VM/container's own firewall on or off (rules stay inert until this is on). Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "type": {"string", descGType}, "vmid": {"integer", descVMID}, "enable": {"boolean", "Enable the guest's firewall."},
		}, "connectionId", "node", "type", "vmid", "enable")},
		Tool{Name: "update_cluster_firewall_options", Description: "Turn the cluster-wide firewall on or off. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "enable": {"boolean", "Enable the cluster firewall."},
		}, "connectionId", "enable")},

		Tool{Name: "create_backup_job", Description: "Create a scheduled backup job. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "schedule": {"string", "PVE calendar-event schedule, e.g. \"sat 02:00\"."}, "storage": {"string", "Target storage for backups."},
			"vmids": {"string", "Comma-separated VMIDs; omit for all guests."}, "mode": {"string", "\"snapshot\", \"suspend\", or \"stop\"."},
			"compress": {"string", "\"0\", \"lzo\", \"gzip\", or \"zstd\"."}, "enabled": {"boolean", "Enable the job."}, "comment": {"string", "Comment."},
			"prune": {"integer", "Keep-last count for backup retention; 0 = unset."},
		}, "connectionId", "schedule", "storage")},
		Tool{Name: "update_backup_job", Description: "Update an existing scheduled backup job. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "jobId": {"string", "The backup job's id."}, "schedule": {"string", "PVE calendar-event schedule."}, "storage": {"string", "Target storage."},
			"vmids": {"string", "Comma-separated VMIDs; omit for all guests."}, "mode": {"string", "\"snapshot\", \"suspend\", or \"stop\"."},
			"compress": {"string", "\"0\", \"lzo\", \"gzip\", or \"zstd\"."}, "enabled": {"boolean", "Enable the job."}, "comment": {"string", "Comment."},
			"prune": {"integer", "Keep-last count for backup retention; 0 = unset."},
		}, "connectionId", "jobId")},
		Tool{Name: "run_backup_now", Description: "Run a one-off backup immediately. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "storage": {"string", "Target storage for the backup."}, "mode": {"string", "\"snapshot\", \"suspend\", or \"stop\"."},
			"vmids": {"array", "VMIDs to back up (array of strings); omit for all guests."},
		}, "connectionId", "node", "storage")},
		Tool{Name: "create_replication_job", Description: "Create a storage replication job for a guest. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "jobId": {"string", "New job id, e.g. \"100-0\"."}, "guest": {"integer", "The guest's VMID."}, "target": {"string", "Target node."},
			"schedule": {"string", "PVE calendar-event schedule."}, "comment": {"string", "Comment."}, "disable": {"boolean", "Create disabled."},
		}, "connectionId", "jobId", "guest", "target")},
		Tool{Name: "update_replication_job", Description: "Update an existing storage replication job. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "jobId": {"string", "The replication job's id."}, "target": {"string", "Target node."},
			"schedule": {"string", "PVE calendar-event schedule."}, "comment": {"string", "Comment."}, "disable": {"boolean", "Disable the job."},
		}, "connectionId", "jobId")},
		Tool{Name: "run_replication_now", Description: "Run a replication job immediately. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "jobId": {"string", "The replication job's id."},
		}, "connectionId", "node", "jobId")},

		Tool{Name: "add_ha_resource", Description: "Add a guest to HA management. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "sid": {"string", "HA resource id, e.g. \"vm:100\"."}, "group": {"string", "HA group name."},
			"maxRestart": {"integer", "Max restart attempts."}, "maxRelocate": {"integer", "Max relocation attempts."},
		}, "connectionId", "sid")},
		Tool{Name: "update_ha_resource", Description: "Update an HA-managed resource's settings. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "sid": {"string", "HA resource id, e.g. \"vm:100\"."}, "group": {"string", "HA group name."},
			"maxRestart": {"integer", "Max restart attempts."}, "maxRelocate": {"integer", "Max relocation attempts."}, "comment": {"string", "Comment."},
		}, "connectionId", "sid")},
		Tool{Name: "create_ha_group", Description: "Create an HA group (a set of nodes resources can run on). Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "group": {"string", "Group name."}, "nodes": {"string", "Comma-separated node list, e.g. \"node1,node2\"."},
			"restricted": {"boolean", "Restrict resources to only these nodes."}, "nofailback": {"boolean", "Disable automatic failback."}, "comment": {"string", "Comment."},
		}, "connectionId", "group", "nodes")},
		Tool{Name: "update_ha_group", Description: "Update an HA group's raw options. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "id": {"string", "The HA group's name."}, "options": {"object", "Raw PVE option key/value pairs to set, e.g. {\"nodes\":\"node1,node2\"}."},
		}, "connectionId", "id", "options")},
		Tool{Name: "create_ha_rule", Description: "Create an HA rule (raw PVE option key/value pairs). Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "id": {"string", "New rule id."}, "options": {"object", "Raw PVE option key/value pairs."},
		}, "connectionId", "id", "options")},
		Tool{Name: "update_ha_rule", Description: "Update an HA rule's raw options. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "id": {"string", "The rule's id."}, "options": {"object", "Raw PVE option key/value pairs to set."},
		}, "connectionId", "id", "options")},

		Tool{Name: "create_storage", Description: "Register a new storage pool. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "storage": {"string", "Storage id/name."}, "type": {"string", "PVE storage type, e.g. \"dir\", \"nfs\", \"lvm\", \"zfspool\"."},
			"content": {"string", "Comma-separated content types, e.g. \"images,iso,backup\"."}, "nodes": {"string", "Comma-separated node restriction; omit for all nodes."},
			"shared": {"boolean", "Mark as shared storage."}, "disable": {"boolean", "Create disabled."}, "extra": {"object", "Additional raw PVE storage options."},
		}, "connectionId", "storage", "type")},
		Tool{Name: "update_storage", Description: "Update an existing storage pool's settings. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "storage": {"string", "Storage id/name."}, "content": {"string", "Comma-separated content types."},
			"nodes": {"string", "Comma-separated node restriction."}, "shared": {"boolean", "Mark as shared storage."}, "disable": {"boolean", "Disable the storage."},
			"extra": {"object", "Additional raw PVE storage options."},
		}, "connectionId", "storage")},
		Tool{Name: "create_sdn_zone", Description: "Create an SDN zone. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "name": {"string", "Zone id."}, "options": {"object", "Raw PVE zone options, e.g. {\"type\":\"simple\"}."},
		}, "connectionId", "name")},
		Tool{Name: "create_sdn_vnet", Description: "Create an SDN vnet within a zone. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "vnet": {"string", "Vnet id."}, "zone": {"string", "The zone to create it in."},
			"alias": {"string", "Display alias."}, "tag": {"integer", "VLAN/VXLAN tag."}, "vlanAware": {"boolean", "Allow VLAN-tagged traffic through."},
		}, "connectionId", "vnet", "zone")},
		Tool{Name: "create_sdn_subnet", Description: "Create a subnet on an SDN vnet. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "vnet": {"string", "The vnet to attach it to."}, "cidr": {"string", "Subnet CIDR."},
			"gateway": {"string", "Gateway address."}, "snat": {"boolean", "Enable SNAT for this subnet."},
		}, "connectionId", "vnet", "cidr")},
		Tool{Name: "create_sdn_controller", Description: "Create an SDN controller. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "name": {"string", "Controller id."}, "options": {"object", "Raw PVE controller options, e.g. {\"type\":\"evpn\"}."},
		}, "connectionId", "name")},
		Tool{Name: "create_sdn_ipam", Description: "Create an SDN IPAM. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "name": {"string", "IPAM id."}, "options": {"object", "Raw PVE IPAM options, e.g. {\"type\":\"pve\"}."},
		}, "connectionId", "name")},
		Tool{Name: "apply_sdn_config", Description: "Apply pending SDN configuration changes across the cluster. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID},
		}, "connectionId")},
		Tool{Name: "create_zfs_pool", Description: "Create a ZFS storage pool on a node from raw disks. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "name": {"string", "Pool name."}, "devices": {"array", "Array of device paths, e.g. [\"/dev/sdb\"]."},
			"raidLevel": {"string", "\"single\", \"mirror\", \"raid10\", \"raidz\", \"raidz2\", or \"raidz3\"."}, "ashift": {"integer", "ZFS ashift value."},
			"addStorage": {"boolean", "Also register it as a PVE storage pool."},
		}, "connectionId", "node", "name", "devices", "raidLevel")},
		Tool{Name: "create_lvm_storage", Description: "Create an LVM storage pool on a node from a raw disk. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "devpath": {"string", "The raw device path, e.g. /dev/sdb."}, "name": {"string", "Volume group / storage name."},
			"addStorage": {"boolean", "Also register it as a PVE storage pool."},
		}, "connectionId", "node", "devpath", "name")},
		Tool{Name: "create_lvm_thin_storage", Description: "Create an LVM-thin storage pool on a node from a raw disk. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "devpath": {"string", "The raw device path, e.g. /dev/sdb."}, "name": {"string", "Thin pool / storage name."},
			"addStorage": {"boolean", "Also register it as a PVE storage pool."},
		}, "connectionId", "node", "devpath", "name")},
		Tool{Name: "create_directory_storage", Description: "Format and mount a raw disk as directory storage on a node. Requires an admin API key.", InputSchema: buildSchema(map[string]prop{
			"connectionId": {"string", descConnID}, "node": {"string", descNode}, "devpath": {"string", "The raw device path, e.g. /dev/sdb."}, "name": {"string", "Storage name."},
			"addStorage": {"boolean", "Also register it as a PVE storage pool."},
		}, "connectionId", "node", "devpath", "name")},
	)

	for name, fn := range map[string]toolFunc{
		"create_vm": toolCreateVM, "create_lxc": toolCreateLXC, "clone_guest": toolCloneGuest,
		"list_snapshots": toolListSnapshots, "create_snapshot": toolCreateSnapshot, "rollback_snapshot": toolRollbackSnapshot,
		"unlock_guest": toolUnlockGuest, "set_guest_template": toolSetGuestTemplate,

		"wake_on_lan": toolWakeOnLan, "start_all_guests": toolStartAllGuests, "stop_all_guests": toolStopAllGuests,
		"node_service_action": toolNodeServiceAction, "apt_refresh": toolAptRefresh, "apt_upgrade": toolAptUpgrade,

		"add_guest_firewall_rule": toolAddGuestFirewallRule, "add_node_firewall_rule": toolAddNodeFirewallRule,
		"add_cluster_firewall_rule": toolAddClusterFirewallRule, "add_firewall_alias": toolAddFirewallAlias,
		"add_firewall_ipset": toolAddFirewallIPSet, "add_firewall_ipset_entry": toolAddFirewallIPSetEntry,
		"create_firewall_security_group": toolCreateFirewallSecurityGroup, "add_security_group_rule": toolAddSecurityGroupRule,
		"update_guest_firewall_options": toolUpdateGuestFirewallOptions, "update_cluster_firewall_options": toolUpdateClusterFirewallOptions,

		"create_backup_job": toolCreateBackupJob, "update_backup_job": toolUpdateBackupJob, "run_backup_now": toolRunBackupNow,
		"create_replication_job": toolCreateReplicationJob, "update_replication_job": toolUpdateReplicationJob, "run_replication_now": toolRunReplicationNow,

		"add_ha_resource": toolAddHAResource, "update_ha_resource": toolUpdateHAResource,
		"create_ha_group": toolCreateHAGroup, "update_ha_group": toolUpdateHAGroup,
		"create_ha_rule": toolCreateHARule, "update_ha_rule": toolUpdateHARule,

		"create_storage": toolCreateStorage, "update_storage": toolUpdateStorage,
		"create_sdn_zone": toolCreateSDNZone, "create_sdn_vnet": toolCreateSDNVnet, "create_sdn_subnet": toolCreateSDNSubnet,
		"create_sdn_controller": toolCreateSDNController, "create_sdn_ipam": toolCreateSDNIPAM, "apply_sdn_config": toolApplySDNConfig,
		"create_zfs_pool": toolCreateZFSPool, "create_lvm_storage": toolCreateLVMStorage,
		"create_lvm_thin_storage": toolCreateLVMThinStorage, "create_directory_storage": toolCreateDirectoryStorage,
	} {
		toolHandlers[name] = fn
	}
}

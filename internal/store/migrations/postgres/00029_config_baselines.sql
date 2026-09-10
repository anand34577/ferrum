-- +goose Up
-- Config drift detection: an admin-captured snapshot of a guest's config +
-- firewall rules, compared against the live values on demand (GET .../drift)
-- to flag changes made outside this app (or by another admin) since the
-- baseline was taken. One baseline per (connection, guest) — capturing again
-- overwrites the previous snapshot rather than keeping history.
CREATE TABLE config_baselines (
    id                     TEXT PRIMARY KEY,
    connection_id          TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    guest_type             TEXT NOT NULL, -- 'qemu' | 'lxc'
    node                   TEXT NOT NULL, -- node the guest lived on when captured; informational only, not part of identity (guests migrate)
    vmid                   INTEGER NOT NULL,
    name                   TEXT NOT NULL,
    snapshot_json          TEXT NOT NULL, -- pve.GuestConfig, JSON-encoded
    firewall_snapshot_json TEXT NOT NULL, -- []pve.FirewallRule, JSON-encoded
    created_by             TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at             TEXT NOT NULL,
    UNIQUE (connection_id, guest_type, vmid)
);

-- +goose Down
DROP TABLE config_baselines;

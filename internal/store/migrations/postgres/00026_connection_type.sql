-- +goose Up
-- Distinguishes a PVE cluster/standalone-node connection from a Proxmox
-- Backup Server remote — both are stored in the same table (same
-- host/port/auth-type/credential shape) but are routed to different API
-- clients. Defaults to 'pve' so every existing row keeps behaving exactly
-- as before the column existed.
ALTER TABLE connections ADD COLUMN type TEXT NOT NULL DEFAULT 'pve';

-- +goose Down
ALTER TABLE connections DROP COLUMN type;

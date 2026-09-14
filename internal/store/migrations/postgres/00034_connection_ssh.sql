-- +goose Up
-- Optional SSH credentials stored per connection, so "SSH Shell" in
-- Inventory can connect straight to that connection's own host (or a
-- guest, from the same dialog) without retyping a password every time.
-- ssh_auth_type: '' (not configured) | 'password' | 'key'. ssh_secret_enc
-- holds whichever secret ssh_auth_type implies (password, or a private
-- key PEM), encrypted the same way token_secret_enc/password_enc already
-- are for the Proxmox credentials on this same row.
ALTER TABLE connections ADD COLUMN ssh_username TEXT NOT NULL DEFAULT '';
ALTER TABLE connections ADD COLUMN ssh_port INTEGER NOT NULL DEFAULT 22;
ALTER TABLE connections ADD COLUMN ssh_auth_type TEXT NOT NULL DEFAULT '';
ALTER TABLE connections ADD COLUMN ssh_secret_enc TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE connections DROP COLUMN ssh_username;
ALTER TABLE connections DROP COLUMN ssh_port;
ALTER TABLE connections DROP COLUMN ssh_auth_type;
ALTER TABLE connections DROP COLUMN ssh_secret_enc;

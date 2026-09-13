-- +goose Up
-- Optional TLS certificate fingerprint pinning for a connection (SHA-256
-- hex of the server certificate's DER, as shown by e.g. pvenode cert or
-- `openssl s_client`). Empty = unset: verification then follows verify_tls
-- as before. When set, the PVE client compares the fingerprint instead of
-- trusting the system CA pool, so a skip-verify connection is no longer
-- silently MITM-able.
ALTER TABLE connections ADD COLUMN tls_fingerprint TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE connections DROP COLUMN tls_fingerprint;

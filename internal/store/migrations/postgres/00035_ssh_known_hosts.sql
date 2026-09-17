-- +goose Up
-- Trust-on-first-use host key pinning for the direct SSH shell feature
-- (internal/api/ssh_console.go). The first connection to a given host:port
-- records the server's host-key fingerprint here instead of accepting it
-- unverified forever; every later connection is compared against the
-- pinned value, so a MITM after that first connection is rejected instead
-- of silently trusted the way ssh.InsecureIgnoreHostKey() did.
CREATE TABLE ssh_known_hosts (
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    fingerprint TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (host, port)
);

-- +goose Down
DROP TABLE ssh_known_hosts;

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ferrumEnvVars is every FERRUM_* variable applyEnv looks at. Tests that
// assert on file contents or defaults neutralize them all first so an
// ambient variable from the surrounding environment can't skew a result.
var ferrumEnvVars = []string{
	"FERRUM_ADDR",
	"FERRUM_SECURE_COOKIES",
	"FERRUM_BEHIND_PROXY",
	"FERRUM_TLS_CERT_FILE",
	"FERRUM_TLS_KEY_FILE",
	"FERRUM_DB_DRIVER",
	"FERRUM_DB_PATH",
	"FERRUM_DB_DSN",
	"FERRUM_SECRET",
	"FERRUM_OIDC_ENABLED",
	"FERRUM_OIDC_DISPLAY_NAME",
	"FERRUM_OIDC_ISSUER_URL",
	"FERRUM_OIDC_CLIENT_ID",
	"FERRUM_OIDC_CLIENT_SECRET",
	"FERRUM_OIDC_REDIRECT_URL",
	"FERRUM_NEEDLE_BIN",
}

func clearFerrumEnv(t *testing.T) {
	t.Helper()
	for _, v := range ferrumEnvVars {
		t.Setenv(v, "") // empty values are skipped by applyEnv
	}
}

// writeConfig writes a YAML file into a fresh temp dir and returns its path,
// so tests never touch the repo's real config files.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ferrum.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestLoadDefaultsWithoutFile(t *testing.T) {
	clearFerrumEnv(t)

	// Empty path: no file read at all.
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("default addr = %q, want \":8080\"", cfg.Server.Addr)
	}
	if cfg.DB.Driver != "sqlite" {
		t.Errorf("default db.driver = %q, want \"sqlite\"", cfg.DB.Driver)
	}
	if cfg.DB.Path != "./data/ferrum.db" {
		t.Errorf("default db.path = %q, want \"./data/ferrum.db\"", cfg.DB.Path)
	}
	if cfg.Secret != "" {
		t.Errorf("default secret = %q, want empty (a random one is generated)", cfg.Secret)
	}
	if cfg.Server.SecureCookies || cfg.Server.BehindProxy || cfg.OIDC.Enabled {
		t.Errorf("defaults enabled something that should be off: %+v", cfg)
	}

	// A non-empty path that doesn't exist must behave the same way, not error.
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	cfg2, err := Load(missing)
	if err != nil {
		t.Fatalf("Load(missing file): %v", err)
	}
	if cfg2 != cfg {
		t.Errorf("missing file gave %+v, want defaults %+v", cfg2, cfg)
	}
}

func TestLoadFullYAML(t *testing.T) {
	clearFerrumEnv(t)

	path := writeConfig(t, `
server:
  addr: ":8443"
  secureCookies: true
  behindProxy: true
  tlsCertFile: /etc/ferrum/tls.crt
  tlsKeyFile: /etc/ferrum/tls.key
db:
  driver: postgres
  dsn: postgres://ferrum:pw@localhost:5432/ferrum
secret: 0123456789abcdef0123456789abcdef
oidc:
  enabled: true
  displayName: Keycloak
  issuerUrl: https://keycloak.example.com/realms/ferrum
  clientId: ferrum
  clientSecret: oidc-client-secret
  redirectUrl: https://ferrum.example.com/api/v1/auth/oidc/callback
needleBinPath: /usr/local/bin/needle
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":8443" {
		t.Errorf("addr = %q", cfg.Server.Addr)
	}
	if !cfg.Server.SecureCookies || !cfg.Server.BehindProxy {
		t.Errorf("secureCookies/behindProxy = %v/%v, want true/true", cfg.Server.SecureCookies, cfg.Server.BehindProxy)
	}
	if cfg.Server.TLSCertFile != "/etc/ferrum/tls.crt" || cfg.Server.TLSKeyFile != "/etc/ferrum/tls.key" {
		t.Errorf("tls files = %q/%q", cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
	}
	if cfg.DB.Driver != "postgres" {
		t.Errorf("db.driver = %q", cfg.DB.Driver)
	}
	if cfg.DB.DSN != "postgres://ferrum:pw@localhost:5432/ferrum" {
		t.Errorf("db.dsn = %q", cfg.DB.DSN)
	}
	if cfg.Secret != "0123456789abcdef0123456789abcdef" {
		t.Errorf("secret = %q", cfg.Secret)
	}
	if !cfg.OIDC.Enabled {
		t.Error("oidc.enabled should be true")
	}
	if cfg.OIDC.DisplayName != "Keycloak" ||
		cfg.OIDC.IssuerURL != "https://keycloak.example.com/realms/ferrum" ||
		cfg.OIDC.ClientID != "ferrum" ||
		cfg.OIDC.ClientSecret != "oidc-client-secret" ||
		cfg.OIDC.RedirectURL != "https://ferrum.example.com/api/v1/auth/oidc/callback" {
		t.Errorf("oidc = %+v", cfg.OIDC)
	}
	if cfg.NeedleBinPath != "/usr/local/bin/needle" {
		t.Errorf("needleBinPath = %q", cfg.NeedleBinPath)
	}
}

// KnownFields(true) is on: a typo'd key must fail loudly instead of being
// silently ignored (which would quietly fall back to the default value).
func TestLoadRejectsUnknownKeys(t *testing.T) {
	clearFerrumEnv(t)

	cases := []struct {
		name string
		yaml string
	}{
		{"typo'd top-level key", "secreet: nope\n"},
		{"typo'd nested key", "server:\n  addres: \":8080\"\n"},
		{"misspelled section", "databases:\n  driver: sqlite\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yaml))
			if err == nil {
				t.Fatal("expected an error for an unknown key, got nil")
			}
			if !strings.Contains(err.Error(), "parsing config") {
				t.Errorf("error %q should mention the config file parse", err)
			}
		})
	}
}

// A zero-byte or whitespace-only config file means "no config": defaults
// apply (plus env), instead of Load failing on yaml's EOF.
func TestLoadTreatsEmptyConfigFileAsNoConfig(t *testing.T) {
	clearFerrumEnv(t)

	cases := map[string]string{
		"zero-byte file":  "",
		"whitespace only": "\n\n   \n\t\n  \n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, content))
			if err != nil {
				t.Fatalf("Load(empty file): %v", err)
			}
			if cfg.Server.Addr != ":8080" {
				t.Errorf("addr = %q, want the default \":8080\"", cfg.Server.Addr)
			}
			if cfg.DB.Driver != "sqlite" || cfg.DB.Path != "./data/ferrum.db" {
				t.Errorf("db = %+v, want the sqlite defaults", cfg.DB)
			}
			if cfg != defaults() {
				t.Errorf("cfg = %+v, want defaults %+v", cfg, defaults())
			}
		})
	}

	t.Run("env still applies on top of an empty file", func(t *testing.T) {
		t.Setenv("FERRUM_ADDR", ":9191")
		t.Setenv("FERRUM_DB_PATH", "/env/wins.db")
		cfg, err := Load(writeConfig(t, ""))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Server.Addr != ":9191" || cfg.DB.Path != "/env/wins.db" {
			t.Errorf("addr/db.path = %q/%q, want the env values", cfg.Server.Addr, cfg.DB.Path)
		}
		if cfg.DB.Driver != "sqlite" {
			t.Errorf("db.driver = %q, want the default", cfg.DB.Driver)
		}
	})
}

func TestLoadValidation(t *testing.T) {
	clearFerrumEnv(t)

	cases := []struct {
		name    string
		yaml    string
		wantErr string // empty means Load must succeed
	}{
		{"sqlite is the valid default", "db:\n  path: /tmp/x.db\n", ""},
		{"mysql driver rejected", "db:\n  driver: mysql\n", `db.driver must be 'sqlite' or 'postgres'`},
		{"postgres without dsn", "db:\n  driver: postgres\n", "db.dsn is required"},
		{"postgres with dsn ok", "db:\n  driver: postgres\n  dsn: postgres://localhost/ferrum\n", ""},
		{"secret shorter than 16", "secret: tooshort\n", "secret must be at least 16 characters"},
		{"secret exactly 16 ok", "secret: abcdefghijklmnop\n", ""},
		{"cert without key", "server:\n  tlsCertFile: /tmp/cert.pem\n", "server.tlsCertFile and server.tlsKeyFile"},
		{"key without cert", "server:\n  tlsKeyFile: /tmp/key.pem\n", "server.tlsCertFile and server.tlsKeyFile"},
		{"cert and key together ok", "server:\n  tlsCertFile: /tmp/cert.pem\n  tlsKeyFile: /tmp/key.pem\n", ""},
		{"oidc enabled missing everything", "oidc:\n  enabled: true\n", "oidc.issuerUrl"},
		{"oidc enabled missing clientId and redirectUrl", "oidc:\n  enabled: true\n  issuerUrl: https://idp.example.com\n", "oidc.issuerUrl"},
		{"oidc fully configured ok", "oidc:\n  enabled: true\n  issuerUrl: https://idp.example.com\n  clientId: ferrum\n  redirectUrl: https://ferrum.example.com/cb\n", ""},
		{"oidc disabled without endpoints ok", "oidc:\n  displayName: Keycloak\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yaml))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("FERRUM_ADDR", ":9090")
	t.Setenv("FERRUM_DB_DRIVER", "postgres")
	t.Setenv("FERRUM_DB_DSN", "postgres://localhost/ferrum-test")
	t.Setenv("FERRUM_SECRET", "env-secret-0123456789abcdef")
	t.Setenv("FERRUM_SECURE_COOKIES", "true")
	t.Setenv("FERRUM_OIDC_ENABLED", "true")
	t.Setenv("FERRUM_OIDC_ISSUER_URL", "https://idp.example.com/realms/test")
	t.Setenv("FERRUM_OIDC_CLIENT_ID", "ferrum")
	t.Setenv("FERRUM_OIDC_REDIRECT_URL", "https://ferrum.example.com/api/v1/auth/oidc/callback")

	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Errorf("addr = %q, want \":9090\"", cfg.Server.Addr)
	}
	if cfg.DB.Driver != "postgres" {
		t.Errorf("db.driver = %q, want \"postgres\"", cfg.DB.Driver)
	}
	if cfg.DB.DSN != "postgres://localhost/ferrum-test" {
		t.Errorf("db.dsn = %q", cfg.DB.DSN)
	}
	if cfg.Secret != "env-secret-0123456789abcdef" {
		t.Errorf("secret = %q", cfg.Secret)
	}
	if !cfg.Server.SecureCookies {
		t.Error("FERRUM_SECURE_COOKIES=true should set secureCookies")
	}
	if !cfg.OIDC.Enabled {
		t.Error("FERRUM_OIDC_ENABLED=true should enable oidc")
	}
	if cfg.OIDC.IssuerURL != "https://idp.example.com/realms/test" || cfg.OIDC.ClientID != "ferrum" {
		t.Errorf("oidc = %+v", cfg.OIDC)
	}
}

// Env wins over the file for the keys it sets; file values survive for the
// keys it doesn't.
func TestLoadEnvPrecedenceOverFile(t *testing.T) {
	clearFerrumEnv(t)
	path := writeConfig(t, `
server:
  addr: ":7777"
db:
  path: /from/file.db
secret: file-secret-0123456789
`)
	t.Setenv("FERRUM_ADDR", ":8888")
	t.Setenv("FERRUM_DB_PATH", "/from/env.db")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":8888" {
		t.Errorf("addr = %q, want the env value \":8888\"", cfg.Server.Addr)
	}
	if cfg.DB.Path != "/from/env.db" {
		t.Errorf("db.path = %q, want the env value \"/from/env.db\"", cfg.DB.Path)
	}
	// Untouched by env: the file's values (and defaults) must stand.
	if cfg.Secret != "file-secret-0123456789" {
		t.Errorf("secret = %q, want the file value", cfg.Secret)
	}
	if cfg.DB.Driver != "sqlite" {
		t.Errorf("db.driver = %q, want the default \"sqlite\"", cfg.DB.Driver)
	}
}

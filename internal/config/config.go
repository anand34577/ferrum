// Package config loads Ferrum's runtime configuration from a YAML file with
// environment-variable overrides, and applies sane single-binary defaults.
package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

type DBConfig struct {
	// Driver selects the storage backend: "sqlite" (default) or "postgres".
	Driver string `yaml:"driver"`
	// Path is the SQLite file path, used when Driver == "sqlite".
	Path string `yaml:"path"`
	// DSN is the Postgres connection string, used when Driver == "postgres".
	DSN string `yaml:"dsn"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
	// SecureCookies marks the session cookie Secure so browsers only send it
	// over HTTPS. Required whenever Ferrum is served over TLS (directly or
	// behind a reverse proxy); with BehindProxy the flag is also set
	// automatically per-request when X-Forwarded-Proto is https.
	SecureCookies bool `yaml:"secureCookies"`
	// BehindProxy tells Ferrum the app is served behind a reverse proxy:
	// client IPs are taken from X-Forwarded-For and the session cookie is
	// marked Secure when the proxy reports an https request.
	BehindProxy bool `yaml:"behindProxy"`
}

// OIDCConfig configures single sign-on against an external identity
// provider (Keycloak, Authentik, Entra ID, ...) via the standard OpenID
// Connect authorization-code flow. Local username/password login keeps
// working unconditionally — this is an additional, optional login path.
type OIDCConfig struct {
	Enabled      bool   `yaml:"enabled"`
	DisplayName  string `yaml:"displayName"` // shown on the "Continue with ..." button, e.g. "Keycloak"
	IssuerURL    string `yaml:"issuerUrl"`   // e.g. https://keycloak.example.com/realms/myrealm
	ClientID     string `yaml:"clientId"`
	ClientSecret string `yaml:"clientSecret"`
	RedirectURL  string `yaml:"redirectUrl"` // e.g. https://ferrum.example.com/api/v1/auth/oidc/callback
}

type Config struct {
	Server ServerConfig `yaml:"server"`
	DB     DBConfig     `yaml:"db"`
	Secret string       `yaml:"secret"`
	OIDC   OIDCConfig   `yaml:"oidc"`
}

func defaults() Config {
	return Config{
		Server: ServerConfig{Addr: ":8080"},
		DB: DBConfig{
			Driver: "sqlite",
			Path:   "./data/ferrum.db",
		},
	}
}

// Load reads config from path (if it exists) and layers FERRUM_* env vars on top.
func Load(path string) (Config, error) {
	cfg := defaults()

	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			dec := yaml.NewDecoder(bytes.NewReader(data))
			dec.KnownFields(true) // a typo'd key like "secreet" must fail loudly, not be ignored
			if err := dec.Decode(&cfg); err != nil {
				return cfg, fmt.Errorf("parsing config %s: %w", path, err)
			}
		} else if !os.IsNotExist(err) {
			return cfg, fmt.Errorf("reading config %s: %w", path, err)
		}
	}

	applyEnv(&cfg)

	if cfg.DB.Driver != "sqlite" && cfg.DB.Driver != "postgres" {
		return cfg, fmt.Errorf("db.driver must be 'sqlite' or 'postgres', got %q", cfg.DB.Driver)
	}
	if cfg.DB.Driver == "postgres" && cfg.DB.DSN == "" {
		return cfg, fmt.Errorf("db.dsn is required when db.driver is postgres")
	}
	if cfg.OIDC.Enabled && (cfg.OIDC.IssuerURL == "" || cfg.OIDC.ClientID == "" || cfg.OIDC.RedirectURL == "") {
		return cfg, fmt.Errorf("oidc.issuerUrl, oidc.clientId, and oidc.redirectUrl are required when oidc.enabled is true")
	}
	if cfg.Secret != "" && len(cfg.Secret) < 16 {
		return cfg, fmt.Errorf("secret must be at least 16 characters (a random one is generated if omitted)")
	}

	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("FERRUM_ADDR"); v != "" {
		cfg.Server.Addr = v
	}
	if v := os.Getenv("FERRUM_SECURE_COOKIES"); v != "" {
		cfg.Server.SecureCookies = v == "true" || v == "1"
	}
	if v := os.Getenv("FERRUM_BEHIND_PROXY"); v != "" {
		cfg.Server.BehindProxy = v == "true" || v == "1"
	}
	if v := os.Getenv("FERRUM_DB_DRIVER"); v != "" {
		cfg.DB.Driver = v
	}
	if v := os.Getenv("FERRUM_DB_PATH"); v != "" {
		cfg.DB.Path = v
	}
	if v := os.Getenv("FERRUM_DB_DSN"); v != "" {
		cfg.DB.DSN = v
	}
	if v := os.Getenv("FERRUM_SECRET"); v != "" {
		cfg.Secret = v
	}
	if v := os.Getenv("FERRUM_OIDC_ENABLED"); v != "" {
		if v != "true" && v != "1" && v != "false" && v != "0" {
			slog.Warn("FERRUM_OIDC_ENABLED should be true/false (or 1/0); treating as false", "value", v)
		}
		cfg.OIDC.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("FERRUM_OIDC_DISPLAY_NAME"); v != "" {
		cfg.OIDC.DisplayName = v
	}
	if v := os.Getenv("FERRUM_OIDC_ISSUER_URL"); v != "" {
		cfg.OIDC.IssuerURL = v
	}
	if v := os.Getenv("FERRUM_OIDC_CLIENT_ID"); v != "" {
		cfg.OIDC.ClientID = v
	}
	if v := os.Getenv("FERRUM_OIDC_CLIENT_SECRET"); v != "" {
		cfg.OIDC.ClientSecret = v
	}
	if v := os.Getenv("FERRUM_OIDC_REDIRECT_URL"); v != "" {
		cfg.OIDC.RedirectURL = v
	}
}

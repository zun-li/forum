package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validYAML = `jwt:
  secret: "test-only-secret-at-least-32-bytes"
  issuer: "forum"
  audience: "forum-api"
  access_ttl: "30m"
server:
  addr: "127.0.0.1:8080"
mysql:
  addr: "127.0.0.1:3306"
  user: "root"
  password: "file-password"
  database: "forum"
  max_open_conns: 10
  max_idle_conns: 5
`

func cleanEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"JWT_SECRET", "JWT_ISSUER", "JWT_AUDIENCE", "JWT_ACCESS_TTL",
		"JWT_RENEWAL_WINDOW", "JWT_MAX_LIFETIME",
		"SNOWFLAKE_NODE_ID",
		"REDIS_ADDR", "REDIS_DB", "REDIS_USERNAME", "REDIS_PASSWORD",
		"SERVER_ADDR",
		"DB_ADDR",
		"DB_USER",
		"DB_PASSWORD",
		"DB_NAME",
		"DB_MAX_OPEN_CONNS",
		"DB_MAX_IDLE_CONNS",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	cleanEnv(t)
	path := writeConfig(t, validYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != "127.0.0.1:8080" || cfg.MySQL.Database != "forum" || cfg.MySQL.MaxOpenConns != 10 {
		t.Fatal("YAML values were not loaded")
	}
	if cfg.MySQL.Password != "file-password" {
		t.Fatal("YAML password was not loaded")
	}
	if cfg.JWT.AccessTTL != 30*time.Minute || cfg.JWT.RenewalWindow != 5*time.Minute || cfg.JWT.MaxLifetime != 7*24*time.Hour {
		t.Fatal("incorrect default session lifetimes")
	}
	t.Setenv("REDIS_ADDR", "localhost:6381")
	t.Setenv("REDIS_DB", "3")
	t.Setenv("REDIS_PASSWORD", "cache-password")
	t.Setenv("DB_PASSWORD", "env-password")
	t.Setenv("JWT_SECRET", "environment-secret-at-least-32-bytes")
	t.Setenv("JWT_ACCESS_TTL", "15m")
	t.Setenv("JWT_RENEWAL_WINDOW", "2m")
	t.Setenv("JWT_MAX_LIFETIME", "24h")
	t.Setenv("DB_MAX_OPEN_CONNS", "20")
	t.Setenv("SERVER_ADDR", "127.0.0.1:9090")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.Secret != "environment-secret-at-least-32-bytes" || cfg.JWT.AccessTTL.String() != "15m0s" {
		t.Fatal("JWT environment variables did not override YAML")
	}
	if cfg.JWT.RenewalWindow != 2*time.Minute || cfg.JWT.MaxLifetime != 24*time.Hour {
		t.Fatal("session environment overrides were not loaded")
	}
	if cfg.Redis.Addr != "localhost:6381" || cfg.Redis.DB != 3 || cfg.Redis.Password != "cache-password" {
		t.Fatal("redis environment variables were not loaded")
	}
	if cfg.MySQL.Password != "env-password" || cfg.MySQL.MaxOpenConns != 20 || cfg.Server.Addr != "127.0.0.1:9090" {
		t.Fatal("environment variables did not override YAML")
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	cleanEnv(t)
	cases := map[string]string{
		"missing secret":         strings.Replace(validYAML, "test-only-secret-at-least-32-bytes", "", 1),
		"short secret":           strings.Replace(validYAML, "test-only-secret-at-least-32-bytes", "short", 1),
		"missing issuer":         strings.Replace(validYAML, `issuer: "forum"`, `issuer: ""`, 1),
		"invalid TTL":            strings.Replace(validYAML, "30m", "-1s", 1),
		"invalid snowflake":      validYAML + "snowflake:\n  node_id: 1024\n",
		"malformed YAML":         "server: [",
		"unknown field":          validYAML + "unknown: true\n",
		"invalid redis address":  validYAML + "redis:\n  addr: localhost\n",
		"invalid redis database": validYAML + "redis:\n  db: -1\n",
		"missing fields":         "server:\n  addr: '127.0.0.1:8080'\n",
		"invalid port":           strings.Replace(validYAML, "8080", "70000", 1),
		"invalid pool":           strings.Replace(validYAML, "max_idle_conns: 5", "max_idle_conns: 11", 1),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, content)); err == nil {
				t.Fatal("expected invalid configuration to fail")
			}
		})
	}
	for _, tc := range []struct{ key, value string }{
		{"JWT_RENEWAL_WINDOW", "0s"},
		{"JWT_RENEWAL_WINDOW", "30m"},
		{"JWT_RENEWAL_WINDOW", "1.5s"},
		{"JWT_MAX_LIFETIME", "29m"},
		{"JWT_MAX_LIFETIME", "0s"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(writeConfig(t, validYAML)); err == nil {
				t.Fatal("invalid session lifetime accepted")
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected missing configuration to fail")
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GITSYNC_SERVER_BASE_URL", "https://sync.example.test/")
	t.Setenv("GITSYNC_OIDC_ISSUER_URL", "https://id.example.test")
	t.Setenv("GITSYNC_OIDC_CLIENT_ID", "client")
	t.Setenv("GITSYNC_OIDC_CLIENT_SECRET", "client-secret-value")
	t.Setenv("GITSYNC_OIDC_SESSION_SECRET", strings.Repeat("s", 32))
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaultsAndPrecedence(t *testing.T) {
	requiredEnv(t)
	path := writeConfig(t, "server:\n  listen: ':9090'\nsync:\n  max_concurrent: 7\n")
	t.Setenv("GITSYNC_SERVER_LISTEN", ":7070")
	t.Setenv("GITSYNC_SYNC_MAX_CONCURRENT", "8")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != ":7070" || c.Sync.MaxConcurrent != 8 || c.Storage.LogsDir != "/data/logs" || c.Server.BaseURL != "https://sync.example.test" || c.OIDC.RedirectURL != "https://sync.example.test/auth/callback" {
		t.Fatalf("unexpected config: %+v", c)
	}
	if c.Server.ShutdownTimeout.String() != "30s" || c.OIDC.SessionTTL.String() != "12h0m0s" {
		t.Fatalf("duration defaults not loaded: %+v", c)
	}
}

func TestLoadExpansionAndScopes(t *testing.T) {
	requiredEnv(t)
	t.Setenv("SAMPLE_VALUE", "expanded")
	p := writeConfig(t, "webhooks:\n  secret: '${SAMPLE_VALUE}'\nnotifications:\n  webhook:\n    headers:\n      X-Test: '${SAMPLE_VALUE}'\n    secret: '$$literal'\n")
	t.Setenv("GITSYNC_OIDC_SCOPES", "openid,email")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Webhooks.Secret != "expanded" || c.Notifications.Webhook.Headers["X-Test"] != "expanded" || c.Notifications.Webhook.Secret != "$literal" {
		t.Fatalf("expansion failed: %+v", c)
	}
	if strings.Join(c.OIDC.Scopes, ",") != "openid,email" {
		t.Fatalf("scopes: %#v", c.OIDC.Scopes)
	}
	// YAML can omit openid; it is enforced in the effective config.
	t.Setenv("GITSYNC_OIDC_SCOPES", "email")
	c, err = Load(writeConfig(t, "oidc:\n  scopes: [email]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.OIDC.Scopes[0] != "openid" {
		t.Fatalf("openid missing: %#v", c.OIDC.Scopes)
	}
}

func TestLoadExpansionErrorsAndUnknownKeys(t *testing.T) {
	requiredEnv(t)
	_, err := Load(writeConfig(t, "webhooks:\n  secret: '${MISSING_CONFIG_TEST}'\n"))
	if err == nil || !strings.Contains(err.Error(), `key "webhooks.secret"`) || !strings.Contains(err.Error(), "MISSING_CONFIG_TEST") {
		t.Fatalf("unexpected expansion error: %v", err)
	}
	_, err = Load(writeConfig(t, "unexpected: true\n"))
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("unexpected unknown-key error: %v", err)
	}
}

func TestValidationCollectsErrorsAndNamesKeys(t *testing.T) {
	tests := []struct{ name, yaml, envName, envValue, want string }{
		{"base url", "", "GITSYNC_SERVER_BASE_URL", "ftp://bad", "server.base_url"},
		{"issuer", "", "GITSYNC_OIDC_ISSUER_URL", "ftp://bad", "oidc.issuer_url"},
		{"short secret", "", "GITSYNC_OIDC_SESSION_SECRET", "short", "oidc.session_secret"},
		{"concurrency", "", "GITSYNC_SYNC_MAX_CONCURRENT", "0", "sync.max_concurrent"},
		{"history", "", "GITSYNC_HISTORY_MAX_ITEMS", "0", "history.max_items"},
		{"relative logs", "", "GITSYNC_STORAGE_LOGS_DIR", "relative", "storage.logs_dir"},
		{"telegram", "notifications:\n  telegram:\n    enabled: true\n", "", "", "notifications.telegram.bot_token"},
		{"webhook", "notifications:\n  webhook:\n    enabled: true\n    url: ftp://invalid\n", "", "", "notifications.webhook.url"},
		{"log level", "", "GITSYNC_LOG_LEVEL", "trace", "log.level"},
		{"log format", "", "GITSYNC_LOG_FORMAT", "xml", "log.format"},
		{"data directory", "", "GITSYNC_STORAGE_DATA_DIR", "relative", "storage.data_dir"},
		{"repos directory", "", "GITSYNC_STORAGE_REPOS_ROOT", "relative", "storage.repos_root"},
		{"duration", "", "GITSYNC_SYNC_GIT_TIMEOUT", "0s", "sync.git_timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requiredEnv(t)
			if tt.envName != "" {
				t.Setenv(tt.envName, tt.envValue)
			}
			_, err := Load(writeConfig(t, tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v does not name %q", err, tt.want)
			}
		})
	}
	t.Setenv("GITSYNC_SERVER_BASE_URL", "bad")
	t.Setenv("GITSYNC_OIDC_ISSUER_URL", "bad")
	t.Setenv("GITSYNC_SYNC_MAX_CONCURRENT", "0")
	t.Setenv("GITSYNC_HISTORY_MAX_ITEMS", "0")
	_, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil || !strings.Contains(err.Error(), "server.base_url") || !strings.Contains(err.Error(), "oidc.issuer_url") || !strings.Contains(err.Error(), "sync.max_concurrent") || !strings.Contains(err.Error(), "history.max_items") {
		t.Fatalf("errors were not collected: %v", err)
	}
}

func TestLogsDirectoryOverrideAndEnvParseError(t *testing.T) {
	requiredEnv(t)
	t.Setenv("GITSYNC_STORAGE_LOGS_DIR", "/logs-volume")
	c, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Storage.LogsDir != "/logs-volume" {
		t.Fatal(c.Storage.LogsDir)
	}
	t.Setenv("GITSYNC_STORAGE_LOGS_DIR", "relative")
	_, err = Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil || !strings.Contains(err.Error(), "storage.logs_dir") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
	t.Setenv("GITSYNC_SYNC_MAX_CONCURRENT", "many")
	_, err = Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil || !strings.Contains(err.Error(), "GITSYNC_SYNC_MAX_CONCURRENT") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestRedactedAndExample(t *testing.T) {
	t.Setenv("GITSYNC_PUBLIC_URL", "https://sync.example.test")
	for _, name := range []string{"OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "GITSYNC_SESSION_SECRET", "GITSYNC_WEBHOOK_SECRET", "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID", "NOTIFICATION_WEBHOOK_AUTH", "NOTIFICATION_WEBHOOK_SECRET"} {
		t.Setenv(name, "secret-value")
	}
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.test")
	t.Setenv("OIDC_CLIENT_ID", "client")
	t.Setenv("OIDC_CLIENT_SECRET", "secret-value")
	t.Setenv("GITSYNC_PUBLIC_URL", "https://sync.example.test")
	t.Setenv("GITSYNC_SESSION_SECRET", strings.Repeat("s", 32))
	c, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.OIDC.ClientSecret != "secret-value" {
		t.Fatalf("example env expansion failed: %+v", c.OIDC)
	}
	r := c.Redacted()
	if r.OIDC.ClientSecret != "***" || r.OIDC.SessionSecret != "***" || r.Webhooks.Secret != "***" || r.Notifications.Telegram.BotToken != "***" || r.Notifications.Webhook.Secret != "***" || r.Notifications.Webhook.Headers["Authorization"] != "***" {
		t.Fatalf("secrets not redacted: %+v", r)
	}
	if c.OIDC.ClientSecret != "secret-value" {
		t.Fatal("Redacted mutated the original")
	}
}

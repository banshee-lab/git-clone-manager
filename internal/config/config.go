package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultPath = "/etc/gitsync/config.yaml"

type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) String() string { return time.Duration(d).String() }

type Config struct {
	Server struct {
		Listen          string   `yaml:"listen"`
		BaseURL         string   `yaml:"base_url"`
		ShutdownTimeout Duration `yaml:"shutdown_timeout"`
	} `yaml:"server"`
	Storage struct {
		DataDir   string `yaml:"data_dir"`
		ReposRoot string `yaml:"repos_root"`
		LogsDir   string `yaml:"logs_dir"`
	} `yaml:"storage"`
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
	OIDC struct {
		IssuerURL     string   `yaml:"issuer_url"`
		ClientID      string   `yaml:"client_id"`
		ClientSecret  string   `yaml:"client_secret"`
		RedirectURL   string   `yaml:"redirect_url"`
		Scopes        []string `yaml:"scopes"`
		SessionSecret string   `yaml:"session_secret"`
		SessionTTL    Duration `yaml:"session_ttl"`
	} `yaml:"oidc"`
	Sync struct {
		MaxConcurrent      int      `yaml:"max_concurrent"`
		GitTimeout         Duration `yaml:"git_timeout"`
		PostCommandTimeout Duration `yaml:"post_command_timeout"`
		EnvPassthrough     []string `yaml:"env_passthrough"`
	} `yaml:"sync"`
	History struct {
		MaxItems int `yaml:"max_items"`
	} `yaml:"history"`
	Webhooks struct {
		Secret string `yaml:"secret"`
	} `yaml:"webhooks"`
	Notifications struct {
		Telegram struct {
			Enabled  bool   `yaml:"enabled"`
			BotToken string `yaml:"bot_token"`
			ChatID   string `yaml:"chat_id"`
		} `yaml:"telegram"`
		Webhook struct {
			Enabled bool              `yaml:"enabled"`
			URL     string            `yaml:"url"`
			Headers map[string]string `yaml:"headers"`
			Secret  string            `yaml:"secret"`
			Timeout Duration          `yaml:"timeout"`
		} `yaml:"webhook"`
	} `yaml:"notifications"`
}

func Default() Config {
	var c Config
	c.Server.Listen = ":8080"
	c.Server.ShutdownTimeout = Duration(30 * time.Second)
	c.Storage.DataDir = "/data"
	c.Storage.ReposRoot = "/repos"
	c.Log.Level, c.Log.Format = "info", "json"
	c.OIDC.Scopes = []string{"openid", "profile", "email"}
	c.OIDC.SessionTTL = Duration(12 * time.Hour)
	c.Sync.MaxConcurrent = 3
	c.Sync.GitTimeout = Duration(30 * time.Minute)
	c.Sync.PostCommandTimeout = Duration(10 * time.Minute)
	c.History.MaxItems = 50
	c.Notifications.Webhook.Headers = map[string]string{}
	c.Notifications.Webhook.Timeout = Duration(10 * time.Second)
	return c
}

type envSetter struct {
	name, key string
	set       func(*Config, string) error
}

var envSetters = []envSetter{
	{"GITSYNC_SERVER_LISTEN", "server.listen", setString(func(c *Config) *string { return &c.Server.Listen })},
	{"GITSYNC_SERVER_BASE_URL", "server.base_url", setString(func(c *Config) *string { return &c.Server.BaseURL })},
	{"GITSYNC_SERVER_SHUTDOWN_TIMEOUT", "server.shutdown_timeout", setDuration(func(c *Config) *Duration { return &c.Server.ShutdownTimeout })},
	{"GITSYNC_STORAGE_DATA_DIR", "storage.data_dir", setString(func(c *Config) *string { return &c.Storage.DataDir })},
	{"GITSYNC_STORAGE_REPOS_ROOT", "storage.repos_root", setString(func(c *Config) *string { return &c.Storage.ReposRoot })},
	{"GITSYNC_STORAGE_LOGS_DIR", "storage.logs_dir", setString(func(c *Config) *string { return &c.Storage.LogsDir })},
	{"GITSYNC_LOG_LEVEL", "log.level", setString(func(c *Config) *string { return &c.Log.Level })},
	{"GITSYNC_LOG_FORMAT", "log.format", setString(func(c *Config) *string { return &c.Log.Format })},
	{"GITSYNC_OIDC_ISSUER_URL", "oidc.issuer_url", setString(func(c *Config) *string { return &c.OIDC.IssuerURL })},
	{"GITSYNC_OIDC_CLIENT_ID", "oidc.client_id", setString(func(c *Config) *string { return &c.OIDC.ClientID })},
	{"GITSYNC_OIDC_CLIENT_SECRET", "oidc.client_secret", setString(func(c *Config) *string { return &c.OIDC.ClientSecret })},
	{"GITSYNC_OIDC_REDIRECT_URL", "oidc.redirect_url", setString(func(c *Config) *string { return &c.OIDC.RedirectURL })},
	{"GITSYNC_OIDC_SCOPES", "oidc.scopes", setList(func(c *Config) *[]string { return &c.OIDC.Scopes })},
	{"GITSYNC_OIDC_SESSION_SECRET", "oidc.session_secret", setString(func(c *Config) *string { return &c.OIDC.SessionSecret })},
	{"GITSYNC_OIDC_SESSION_TTL", "oidc.session_ttl", setDuration(func(c *Config) *Duration { return &c.OIDC.SessionTTL })},
	{"GITSYNC_SYNC_MAX_CONCURRENT", "sync.max_concurrent", setInt(func(c *Config) *int { return &c.Sync.MaxConcurrent })},
	{"GITSYNC_SYNC_GIT_TIMEOUT", "sync.git_timeout", setDuration(func(c *Config) *Duration { return &c.Sync.GitTimeout })},
	{"GITSYNC_SYNC_POST_COMMAND_TIMEOUT", "sync.post_command_timeout", setDuration(func(c *Config) *Duration { return &c.Sync.PostCommandTimeout })},
	{"GITSYNC_SYNC_ENV_PASSTHROUGH", "sync.env_passthrough", setList(func(c *Config) *[]string { return &c.Sync.EnvPassthrough })},
	{"GITSYNC_HISTORY_MAX_ITEMS", "history.max_items", setInt(func(c *Config) *int { return &c.History.MaxItems })},
	{"GITSYNC_WEBHOOKS_SECRET", "webhooks.secret", setString(func(c *Config) *string { return &c.Webhooks.Secret })},
	{"GITSYNC_NOTIFICATIONS_TELEGRAM_ENABLED", "notifications.telegram.enabled", setBool(func(c *Config) *bool { return &c.Notifications.Telegram.Enabled })},
	{"GITSYNC_NOTIFICATIONS_TELEGRAM_BOT_TOKEN", "notifications.telegram.bot_token", setString(func(c *Config) *string { return &c.Notifications.Telegram.BotToken })},
	{"GITSYNC_NOTIFICATIONS_TELEGRAM_CHAT_ID", "notifications.telegram.chat_id", setString(func(c *Config) *string { return &c.Notifications.Telegram.ChatID })},
	{"GITSYNC_NOTIFICATIONS_WEBHOOK_ENABLED", "notifications.webhook.enabled", setBool(func(c *Config) *bool { return &c.Notifications.Webhook.Enabled })},
	{"GITSYNC_NOTIFICATIONS_WEBHOOK_URL", "notifications.webhook.url", setString(func(c *Config) *string { return &c.Notifications.Webhook.URL })},
	{"GITSYNC_NOTIFICATIONS_WEBHOOK_SECRET", "notifications.webhook.secret", setString(func(c *Config) *string { return &c.Notifications.Webhook.Secret })},
	{"GITSYNC_NOTIFICATIONS_WEBHOOK_TIMEOUT", "notifications.webhook.timeout", setDuration(func(c *Config) *Duration { return &c.Notifications.Webhook.Timeout })},
}

func setString(p func(*Config) *string) func(*Config, string) error {
	return func(c *Config, v string) error { *p(c) = v; return nil }
}
func setInt(p func(*Config) *int) func(*Config, string) error {
	return func(c *Config, v string) error {
		n, e := strconv.Atoi(v)
		if e == nil {
			*p(c) = n
		}
		return e
	}
}
func setBool(p func(*Config) *bool) func(*Config, string) error {
	return func(c *Config, v string) error {
		n, e := strconv.ParseBool(v)
		if e == nil {
			*p(c) = n
		}
		return e
	}
}
func setDuration(p func(*Config) *Duration) func(*Config, string) error {
	return func(c *Config, v string) error {
		n, e := time.ParseDuration(v)
		if e == nil {
			*p(c) = Duration(n)
		}
		return e
	}
}
func setList(p func(*Config) *[]string) func(*Config, string) error {
	return func(c *Config, v string) error {
		if strings.TrimSpace(v) == "" {
			*p(c) = []string{}
		} else {
			*p(c) = splitList(v)
		}
		return nil
	}
}
func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads defaults, an optional YAML file, then environment overrides.
func Load(path string) (Config, error) {
	if path == "" {
		path = os.Getenv("GITSYNC_CONFIG")
		if path == "" {
			path = DefaultPath
		}
	}
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("config: read %q: %w", path, err)
	}
	if err == nil {
		var node yaml.Node
		if e := yaml.Unmarshal(data, &node); e != nil {
			return Config{}, fmt.Errorf("config: parse %q: %w", path, e)
		}
		if e := expandNode(&node, ""); e != nil {
			return Config{}, e
		}
		dec := yaml.NewDecoder(strings.NewReader(nodeString(&node)))
		dec.KnownFields(true)
		if e := dec.Decode(&c); e != nil {
			return Config{}, fmt.Errorf("config: decode %q: %w", path, e)
		}
	}
	for _, item := range envSetters {
		if value, ok := os.LookupEnv(item.name); ok {
			if e := item.set(&c, value); e != nil {
				return Config{}, fmt.Errorf("config: key %q: invalid %s: %w", item.key, item.name, e)
			}
		}
	}
	if c.Storage.LogsDir == "" {
		c.Storage.LogsDir = filepath.Join(c.Storage.DataDir, "logs")
	}
	c.Server.BaseURL = strings.TrimRight(c.Server.BaseURL, "/")
	if c.OIDC.RedirectURL == "" && c.Server.BaseURL != "" {
		c.OIDC.RedirectURL = c.Server.BaseURL + "/auth/callback"
	}
	if !contains(c.OIDC.Scopes, "openid") {
		c.OIDC.Scopes = append([]string{"openid"}, c.OIDC.Scopes...)
	}
	if errs := validate(c); len(errs) > 0 {
		return Config{}, fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

func nodeString(n *yaml.Node) string {
	b, e := yaml.Marshal(n)
	if e != nil {
		return ""
	}
	return string(b)
}
func expandNode(n *yaml.Node, path string) error {
	if n.Kind == yaml.DocumentNode {
		for _, child := range n.Content {
			if err := expandNode(child, path); err != nil {
				return err
			}
		}
		return nil
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i].Value
			child := n.Content[i+1]
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if err := expandNode(child, childPath); err != nil {
				return err
			}
		}
		return nil
	}
	if n.Kind == yaml.SequenceNode {
		for i, ch := range n.Content {
			if err := expandNode(ch, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return nil
	}
	v, err := expand(n.Value)
	if err != nil {
		return fmt.Errorf("config: key %q: %w", path, err)
	}
	n.Value = v
	return nil
}
func expand(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		if i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				b.WriteByte(s[i])
				i++
				continue
			}
			end += i + 2
			name := s[i+2 : end]
			value, ok := os.LookupEnv(name)
			if !ok {
				return "", fmt.Errorf("environment variable %q is not set", name)
			}
			b.WriteString(value)
			i = end + 1
			continue
		}
		b.WriteByte('$')
		i++
	}
	return b.String(), nil
}
func contains(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}

func validate(c Config) []string {
	var errs []string
	required := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			errs = append(errs, fmt.Sprintf("%s is required", key))
		}
	}
	required("server.base_url", c.Server.BaseURL)
	required("oidc.issuer_url", c.OIDC.IssuerURL)
	required("oidc.client_id", c.OIDC.ClientID)
	required("oidc.client_secret", c.OIDC.ClientSecret)
	required("oidc.session_secret", c.OIDC.SessionSecret)
	validURL := func(key, value string, required bool) {
		if value == "" && !required {
			return
		}
		u, e := url.ParseRequestURI(value)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, key+" must be an absolute http(s) URL")
		}
	}
	validURL("server.base_url", c.Server.BaseURL, true)
	validURL("oidc.issuer_url", c.OIDC.IssuerURL, true)
	if len(c.OIDC.SessionSecret) < 32 {
		errs = append(errs, "oidc.session_secret must be at least 32 bytes")
	}
	if c.Sync.MaxConcurrent < 1 {
		errs = append(errs, "sync.max_concurrent must be at least 1")
	}
	if c.History.MaxItems < 1 {
		errs = append(errs, "history.max_items must be at least 1")
	}
	for key, d := range map[string]Duration{"server.shutdown_timeout": c.Server.ShutdownTimeout, "oidc.session_ttl": c.OIDC.SessionTTL, "sync.git_timeout": c.Sync.GitTimeout, "sync.post_command_timeout": c.Sync.PostCommandTimeout, "notifications.webhook.timeout": c.Notifications.Webhook.Timeout} {
		if d <= 0 {
			errs = append(errs, key+" must be greater than 0")
		}
	}
	if c.Log.Level != "debug" && c.Log.Level != "info" && c.Log.Level != "warn" && c.Log.Level != "error" {
		errs = append(errs, "log.level must be debug, info, warn, or error")
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		errs = append(errs, "log.format must be json or text")
	}
	for key, p := range map[string]string{"storage.data_dir": c.Storage.DataDir, "storage.logs_dir": c.Storage.LogsDir, "storage.repos_root": c.Storage.ReposRoot} {
		if !filepath.IsAbs(p) {
			errs = append(errs, key+" must be an absolute path")
		}
	}
	if c.Notifications.Telegram.Enabled {
		required("notifications.telegram.bot_token", c.Notifications.Telegram.BotToken)
		required("notifications.telegram.chat_id", c.Notifications.Telegram.ChatID)
	}
	if c.Notifications.Webhook.Enabled {
		validURL("notifications.webhook.url", c.Notifications.Webhook.URL, true)
	}
	return errs
}

// Redacted returns a copy safe for startup logging.
func (c Config) Redacted() Config {
	if c.OIDC.ClientSecret != "" {
		c.OIDC.ClientSecret = "***"
	}
	if c.OIDC.SessionSecret != "" {
		c.OIDC.SessionSecret = "***"
	}
	if c.Webhooks.Secret != "" {
		c.Webhooks.Secret = "***"
	}
	if c.Notifications.Telegram.BotToken != "" {
		c.Notifications.Telegram.BotToken = "***"
	}
	if c.Notifications.Webhook.Secret != "" {
		c.Notifications.Webhook.Secret = "***"
	}
	if len(c.Notifications.Webhook.Headers) > 0 {
		headers := make(map[string]string, len(c.Notifications.Webhook.Headers))
		for k := range c.Notifications.Webhook.Headers {
			headers[k] = "***"
		}
		c.Notifications.Webhook.Headers = headers
	}
	return c
}

# Configuration

Git Clone Manager loads built-in defaults, then an optional YAML file, then environment overrides. Environment values win when both sources set a field. Unknown YAML keys and invalid values stop startup. A missing config file is allowed if the required settings are supplied by the environment.

The config file is selected with `--config <path>`, then `GITSYNC_CONFIG`, then `/etc/gitsync/config.yaml`. Values in YAML strings may use `${VARIABLE}` expansion; an unset variable is a startup error. Use `$$` for a literal dollar sign. Environment lists are comma-separated.

Start from [`../config.example.yaml`](../config.example.yaml). Its `${...}` placeholders are expanded from the process environment.

## Settings

| YAML key | Environment variable | Default | Notes |
|---|---|---|---|
| `server.listen` | `GITSYNC_SERVER_LISTEN` | `:8080` | HTTP listen address. |
| `server.base_url` | `GITSYNC_SERVER_BASE_URL` | required | Public URL without trailing slash. Controls redirect/cookie behavior and webhook URLs. Use HTTPS in production. |
| `server.shutdown_timeout` | `GITSYNC_SERVER_SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown limit. |
| `storage.data_dir` | `GITSYNC_STORAGE_DATA_DIR` | `/data` | bbolt database and default log directory. |
| `storage.repos_root` | `GITSYNC_STORAGE_REPOS_ROOT` | `/repos` | Suggested clone location in the UI. |
| `storage.logs_dir` | `GITSYNC_STORAGE_LOGS_DIR` | `<data_dir>/logs` | Absolute path for console files. |
| `log.level` | `GITSYNC_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `log.format` | `GITSYNC_LOG_FORMAT` | `json` | `json` or `text`. |
| `oidc.issuer_url` | `GITSYNC_OIDC_ISSUER_URL` | required | OIDC discovery issuer URL. |
| `oidc.client_id` | `GITSYNC_OIDC_CLIENT_ID` | required | OIDC client identifier. |
| `oidc.client_secret` | `GITSYNC_OIDC_CLIENT_SECRET` | required | Keep secret in deployment secret storage. |
| `oidc.redirect_url` | `GITSYNC_OIDC_REDIRECT_URL` | `<base_url>/auth/callback` | Register this exact callback at the IdP. |
| `oidc.scopes` | `GITSYNC_OIDC_SCOPES` | `openid,profile,email` | `openid` is always included. |
| `oidc.session_secret` | `GITSYNC_OIDC_SESSION_SECRET` | required | At least 32 bytes; use a unique random value. |
| `oidc.session_ttl` | `GITSYNC_OIDC_SESSION_TTL` | `12h` | Session cookie lifetime. |
| `sync.max_concurrent` | `GITSYNC_SYNC_MAX_CONCURRENT` | `3` | Minimum 1. |
| `sync.git_timeout` | `GITSYNC_SYNC_GIT_TIMEOUT` | `30m` | Timeout per Git command. |
| `sync.post_command_timeout` | `GITSYNC_SYNC_POST_COMMAND_TIMEOUT` | `10m` | Default post-command timeout; repository values may override it. |
| `sync.env_passthrough` | `GITSYNC_SYNC_ENV_PASSTHROUGH` | empty | Comma-separated environment variable names permitted into post commands. OIDC, server, webhook, and notification secret namespaces are excluded. |
| `history.max_items` | `GITSYNC_HISTORY_MAX_ITEMS` | `50` | Finished activities kept per repository. |
| `webhooks.secret` | `GITSYNC_WEBHOOKS_SECRET` | empty | Global fallback when a repository has no webhook secret. |
| `notifications.telegram.enabled` | `GITSYNC_NOTIFICATIONS_TELEGRAM_ENABLED` | `false` | Enables Telegram failure notifications. |
| `notifications.telegram.bot_token` | `GITSYNC_NOTIFICATIONS_TELEGRAM_BOT_TOKEN` | empty | Required when Telegram is enabled. |
| `notifications.telegram.chat_id` | `GITSYNC_NOTIFICATIONS_TELEGRAM_CHAT_ID` | empty | Required when Telegram is enabled. |
| `notifications.webhook.enabled` | `GITSYNC_NOTIFICATIONS_WEBHOOK_ENABLED` | `false` | Enables generic webhook notifications. |
| `notifications.webhook.url` | `GITSYNC_NOTIFICATIONS_WEBHOOK_URL` | empty | Required when enabled; HTTP or HTTPS. Prefer HTTPS. |
| `notifications.webhook.headers` | YAML only | `{}` | Static headers; values may use `${VARIABLE}` expansion. Do not commit credentials. |
| `notifications.webhook.secret` | `GITSYNC_NOTIFICATIONS_WEBHOOK_SECRET` | empty | Optional HMAC-SHA256 payload signature. |
| `notifications.webhook.timeout` | `GITSYNC_NOTIFICATIONS_WEBHOOK_TIMEOUT` | `10s` | Also used for Telegram requests. |

## Repository settings

Repository values are managed through the authenticated UI/API. Git URLs must use HTTP(S) without embedded credentials. Enter a personal access token in the repository access-token field; it is write-only and the API reports only whether one is set. Branches are passed to Git as arguments. Clone paths must be absolute, normalized, outside `storage.data_dir`, and must not overlap another repository path.

Each repository gets a random webhook secret. The authenticated UI displays it so it can be copied into the provider configuration. Clearing it makes the configured global `webhooks.secret` the fallback; if both are empty, webhook requests are rejected.

## Storage and logs

The bbolt database is `<data_dir>/gitsync.db`. Logs are stored at `<logs_dir>/<repository UUID>/<activity UUID>.log`; newly created directories and files use modes `0750` and `0640`. Logs are plain text and may contain sensitive output from Git hooks or administrator-supplied post commands. Restrict access to the volumes and retention backups accordingly.

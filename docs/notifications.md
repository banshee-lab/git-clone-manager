# Notifications

Git Clone Manager can send global notifications when an activity fails or is interrupted. Configure Telegram, a generic HTTP webhook, or both in YAML; secret fields can be populated with `${VARIABLE}` expansion. Environment overrides are also available for supported scalar fields. See [configuration](configuration.md) for all keys.

```yaml
notifications:
  telegram:
    enabled: true
    bot_token: "${TELEGRAM_BOT_TOKEN}"
    chat_id: "${TELEGRAM_CHAT_ID}"
  webhook:
    enabled: false
    url: "https://alerts.example.com/gitsync"
    headers:
      Authorization: "Bearer ${ALERTS_TOKEN}"
    secret: "${NOTIFICATION_WEBHOOK_SECRET}"
    timeout: 10s
```

The webhook receives a JSON `sync.failed` or `sync.interrupted` event with repository and activity metadata, a link, and the last 40 log lines. When `secret` is set, Git Clone Manager adds `X-Git Clone Manager-Signature: sha256=<hex>` containing an HMAC-SHA256 of the exact request body. Telegram messages escape HTML. Repository access tokens are masked in log tails before notification. Delivery is retried up to three times with backoff and does not block or change the sync result; only the channel and HTTP status are logged for failures.

Use HTTPS for notification endpoints and protect any authorization headers and bot tokens as secrets. Notification payloads can include administrator-supplied command output, so treat receiving systems and their retention as sensitive.

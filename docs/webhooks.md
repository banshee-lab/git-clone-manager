# Webhooks

Webhook endpoints do not use an OIDC session. Each request must be authenticated with the per-repository secret, or the global fallback secret if the repository secret is empty. GitSync has no rate limiter for `/hooks/*`; put a rate-limiting reverse proxy in front of it. The receiver caps request bodies at 25 MiB.

Copy the repository's webhook URL and secret from the authenticated repository detail page. Configure the provider to send events to `https://<your-host>/hooks/<repository UUID>`. Use TLS at the reverse proxy. Rotate a secret by editing the repository, then update the provider.

## GitHub

Create a repository webhook with content type `application/json`, select **Just the push event**, and set the secret to the GitSync webhook secret. GitHub's `X-Hub-Signature-256` HMAC-SHA256 signature is verified against the raw body. `ping` receives a successful ignored response; only `push` events are considered. Tag pushes and pushes to a different tracked branch are acknowledged and ignored.

## GitLab

Add a project webhook at the GitSync URL, set the secret token to the GitSync webhook secret, and enable **Push events**. GitSync checks `X-Gitlab-Token` and accepts only `Push Hook` events. Tag refs and pushes to another tracked branch are acknowledged and ignored.

## Generic source

Send an HTTP POST with the configured secret in `X-Token`. Generic requests trigger a sync without provider event or branch filtering.

```sh
curl -X POST \
  -H 'Content-Type: application/json' \
  -H 'X-Token: REPLACE_WITH_REPOSITORY_SECRET' \
  --data '{}' \
  'https://gitsync.example.com/hooks/REPOSITORY_UUID'
```

## Responses

| Status | Meaning |
|---|---|
| `202 Accepted` | A sync was queued; response includes the activity ID. |
| `200 OK` | Authenticated event was intentionally ignored (for example, GitHub ping or another branch). |
| `400 Bad Request` | Invalid body or provider payload. |
| `401 Unauthorized` | Missing or invalid webhook credentials/signature. |
| `403 Forbidden` | No repository or global secret is configured. |
| `404 Not Found` | Repository ID is unknown. |
| `409 Conflict` | This repository already has a queued or running sync. |
| `413 Content Too Large` | Body exceeds 25 MiB. |

Webhook outcomes are logged with repository ID, source, and outcome. Payloads and credentials are not logged.

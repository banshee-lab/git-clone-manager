# OIDC setup

GitSync requires OIDC. Every successfully authenticated identity has the `admin` role; configure access policy in the identity provider so only trusted operators can sign in. GitSync does not provide a second role or local password login.

## Pocket ID

Create an OIDC client in Pocket ID with the redirect URI:

```text
https://gitsync.example.com/auth/callback
```

Set `server.base_url` to the public HTTPS origin. Configure the issuer URL, client ID, client secret, and a unique random session secret of at least 32 bytes. The default scopes are `openid profile email`; `openid` is always enforced. Store secrets outside source control, such as in the deployment environment or a protected YAML file.

GitSync uses authorization code flow with PKCE, state, and nonce validation. The session cookie is signed and encrypted, `HttpOnly`, `SameSite=Lax`, and marked `Secure` when `server.base_url` uses HTTPS. Logout clears the local session cookie; it does not initiate IdP logout.

## Local Dex

The Compose stack starts Dex with a static development account. Use `admin@example.com` / `password` for local testing only. The callback is `http://localhost:8080/auth/callback` in the Compose example. Configure the hostname `dex` to resolve to the host loopback address so the browser and containers can reach the local issuer. Replace the client secret, session secret, and static user before any shared deployment.

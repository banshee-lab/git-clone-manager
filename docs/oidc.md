# OIDC setup

Git Clone Manager requires OIDC. Every successfully authenticated identity has the `admin` role; configure access policy in the identity provider so only trusted operators can sign in. Git Clone Manager does not provide a second role or local password login.

## Pocket ID

Create an OIDC client in Pocket ID with the redirect URI:

```text
https://gitsync.example.com/auth/callback
```

Set `server.base_url` to the public HTTPS origin. Configure the issuer URL, client ID, client secret, and a unique random session secret of at least 32 bytes. The default scopes are `openid profile email`; `openid` is always enforced. Store secrets outside source control, such as in the deployment environment or a protected YAML file.

Git Clone Manager uses authorization code flow with PKCE, state, and nonce validation. The session cookie is signed and encrypted, `HttpOnly`, `SameSite=Lax`, and marked `Secure` when `server.base_url` uses HTTPS. Logout clears the local session cookie; it does not initiate IdP logout.

## Local mock provider

The Compose stack starts `mock-oauth2-server` with interactive login disabled. Visiting the sign-in flow automatically returns a fixed development identity: `admin@example.com` (`gitsync-local-admin`). The callback is `http://localhost:8080/auth/callback` and the issuer is `http://localhost:5556/default`. Both Compose services use host networking, so ports `8080` and `5556` must be available on the host. Token claims are configured in `deploy/mock-oauth2-server/config.json`. This mock provider is only for local testing; replace the client secret, session secret, and provider before any shared deployment.

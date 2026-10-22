# Installation with Docker Compose

Git Clone Manager requires an OIDC provider. Create a confidential OIDC client and register the callback URL `https://gitsync.example.com/auth/callback` (replace the host with the public URL you will use). The issuer URL must be the provider's OIDC discovery issuer, not its discovery document URL. See the [OIDC setup guide](oidc.md).

Create a directory for the deployment and save this as `compose.yaml`:

```yaml
services:
  gitsync:
    # Replace OWNER with the image publisher. The published image name is TBD.
    image: ghcr.io/banshee-lab/git-clone-manager:latest
    restart: unless-stopped
    environment:
      GITSYNC_SERVER_BASE_URL: ${GITSYNC_SERVER_BASE_URL:?set the public HTTPS URL}
      GITSYNC_OIDC_ISSUER_URL: ${GITSYNC_OIDC_ISSUER_URL:?set the OIDC issuer URL}
      GITSYNC_OIDC_CLIENT_ID: ${GITSYNC_OIDC_CLIENT_ID:?set the OIDC client ID}
      GITSYNC_OIDC_CLIENT_SECRET: ${GITSYNC_OIDC_CLIENT_SECRET:?set the OIDC client secret}
      GITSYNC_OIDC_SESSION_SECRET: ${GITSYNC_OIDC_SESSION_SECRET:?set a random session secret}
      # Defaults shown for clarity; these paths are also built into the image.
      GITSYNC_SERVER_LISTEN: :8080
      GITSYNC_STORAGE_DATA_DIR: /data
      GITSYNC_STORAGE_LOGS_DIR: /data/logs
      GITSYNC_STORAGE_REPOS_ROOT: /repos
    ports:
      - "8080:8080"
    volumes:
      - gitsync-data:/data
      - gitsync-repos:/repos

volumes:
  gitsync-data:
  gitsync-repos:
```

Create a `.env` file beside `compose.yaml` and set the values for your OIDC provider and public URL:

```dotenv
GITSYNC_SERVER_BASE_URL=https://gitsync.example.com
GITSYNC_OIDC_ISSUER_URL=https://id.example.com
GITSYNC_OIDC_CLIENT_ID=gitsync
GITSYNC_OIDC_CLIENT_SECRET=replace-with-the-client-secret
GITSYNC_OIDC_SESSION_SECRET=replace-with-at-least-32-random-bytes
```

Generate a session secret, for example, with `openssl rand -hex 32`. Keep `.env` private and out of version control. The OIDC client must allow the exact redirect URI `${GITSYNC_SERVER_BASE_URL}/auth/callback`. Configure the reverse proxy to forward HTTPS traffic to port 8080; set the published port to a different host port if needed. Do not expose the application directly to the internet over plain HTTP.

Start and inspect the service:

```sh
docker compose up -d
docker compose ps
docker compose logs -f gitsync
```

Open the public HTTPS URL and sign in through the configured identity provider. Both named volumes are persistent and should be included in your backup plan. The container runs as UID/GID `10001`, so any bind-mounted `/data` or `/repos` directories must be writable by that identity. Configure optional notifications with the environment variables in the [configuration reference](configuration.md).

The `ghcr.io/OWNER/git-clone-manager:latest` image is a placeholder until the project publishes an image. Replace it with the actual published image reference. To build and run from a source checkout instead, see [deployment](deployment.md) and the repository's development Compose setup.

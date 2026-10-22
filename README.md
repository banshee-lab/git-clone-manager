# Git Clone Manager

Git Clone Manager is a self-hosted service that clones and synchronizes Git repositories on demand or from authenticated GitHub, GitLab, and generic webhooks. It includes an OIDC-protected dashboard, live activity logs, and optional failure notifications.

## Getting started

Docker Compose builds the frontend and Go server, then starts Git Clone Manager with a local mock OIDC provider. On a fresh checkout:

```sh
docker compose up --build
```

The local Compose services use host networking, so the browser and Git Clone Manager reach the mock provider at `http://localhost:5556` without a host-name mapping. Open `http://localhost:8080`; sign-in automatically returns the development identity `admin@example.com` (no password prompt). This mock provider is for local testing only. See [local Compose and openSUSE notes](docs/docker-compose-opensuse.md).

Production deployment must use HTTPS, a production OIDC client, unique secrets, persistent `/data` and `/repos` volumes, and a reverse proxy. See [deployment](docs/deployment.md), [OIDC setup](docs/oidc.md), and [configuration](docs/configuration.md).

For a production Docker Compose example, including required environment variables, see the [installation guide](docs/install.md).

## Features

- Register, edit, remove, and manually synchronize repositories individually or in bulk.
- Trigger synchronization with GitHub push, GitLab Push Hook, or generic authenticated webhooks.
- Keep activity metadata in bbolt and console output in configurable plain files, with per-repository history cleanup.
- Follow current activity and console output through Server-Sent Events.
- Authenticate with OIDC. Every authenticated account has the `admin` role.
- Send optional global Telegram and signed webhook notifications when a sync fails or is interrupted.
- Build and run as a non-root Docker image; the image builds the frontend and embeds it in the Go server.

Post-sync commands are configured by an administrator and run through `sh -c` in the repository directory with a sanitized environment. Treat repositories and their post commands as trusted code. Mounting the Docker socket grants root-equivalent control over the host.

## Configuration and operations

Configuration can come from YAML and environment variables. Environment values override YAML. Start with [`config.example.yaml`](config.example.yaml); the [configuration reference](docs/configuration.md) documents keys, environment variables, defaults, and secret handling.

See the [webhook guide](docs/webhooks.md), [notification guide](docs/notifications.md), and [security review](docs/security.md). The [requirements matrix](docs/requirements-matrix.md) maps the project brief to implementation and tests. The [acceptance procedure](docs/acceptance.md) describes the manual browser and deployment pass.

## Development

The app listens on `:8080` by default. Local Go runs require a valid OIDC configuration and the built frontend assets; the supported complete build and test flow is Docker Compose:

```sh
docker compose up --build
docker build --target go-test .
docker build --target web-test .
```

## License

Git Clone Manager is licensed under [GPL-3.0-or-later](LICENSE).

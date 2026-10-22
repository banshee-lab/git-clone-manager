# Deployment

GitSync is distributed as a Docker image. Build it with `docker compose up --build` or `docker build -t gitsync .`; frontend assets are built in the image build and embedded in the Go binary. Runtime data is separate from the image.

## Volumes and identity

Persist `/data` (bbolt database and, by default, logs) and `/repos` (clones). The container runs as UID/GID `10001`; bind-mounted directories must be writable by that identity. Named volumes are simplest. If logs need a separate volume, set `GITSYNC_STORAGE_LOGS_DIR` to its mount path. Back up data with the service stopped or use a filesystem snapshot that captures database and logs consistently.

Only one GitSync process may use a given bbolt database. Do not scale the service to multiple replicas against the same `/data` volume.

## Reverse proxy and HTTPS

Terminate TLS at a trusted reverse proxy and set `server.base_url` to the public HTTPS URL. Allow long-lived connections to `/api/events` and `/api/activities/*/stream`; disable response buffering for those paths and configure proxy idle/read timeouts to permit the 15-second heartbeat. Preserve the `Origin` header so unsafe API requests can be checked by cross-origin protection. Apply rate limiting to `/hooks/*`, which has no built-in rate limiter.

The container's health check uses `/healthz`. Restrict direct access to the container port when the proxy is the intended public entry point.

## openSUSE 16 and SELinux

SELinux is enforcing by default. Named volumes work without host labeling changes. For bind mounts, apply a container label (for example, `:z`) and assign ownership to UID 10001. Check enforcement with `getenforce`. The [Compose notes](docker-compose-opensuse.md) include examples.

If post-sync commands require Docker, mount `/var/run/docker.sock` and provide its group ID to the container. On openSUSE 16, that socket mount also requires `security_opt: ["label=disable"]`. Access to the Docker socket grants root-equivalent control of the host; only enable it for repositories whose post commands are trusted.

## Operational security

Post commands are administrator-supplied shell commands executed as the container user with access to the repository and mounted volumes. Their environment is sanitized, but the commands retain the container user's filesystem and network privileges. Review commands and repository write access accordingly. Activity logs are plain files and may contain sensitive command output. Keep volume access restricted and define backup retention to match your data policy.

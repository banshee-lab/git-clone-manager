# Docker Compose on openSUSE 16

The local stack uses host networking so Git Clone Manager and the browser can both reach the mock OIDC provider at `http://localhost:5556`. This means ports `8080` and `5556` must be available on the host. The stack also uses named volumes for `/data` and `/repos`; Docker manages their ownership for the container's non-root UID 10001.

SELinux is enforcing by default. Check with `getenforce`. Named volumes work as-is. If you bind-mount a host directory, label it for container use with `:z` and give UID 10001 ownership, for example:

```yaml
volumes:
  - /srv/gitsync-data:/data:z
```

```sh
sudo chown 10001:10001 /srv/gitsync-data
```

To let post-sync commands use Docker, uncomment the Docker socket mount and `group_add` in `docker-compose.yml`. Find the socket's group ID and export it before starting Compose:

```sh
stat -c '%g' /var/run/docker.sock
export DOCKER_GID="$(stat -c '%g' /var/run/docker.sock)"
```

On openSUSE, the socket mount also needs `security_opt: ["label=disable"]` on the `gitsync` service. The Docker socket grants broad control over the host; enable it only when a repository's post-sync command needs it.

## Local login

Run `docker compose up --build`. The mock provider automatically signs in as `admin@example.com`; its token claims are configured in `deploy/mock-oauth2-server/config.json`.

The web build uses `node:24-alpine` because SvelteKit in the lockfile requires Node 22.17 or later. This affects only the frontend build stage; Go builds use `golang:1.27-alpine`.

The `go-test` Docker target installs `build-base` and runs the race detector with cgo enabled (Go's default). The production binary is built separately with `CGO_ENABLED=0`.

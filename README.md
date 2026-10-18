# GitSync

A self-hosted service that keeps Git repositories cloned and synchronized. See the implementation plan for the project scope and deployment details.

## Development

```sh
go run ./cmd/gitsync
```

The server listens on `:8080`; check `http://localhost:8080/healthz` for its health status.

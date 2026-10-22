# Repository Guidelines

## Project Structure & Module Organization

Git Clone Manager is a Go service with a SvelteKit frontend. The server entry point is `cmd/gitsync`; backend packages live under `internal/` (API, authentication, configuration, Git execution, persistence, sync orchestration, and webhooks). Backend tests sit beside their packages as `*_test.go`. The frontend is in `web/src`: routes are under `web/src/routes`, shared API and state code under `web/src/lib`, and tests are colocated. Operational and feature documentation is in `docs/`; Docker assets are at the repository root and in `deploy/`.

## Build, Test, and Development Commands

- `docker compose up --build` builds and starts the full local stack, including the mock OIDC provider.
- `docker build --target go-test .` runs Go checks in the project build image.
- `docker build --target web-test .` runs frontend checks in the project build image.
- In `web/`, `npm run dev` starts Vite; `npm run build` builds the frontend; `npm run check` runs Svelte and TypeScript checks; `npm run lint` runs ESLint and Prettier checks; `npm run test:ci` runs Vitest once.
- For backend-only work, `go test ./...` runs the Go test suite when a local Go toolchain is available.

Local server runs need valid OIDC settings and built frontend assets. Use Compose for the complete supported development setup.

## Coding Style & Naming Conventions

Run `gofmt` on changed Go files. Use idiomatic Go names: exported identifiers use `MixedCaps`, package names are short and lowercase, and tests use `Test...` functions. Frontend files use TypeScript and Svelte 5; follow existing component and route conventions, format with Prettier, and keep ESLint and `svelte-check` clean. Keep backend packages focused under `internal/` and put UI-specific shared code in `web/src/lib`.

## Testing Guidelines

Add or update tests alongside the behavior changed. Go tests use the standard `testing` package and `_test.go` filenames. Frontend tests use Vitest with Testing Library and `*.test.ts` filenames. Run the relevant package or frontend checks during development, then use the Docker test targets for the full project checks.

## Commit & Pull Request Guidelines

Recent commits use short imperative subjects, often prefixed with a phase or area (for example, `Harden repository inputs and document operations`). Keep commits focused. Pull requests should summarize user-visible and operational changes, link related issues when available, and include test commands/results. Add screenshots for dashboard or frontend changes and update the relevant docs for configuration or deployment changes.

## Security & Configuration

Use `config.example.yaml` and `docs/configuration.md` for configuration. Never commit credentials, tokens, or production secrets. Keep authentication, webhook verification, and repository input validation in place when changing API behavior; treat configured post-sync commands as trusted code.

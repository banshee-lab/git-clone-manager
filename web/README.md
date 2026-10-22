# Git Clone Manager web

SvelteKit 3 / Svelte 5 static SPA using the RunWisp UI barrel and its Tailwind
v4 theme. Install with Yarn Classic from this directory:

```sh
yarn install
yarn run check
yarn lint
yarn test:ci
yarn build
```

Use `yarn run check` explicitly: Yarn Classic reserves `yarn check` for its
dependency-integrity command, so that shorthand does not invoke the frontend
type/Svelte check script.

`yarn dev` proxies `/api`, `/auth`, `/hooks`, and `/healthz` to the Go server at
`http://localhost:8080`. To preview the frontend shell without the server, copy
`.env.example` to `.env.local`; the built-in development mock supplies the
session, metadata, dashboard, repository list, and health response used by this
phase. The API tests use fetch stubs and `src/test/FakeEventSource.ts`.

The static adapter emits `build/index.html` as the SPA fallback; API requests
and auth redirects remain same-origin.

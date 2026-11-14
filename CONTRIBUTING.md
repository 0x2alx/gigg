# Contributing

Thanks for wanting to help. Issues with clear steps to reproduce are as welcome as pull requests.

## Ground rules

- gigg is licensed under the **AGPL-3.0** (`LICENSE`). Contributions are accepted under the
  **contributor license agreement** in `CLA.md`, which lets the maintainer also offer gigg under other
  terms. Add the agreement line from `CLA.md` to your first pull request.
- Keep changes focused: one subject per pull request, with tests for behaviour changes.
- Security problems: please don't open a public issue. See `SECURITY.md`.

## Building and testing

You need Go 1.27 and Node.js 24, or only Podman/Docker (everything builds in containers).

```sh
# the UI (embedded into the binary) and its tests
(cd web && npm ci && npm run build && npx vitest run)
# formatting, vet, the "no outbound calls" guard, and the Go tests
sh dev/check.sh
# the image
podman build -t gigg:dev .
```

Without local toolchains, run the same steps in containers, for example:

```sh
podman run --rm -v "$PWD":/src -w /src docker.io/library/golang:1.27-alpine sh dev/check.sh
```

More detail, including the end-to-end API test and the UI scenario runner, is in `dev/README.md`.

## Code style

- Go: `gofmt`, standard library first, no new dependencies without a good reason. The server never
  runs commands or makes outbound network calls (`dev/check.sh` enforces this).
- TypeScript/React: strict TypeScript, the existing components and theme tokens (`web/src/theme.css`).
- Text in the UI and docs: short, plain sentences. User-facing pages have a guide page in
  `docs/guide/`, which the in-app help shows too.
- Database changes: a new numbered migration in `internal/store/migrations/`. Never edit the SQL of a
  released migration (its comments are fine).

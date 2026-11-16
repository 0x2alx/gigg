# Developing gigg

Everything below runs with Podman (Docker works the same way: replace `podman` with `docker`). With
Go 1.27 and Node.js 24 installed locally, you can also run the Go and web steps directly.

## Checks and tests

```sh
# gofmt, go vet, the "server never runs commands or calls out" guard, the Go tests
podman run --rm -v "$PWD":/src:ro -v gigg-gomod:/go/pkg/mod -w /src docker.io/library/golang:1.27-alpine sh dev/check.sh
# the web UI: type check, build, unit tests
podman run --rm -v "$PWD":/w -w /w/web docker.io/library/node:24-alpine sh -c "npm ci && npx tsc -b && npx vite build && npx vitest run"
```

CI (`.github/workflows/check.yml`) runs these two, in the same pinned images.

The rest is for when the source is mounted read-only into the container (with a writable tree, plain
`gofmt -w` and `go mod tidy` do the same): `dev/fmt.sh <out-dir>` gofmts a copy of the tree and
writes only the changed files to `<out-dir>`; `dev/tidy.sh <out-dir> [module@version ...]` runs
`go get` and `go mod tidy` on a copy and writes `go.mod` and `go.sum` there; `make sync DEST=<dir>`
puts a clean copy of the source (no `.git`, local dependencies or build output) in `<dir>`, for a
container to mount. `dev/secret-scan.sh` checks staged changes for
anything that looks like a secret (it prints file, line and length only); run it before committing.

## A dev instance

```sh
podman build -t localhost/gigg:dev .
podman network create gigg-dev
podman volume create gigg-devdata
mkdir -p scratch && echo 'gigg-dev-password' > scratch/pw
podman run --rm -v gigg-devdata:/data -v "$PWD/scratch":/s -v "$PWD/dev/e2e":/b:ro localhost/gigg:dev admin batch /b/bootstrap.txt
podman run -d --name gigg-dev --network gigg-dev \
  -e GIGG_LISTEN=:8443 -e GIGG_TLS_CERT=/run/gigg/tls.crt -e GIGG_TLS_KEY=/run/gigg/tls.key \
  -e GIGG_LISTEN_INTERNAL=:8080 -e GIGG_PUBLIC_URL=https://gigg-dev:8443 -e GIGG_ALLOWED_HOSTS=gigg \
  -v "$PWD/scratch/tls.crt":/run/gigg/tls.crt:ro -v "$PWD/scratch/tls.key":/run/gigg/tls.key:ro \
  -v gigg-devdata:/data --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges \
  --health-cmd '["/gigg","healthcheck"]' localhost/gigg:dev
```

`dev/e2e/bootstrap.txt` creates an admin (`ada`, password from `scratch/pw`) and an agent (`scout`) with
a runner key (written to `scratch/scout.token`). The main listener needs a certificate for the name
`gigg-dev`, signed by a CA whose root is in `scratch/ca/root.crt` (any local CA works, for example
mkcert). Without TLS, drop `GIGG_TLS_CERT` and `GIGG_TLS_KEY` and use an `http://` `GIGG_PUBLIC_URL`;
`dev/e2e.sh` and `dev/probe.sh` expect HTTPS on `gigg-dev:8443`, though.

The image runs as UID 65532: with rootless Podman, make `scratch/` writable for it (for example
`podman unshare chown -R 65532 scratch`), so `admin batch` can write the key file, and the TLS key
readable.

The image is `scratch`: there is no shell, so health checks must use the exec form
(`["/gigg","healthcheck"]`).

## End-to-end API test

From a client container on the same network (it needs the dev CA and the bootstrap's files):

```sh
podman build -t localhost/gigg-client dev/client
podman run --rm --user 65532 --network gigg-dev -v "$PWD/scratch":/s:ro -v "$PWD/scratch/ca":/ca:ro \
  -v "$PWD/dev/e2e.sh":/e2e.sh:ro localhost/gigg-client sh /e2e.sh
```

It needs a fresh data volume (it expects the first task to be `GIG-1`): rerun the bootstrap on a new
volume before each run. `--user 65532`: the key file the bootstrap wrote is readable only by the
image's user. `dev/probe.sh` checks the
listeners the same way (TLS, headers, host checks).

## UI scenarios and screenshots

`dev/shot/run.js` drives Chromium through a JSON scenario (log in, click, type, wait, check values,
take screenshots) and fails on page errors and CSP violations. Build its image from
`dev/shot/Containerfile` and run a scenario against the dev instance:

```sh
podman build -t localhost/ui-shot dev/shot
podman run --rm --network gigg-dev -v "$PWD/dev/shot":/shot:ro -v "$PWD/out":/out localhost/ui-shot \
  node /shot/run.js /shot/scenarios/batch.json /out/shots/batch
```

`batch.json` (desktop) and `mobile.json` (phone width) are the regression scenarios. The
documentation's screenshots come from a demo instance; see `dev/demo/README.md`.

# gigg: three stages. Node and Go exist only while building; the image is the static binary.
ARG NODE_IMAGE=docker.io/library/node:24-alpine@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5
ARG GO_IMAGE=docker.io/library/golang:1.27-alpine@sha256:ea30e25680a5470bac02b08bb0e0a735fb75bd0ab7d431b9b95ee0abef362586

FROM ${NODE_IMAGE} AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-fund --no-audit
COPY web/ ./
# the in-app help shows the user guide's pages
COPY docs/guide/ /src/docs/guide/
RUN npm run build

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/
COPY --from=web /src/web/dist/ web/dist/
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -p 2 -trimpath \
      -ldflags "-s -w -X github.com/0x2alx/gigg/internal/version.Version=${VERSION}" \
      -o /out/gigg ./cmd/gigg \
 && mkdir -p /out/data /out/run/gigg \
 && touch /out/run/gigg/tls.crt /out/run/gigg/tls.key   # mount points: the root filesystem is read-only

FROM scratch
COPY --from=build /out/gigg /gigg
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build /out/run/ /run/
USER 65532:65532
ENV GIGG_DATA_DIR=/data
EXPOSE 8080 8443
ENTRYPOINT ["/gigg"]
CMD ["serve"]

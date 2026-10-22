# syntax=docker/dockerfile:1.7

# @sveltejs/kit requires Node >=22.17. Alpine's nodejs package version is
# independent of this build image, so use the permitted Node LTS fallback.
FROM node:24-alpine AS web
RUN apk add --no-cache yarn git
WORKDIR /src/web
COPY web/package.json web/yarn.lock ./
RUN --mount=type=cache,target=/usr/local/share/.cache/yarn \
    yarn install --frozen-lockfile --cache-folder /usr/local/share/.cache/yarn
COPY web/ ./
RUN yarn build

FROM web AS web-test
RUN yarn run check && yarn run lint && yarn run test:ci

FROM golang:1.27-alpine AS go-deps
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

FROM go-deps AS go-test
RUN apk add --no-cache git build-base \
    && git config --global user.name "GitSync Test" \
    && git config --global user.email "gitsync-test@example.invalid" \
    && git config --global push.autoSetupRemote true
COPY . .
COPY --from=web /src/web/build/ ./internal/webui/dist/
RUN --mount=type=cache,target=/root/.cache/go-build \
    go vet ./... && go test ./... -race

FROM go-deps AS build
ARG VERSION=dev
COPY . .
COPY --from=web /src/web/build/ ./internal/webui/dist/
RUN --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/gitsync ./cmd/gitsync

FROM alpine:3 AS final
RUN apk add --no-cache git openssh-client ca-certificates docker-cli docker-cli-compose curl tzdata \
    && git config --system --add safe.directory '*' \
    && addgroup -S -g 10001 gitsync \
    && adduser -S -D -H -u 10001 -G gitsync gitsync \
    && mkdir -p /data /repos /etc/gitsync \
    && chown -R 10001:10001 /data /repos /etc/gitsync
COPY --from=build --chown=10001:10001 /out/gitsync /usr/local/bin/gitsync
USER 10001:10001
ENV GITSYNC_STORAGE_DATA_DIR=/data \
    GITSYNC_STORAGE_LOGS_DIR=/data/logs \
    GITSYNC_STORAGE_REPOS_ROOT=/repos
VOLUME ["/data", "/repos"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/gitsync"]

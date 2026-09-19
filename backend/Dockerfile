# syntax=docker/dockerfile:1.7

# Build stage
FROM golang:1.25-alpine AS builder

ARG RELEASE_VERSION=dev

# Install git for Go modules
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .


# Build the application with optimizations
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/takutakahashi/agentapi-proxy/internal/buildinfo.Version=${RELEASE_VERSION}" -o bin/ccplant main.go

# Extract the standalone Codex binary from the official npm package. Keeping
# Node.js and npm in this throwaway stage lets the API image contain Codex only.
FROM node:22-alpine AS codex

ARG CODEX_VERSION=latest

RUN npm install --global "@openai/codex@${CODEX_VERSION}" && \
    codex_binary="$(find /usr/local/lib/node_modules/@openai/codex/node_modules/@openai \
      -type f -path '*/vendor/*/bin/codex' -print -quit)" && \
    test -n "$codex_binary" && \
    cp "$codex_binary" /codex && \
    chmod +x /codex

# Lightweight runtime for control-plane API and background worker deployments.
# Kubernetes session managers also use this image to supply the CLI to agents.
FROM alpine:3.22 AS api

RUN apk add --no-cache ca-certificates tzdata && \
    runtime_group="$(awk -F: '$3 == 999 { print $1; exit }' /etc/group)" && \
    if [ -z "$runtime_group" ]; then addgroup -S -g 999 agentapi; runtime_group=agentapi; fi && \
    adduser -S -D -H -u 999 -G "$runtime_group" -h /home/agentapi agentapi && \
    mkdir -p /home/agentapi /home/agentapi/workdir && \
    chown -R 999:999 /home/agentapi

COPY --from=builder /app/bin/ccplant /usr/local/bin/ccplant
COPY --from=codex /codex /usr/local/bin/codex

USER 999:999
WORKDIR /home/agentapi/workdir
ENV HOME=/home/agentapi

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -T 2 -O /dev/null http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/ccplant"]
CMD ["server"]

# Compatibility runtime for Docker Compose and local process sessions.
# Kubernetes managers use the api target and inject its CLI into ccplant-agent.
FROM ghcr.io/ccplant/ccplant-agent:assets-068be2ef320224e336d7538c3a700bef AS runtime
COPY --from=builder /app/bin/ccplant /opt/ccplant/bin/ccplant

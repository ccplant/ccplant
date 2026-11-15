# syntax=docker/dockerfile:1.7

FROM debian:bookworm-slim AS download

ARG TARGETARCH
ARG MOLI_VERSION=1.1.24
ARG MOLI_SHA256_AMD64=09e54e48be914df9596e8b9046cd2e1c67d5911761a09b0445488dd5d39c56d6
ARG MOLI_SHA256_ARM64=b4d59ad9e6f9b45cabbe6fe923f3dfc35691f191959d950b95c6570adb617326

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Debian security updates are intentionally resolved at image build time.
# hadolint ignore=DL3008
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libfontconfig1 \
    && rm -rf /var/lib/apt/lists/* \
    && case "$TARGETARCH" in \
      amd64) archive_arch=x86_64; archive_sha="$MOLI_SHA256_AMD64" ;; \
      arm64) archive_arch=aarch64; archive_sha="$MOLI_SHA256_ARM64" ;; \
      *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && archive="moli-${archive_arch}-unknown-linux-gnu.tar.gz" \
    && curl --proto '=https' --tlsv1.2 -fsSL \
      "https://github.com/lexmount/moli/releases/download/v${MOLI_VERSION}/${archive}" \
      -o /tmp/moli.tar.gz \
    && echo "${archive_sha}  /tmp/moli.tar.gz" | sha256sum --check - \
    && mkdir -p /opt/moli \
    && tar -xzf /tmp/moli.tar.gz -C /opt/moli --strip-components=1 \
    && test -x /opt/moli/moli \
    && /opt/moli/moli --version

FROM debian:bookworm-slim

ARG MOLI_VERSION=1.1.24

LABEL org.opencontainers.image.title="ccplant browser" \
      org.opencontainers.image.description="Moli headless browser for ccplant agent sessions" \
      org.opencontainers.image.source="https://github.com/ccplant/ccplant" \
      org.opencontainers.image.url="https://github.com/lexmount/moli" \
      org.opencontainers.image.version="${MOLI_VERSION}" \
      org.opencontainers.image.licenses="Apache-2.0 OR MIT"

# Debian security updates are intentionally resolved at image build time.
# hadolint ignore=DL3008
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      ca-certificates \
      fontconfig \
      fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 999 browser \
    && useradd --uid 999 --gid 999 --no-create-home --home-dir /nonexistent \
      --shell /usr/sbin/nologin browser

COPY --from=download /opt/moli/moli /usr/local/bin/moli
COPY --from=download /opt/moli/LICENSE-APACHE /usr/share/licenses/moli/LICENSE-APACHE
COPY --from=download /opt/moli/LICENSE-MIT /usr/share/licenses/moli/LICENSE-MIT
COPY --from=download /opt/moli/licenses /usr/share/licenses/moli/third-party

USER 999:999

EXPOSE 9222

ENTRYPOINT ["/usr/local/bin/moli"]
CMD ["serve", "--host", "127.0.0.1", "--port", "9222"]

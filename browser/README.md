# ccplant browser image

This directory builds the browser sidecar image used by ccplant agent sessions.
The current implementation packages the official Moli Linux release binary; it
does not build Moli from source or download anything when a session starts.

Published images:

- `ghcr.io/ccplant/ccplant-browser:moli-<version>`: immutable Moli release
- `ghcr.io/ccplant/ccplant-browser:latest`: alias updated by the publish workflow

The image supports `linux/amd64` and `linux/arm64`, runs as UID/GID 999, and
listens on Pod-local `127.0.0.1:9222` by default. It intentionally contains only
a small DejaVu font set. Deployments that require CJK screenshots should derive
a separate image with the required fonts instead of making every session pull
the full font collection.

## Updating Moli

1. Update `MOLI_VERSION` and both archive checksums in `Dockerfile`.
2. Run `scripts/browser-image-tag.sh` and confirm the expected immutable tag.
3. Build both architectures and run the CDP smoke test in
   `.github/workflows/browser-image.yml`.
4. Merge the change. A ccplant release ensures the immutable browser image is
   published, or the workflow can be dispatched manually.

Do not use a floating upstream installer or skip the checksum verification.

# ccplant browser image

This directory builds the browser sidecar image used by ccplant agent sessions.
The current implementation packages the official Moli Linux release binary; it
does not build Moli from source or download anything when a session starts.

Published images:

- `ghcr.io/ccplant/ccplant-browser:moli-<version>-r<revision>`: immutable image release
- `ghcr.io/ccplant/ccplant-browser:latest`: alias updated by the publish workflow

The image supports `linux/amd64` and `linux/arm64`, runs as UID/GID 999, and
listens on Pod-local `127.0.0.1:9222` by default. It contains DejaVu fonts for
Latin text and Noto CJK fonts so that Japanese, Chinese, and Korean text renders
in pages and screenshots instead of falling back to missing-glyph boxes.

## Updating Moli

1. Update `MOLI_VERSION`, reset `IMAGE_REVISION` to `1`, and update both archive
   checksums in `Dockerfile`. For image-only changes, increment `IMAGE_REVISION`.
2. Run `scripts/browser-image-tag.sh` and confirm the expected immutable tag.
3. Build both architectures and run the CDP smoke test in
   `.github/workflows/browser-image.yml`.
4. Merge the change. A ccplant release ensures the immutable browser image is
   published, or the workflow can be dispatched manually.

Do not use a floating upstream installer or skip the checksum verification.

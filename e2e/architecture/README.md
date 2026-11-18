# E2E architecture fitness

This policy covers the API E2E platform and scenarios that exercise session
start, external-pool placement, message observation, search, and TTL cleanup.
Frontend scenarios are scored separately by their own toolchain and are not
part of this backend-facing boundary.

Run from the repository root with the pinned analyzer source used by CI:

```bash
archscore -root e2e -config e2e/architecture/archscore.json > current.json
archscore compare -baseline e2e/architecture/baseline.json -current current.json -fail-on-regression
```

The committed baseline records the score before the first architecture
improvement. Replace it only when an intentional architecture change has been
reviewed; do not overwrite it merely to accept a regression.

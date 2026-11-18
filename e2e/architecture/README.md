# E2E architecture fitness

This policy covers the API E2E platform and scenarios that exercise session
start, external-pool placement, message observation, search, and TTL cleanup.
Frontend scenarios are scored separately by their own toolchain and are not
part of this backend-facing boundary.

Run from the `e2e` directory. The analyzer is fetched directly from the public
`github.com/takutakahashi/archscore` repository and pinned to the commit used
to produce the baseline:

```bash
go run github.com/takutakahashi/archscore/cmd/archscore@5996adf42a447be66472461182d7442edc6e3df2 \
  -root . -config architecture/archscore.json > current.json
go run github.com/takutakahashi/archscore/cmd/archscore@5996adf42a447be66472461182d7442edc6e3df2 \
  compare -baseline architecture/baseline.json -current current.json \
  -fail-on-regression
```

The committed baseline records the score before the first architecture
improvement. Replace it only when an intentional architecture change has been
reviewed; do not overwrite it merely to accept a regression.

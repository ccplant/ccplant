# Architecture fitness baseline

`baseline.json` records the backend architecture before incremental cleanup
started. The policy in `archscore.json` is observational: existing boundary
violations remain visible, while CI rejects score loss, new violations, new
cycles, reduced coverage, and significant confidence loss.

The analyzer source is pinned under `../tools/archscore`. To inspect a local
report, build that module first:

```sh
cd tools/archscore
go build -o /tmp/archscore ./cmd/archscore
cd ../..
/tmp/archscore -root . -config architecture/archscore.json > architecture-current.json
/tmp/archscore compare \
  -baseline architecture/baseline.json \
  -current architecture-current.json \
  -fail-on-regression
```

An analysis exit code of `2` means known violations or cycles are still
present. Generate the report before running `compare`; the comparison result
is the incremental CI gate.

# archscore source snapshot

This directory contains the architecture analyzer used by backend CI. It is a
source snapshot of `github.com/takutakahashi/archscore` at commit
`5996adf42a447be66472461182d7442edc6e3df2`.

The snapshot keeps the architecture gate self-contained because the upstream
repository is private and is not accessible to pull-request GitHub Actions.
Update this directory and regenerate `../../architecture/baseline.json`
together when adopting an incompatible report version.

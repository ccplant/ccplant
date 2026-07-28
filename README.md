# ccplant

`agentapi-proxy` と `agentapi-ui` を一体で開発・リリースするモノレポです。

## Structure

- `backend/` — Go製のAgentAPI proxyとCLI
- `frontend/` — Next.js製のWeb UI
- `chart/ccplant/` — backend/frontendをまとめて配布するHelm Chart

## Development

```bash
make backend-test
make frontend-test
make chart-test
```

ローカル起動にはDocker Composeを利用できます。

```bash
docker compose up --build
```

## Release

`vX.Y.Z` タグから、1つのGitHub Releaseとして以下を発行します。

- GoReleaserでビルドした`agentapi-proxy`バイナリ
- `ghcr.io/ccplant/ccplant-backend:vX.Y.Z`
- `ghcr.io/ccplant/ccplant-frontend:vX.Y.Z`
- `oci://ghcr.io/ccplant/charts/ccplant:X.Y.Z`

新モノレポを正とし、`backend/`と`frontend/`はそれぞれ旧リポジトリへ
同期PRとしてバックポートします。詳細は[移行手順](docs/migration.md)を参照してください。

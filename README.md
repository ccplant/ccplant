# ccplant

`agentapi-proxy` と `agentapi-ui` を一体で開発・リリースするモノレポです。

## Structure

- `backend/` — Go製のAgentAPI proxyとCLI
- `frontend/` — Next.js製のWeb UI
- `backend/helm/agentapi-proxy/` — backendのHelm Chart（旧リポジトリ互換）
- `frontend/helm/agentapi-ui/` — frontendのHelm Chart（旧リポジトリ互換）
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
- `oci://ghcr.io/ccplant/charts/agentapi-proxy:vX.Y.Z`
- `oci://ghcr.io/ccplant/charts/agentapi-ui:vX.Y.Z`
- `oci://ghcr.io/ccplant/charts/ccplant:X.Y.Z`

Helm Chartは旧リポジトリのchart名、values、テンプレートおよび`v`付きの
version/appVersionを維持してbackendとfrontendを個別にリリースします。
統合`ccplant` Chartも引き続き同時にリリースします。

新モノレポを正とし、`backend/`と`frontend/`はそれぞれ旧リポジトリへ
同期PRとしてバックポートします。詳細は[移行手順](docs/migration.md)を参照してください。

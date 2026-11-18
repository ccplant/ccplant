# ccplant

`agentapi-proxy` と `agentapi-ui` を一体で開発・リリースするモノレポです。

## Structure

- `backend/` — Go製のAgentAPI proxyとCLI
- `frontend/` — Next.js製のWeb UI
- `docs/` — MarkdownとVitePressで構築する公開ドキュメントサイト
- `backend/helm/agentapi-proxy/` — `backend` Helm Chart
- `frontend/helm/agentapi-ui/` — `frontend` Helm Chart
- `chart/ccplant/` — backend/frontendをまとめて配布するHelm Chart

## Development

`OPENAI_BASE_URL` and `ANTHROPIC_BASE_URL` set on the proxy process are used as
system-wide defaults when an OpenAI-compatible or Anthropic-compatible API
connection is saved without an explicit Base URL. They are not propagated to
sessions that use other connection modes.

```bash
make backend-test
make frontend-test
make docs-build
make chart-test
```

ローカル起動にはDocker Composeを利用できます。

```bash
docker compose up --build
```

ドキュメントを編集・プレビューする場合は次を実行します。

```bash
cd docs
bun install
bun run dev
```

`main`ブランチへ反映されたドキュメントはGitHub Actionsにより
[GitHub Pages](https://ccplant.github.io/ccplant/)へデプロイされます。

## Session profile team inheritance

Session profiles support `config.inherit_team_settings` (default: `true`).
Set it to `false` in the profile's inheritance settings to skip team-derived MCP
servers, plugins, environment and authentication. Personal settings and
credentials, common defaults and explicit profile settings remain available.
This also disables `settings_team_id` and the personal `preferred_team_id` for
that session; it does not edit either setting or any team's configuration.
Omitting the option preserves existing behavior. Explicit source-profile
settings and profile instructions continue to apply.

Organisation and proxy Codex hooks run from the managed requirements layer.
Identical copies are removed from user hooks only after that layer is written;
custom user hooks keep their normal Codex approval requirements.

## Release

`vX.Y.Z` タグから、1つのGitHub Releaseとして以下を発行します。

- GoReleaserでビルドした`agentapi-proxy`バイナリ
- `ghcr.io/ccplant/ccplant-backend:vX.Y.Z`
- `ghcr.io/ccplant/ccplant-frontend:vX.Y.Z`
- `oci://ghcr.io/ccplant/charts/backend:vX.Y.Z`
- `oci://ghcr.io/ccplant/charts/frontend:vX.Y.Z`
- `oci://ghcr.io/ccplant/charts/ccplant:X.Y.Z`

分離Helm Chartは旧リポジトリのvalues、テンプレートおよび`v`付きの
version/appVersionを維持し、`backend`と`frontend`の名前で個別にリリースします。
統合`ccplant` Chartも引き続き同時にリリースします。

新モノレポを正とし、`backend/`と`frontend/`はそれぞれ旧リポジトリへ
同期PRとしてバックポートします。

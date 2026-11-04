# ccplant E2E

実backendを検証するGo APIシナリオと、実frontendを検証するPlaywrightシナリオを、共通のAPI・worker・操作画面で実行します。

## ローカル起動

```bash
docker compose -f compose.yaml -f e2e/compose.yaml up --build
```

- ccplant: <http://localhost:3000>
- E2E console: <http://localhost:3001>
- E2E API: <http://localhost:8081>

APIだけを開発する場合:

```bash
cd e2e
go run ./cmd/api
go run ./cmd/worker
```

## MCPから実行する

e2e APIサービスにはstdio MCPモードがあり、`start_e2e`と`reload_e2e_scenarios`を公開します。MCPモードでも同じプロセス内でworker向けAPIを起動するため、認証情報は既存の一時メモリ経由でworkerへ渡されます。対象URLと認証情報はtool引数ではなくMCPプロセスの環境変数へ渡します。

```json
{
  "mcpServers": {
    "ccplant-e2e": {
      "type": "stdio",
      "command": "go",
      "args": ["run", "./cmd/api", "mcp"],
      "cwd": "/path/to/ccplant/e2e",
      "env": {
        "E2E_TARGET_API_URL": "https://dev.ccplant.com/api/v1",
        "E2E_TARGET_FRONTEND_URL": "https://dev.ccplant.com",
        "E2E_AUTH_TYPE": "bearer",
        "E2E_AUTH_VALUE": "..."
      }
    }
  }
}
```

- `E2E_TARGET_API_URL`: シナリオの検証対象API。`start_e2e`には必須
- `E2E_TARGET_FRONTEND_URL`: Playwrightシナリオの検証対象frontend
- `E2E_AUTH_TYPE`: `none`、`bearer`、`api-key`。省略時はtokenがあれば`bearer`、なければ`none`
- `E2E_AUTH_VALUE`: 対象環境のtoken。MCP設定のsecret環境変数として渡す

`reload_e2e_scenarios`は`E2E_CATALOG`（既定値`scenarios/catalog.yaml`）を再読込し、検証に成功した場合だけ稼働中のカタログを差し替えます。すでに作成済みのrunは作成時のscenario定義を使用します。

## シナリオ追加

APIシナリオは`scenarios/api/*_test.go`、画面シナリオは`scenarios/frontend/*.spec.ts`へ追加し、`scenarios/catalog.yaml`へ安定ID、category、日本語description、実行対象を登録します。

## 任意環境を検証する

### API機能シナリオ

APIシナリオは、認証済みユーザー・未認証拒否、セッション検索と作成dry-run、設定マネージャー一覧、プロファイル／スケジュール／Webhook／SlackBot一覧、プロファイル／スケジュール／WebhookのCRUD、不正プロファイル／cron式の拒否、healthを検証します。

```bash
cd e2e
E2E_TARGET_API_URL=https://dev.ccplant.com/api/v1 \
E2E_AUTH_TYPE=bearer \
go test -tags=e2e -v ./scenarios/api -count=1 -timeout 12m
```

`E2E_AUTH_VALUE`は実行プロセスの環境変数に渡してください。値をコマンド引数やファイルに記載しないでください。
CRUDはランダムな`e2e-`接頭辞を持つ専用リソースだけを作成・操作し、失敗時も`Cleanup`で削除を試みます。
スケジュールとWebhookは`paused`、Webhook triggerも無効、プロファイルは非defaultで作成します。
`api.session.schedule-suspend-resume`だけは実セッションを起動し、再利用スケジュールの発火、suspend、同一セッションのresumeを検証します。E2Eコンソールから単独実行すると自動的に有効になります。コマンドから実行する場合は`E2E_PARAM_LIVE_SESSION=true`を指定してください。必要なら`E2E_PARAM_SESSION_PROFILE_ID`で利用するプロファイルを指定できます。指定しない場合は対象環境のdefaultプロファイルを使います。

`api.session.persistence-suspend-resume`も実セッションを起動する永続化E2Eです。Session Managerが2 replica以上、`sessionPersistence.backend: volume`、指定したACP agentが利用可能な環境を対象にします。実セッションを起動して課金されるチャットを1回行い、同時suspend、resume、会話履歴の復元を検証します。E2Eコンソールから単独実行すると自動的に有効になります。コマンドから実行する場合は`E2E_PARAM_LIVE_SESSION=true`を指定してください。`agent_type`の既定値は`claude-acp`です。

workerを介さず直接実行する場合は、実セッションの起動を明示的に許可します。

```bash
E2E_PARAM_LIVE_SESSION=true \
E2E_PARAM_AGENT_TYPE=claude-acp \
go test -tags=e2e -v ./scenarios/api -run '^TestSessionPersistenceSuspendResume$' -count=1 -timeout 8m
```
その他のシナリオは実セッション起動、課金されるチャット、スケジュール発火、Webhook配送、Slack送信を実行しません。
必要な機能が無効化されている環境や権限不足はskipせず失敗として報告します。
通常の`go test ./...`はネットワークシナリオを実行せず、helperとcatalogの整合性を検証します。

操作画面または`POST /api/v1/runs`で、対象のAPI URL、Frontend URL、認証方式（なし・Bearer・API key）を実行ごとに指定します。認証値はSQLite、run、event、ログへ保存されません。APIプロセスの一時メモリからworkerのclaim時に一度だけ渡され、直後に削除されます。5分以内にclaimされない場合も削除してrunをキャンセルします。API再起動やclaim時の通信断では復元できないため、認証情報を再入力して新しいrunを作成してください。

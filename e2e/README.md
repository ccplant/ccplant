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

## シナリオ追加

APIシナリオは`scenarios/api/*_test.go`、画面シナリオは`scenarios/frontend/*.spec.ts`へ追加し、`scenarios/catalog.yaml`へ安定ID、category、日本語description、実行対象を登録します。

## 任意環境を検証する

### API機能シナリオ

APIは計15件です。認証済みユーザー・未認証拒否、セッション検索と作成dry-run、設定マネージャー一覧、プロファイル／スケジュール／Webhook／SlackBot一覧、プロファイル／スケジュール／WebhookのCRUD、不正プロファイル／cron式の拒否、healthを検証します。

```bash
cd e2e
E2E_TARGET_API_URL=https://dev.ccplant.com/api/v1 \
E2E_AUTH_TYPE=bearer \
go test -tags=e2e -v ./scenarios/api -count=1 -timeout 8m
```

`E2E_AUTH_VALUE`は実行プロセスの環境変数に渡してください。値をコマンド引数やファイルに記載しないでください。
CRUDはランダムな`e2e-`接頭辞を持つ専用リソースだけを作成・操作し、失敗時も`Cleanup`で削除を試みます。
スケジュールとWebhookは`paused`、Webhook triggerも無効、プロファイルは非defaultで作成します。
実セッション起動、課金されるチャット、スケジュール発火、Webhook配送、Slack送信はこのsuiteでは実行しません。
必要な機能が無効化されている環境や権限不足はskipせず失敗として報告します。
通常の`go test ./...`はネットワークシナリオを実行せず、helperとcatalogの整合性を検証します。

操作画面または`POST /api/v1/runs`で、対象のAPI URL、Frontend URL、認証方式（なし・Bearer・API key）を実行ごとに指定します。認証値はSQLite、run、event、ログへ保存されません。APIプロセスの一時メモリからworkerのclaim時に一度だけ渡され、直後に削除されます。5分以内にclaimされない場合も削除してrunをキャンセルします。API再起動やclaim時の通信断では復元できないため、認証情報を再入力して新しいrunを作成してください。

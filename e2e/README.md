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

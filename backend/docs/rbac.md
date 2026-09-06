# RBAC (Role-Based Access Control)

agentapi-proxy はAPIキーベースの認可と、ロール別の権限管理、セッション所有権制御を提供します。

## 概要

- **Admin key**: 環境変数 `AGENTAPI_AUTH_ADMIN_KEY` で指定する単一の管理者APIキー
- **Personal API token**: ユーザーごとに発行される名前付きAPIトークン（`/api-tokens`）
- **Bootstrap admin**: 緊急時アクセス用のブレークグラス管理者（`auth.bootstrap_admin`）
- **GitHub認証**: GitHub OAuth / PAT ベースのユーザー認証とチームロールマッピング
- **セッション所有権制御**: ユーザーは自分のセッションのみアクセス可能

> 注: 以前提供していた `auth.static`（api_keys リスト / keys_file）による静的APIキー認証は廃止されました。単一の管理者キーは `AGENTAPI_AUTH_ADMIN_KEY` を使用してください。レガシー設定が残っている場合は起動時に警告が出力され、無視されます。

## 設定方法

### Admin key の指定

環境変数で管理者キーを1つ指定します：

```bash
export AGENTAPI_AUTH_ADMIN_KEY="ap_admin_<random-string>"
```

設定ファイルでも指定できますが、値がバージョン管理に残らないよう環境変数を推奨します：

```yaml
auth:
  admin_key: "ap_admin_<random-string>"
```

このキーで `X-API-Key: <admin key>` ヘッダーを送信すると、管理者ユーザー（`admin-key`）として認証されます。

### Personal API token

一般ユーザー向けのAPIアクセスには、管理者がユーザーごとに発行する名前付きAPIトークンを使用します：

```bash
# 管理者がユーザーのトークンを発行
curl -X POST https://<api>/admin/users/<principalId>/api-tokens \
  -H "X-API-Key: $AGENTAPI_AUTH_ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "ci-token"}'

# レスポンスの plaintext_token を安全に受け渡す（再表示は不可）
```

トークンは `session:create` / `session:read` / `session:update` / `session:delete` の権限を持ち、所有ユーザーのスコープで動作します。取り消しは `DELETE /admin/users/{principalId}/api-tokens/{tokenId}` で行います。

### Bootstrap admin（緊急時アクセス）

外部認証プロバイダを設定する前の初期セットアップ用に、非期限切れの管理者トークンを設定できます：

```yaml
auth:
  bootstrap_admin:
    enabled: true
    user_id: admin-operator
    username: admin-operator
    token: "<long-random-token>"
```

詳細は [Bootstrap admin authentication](bootstrap-admin-authentication.md) を参照してください。

## 使用方法

### X-API-Key ヘッダー

```bash
curl -X POST http://localhost:8080/start \
  -H "X-API-Key: $AGENTAPI_AUTH_ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{}'
```

### Bearer トークン（Authorization ヘッダー）

```bash
curl -X POST http://localhost:8080/start \
  -H "Authorization: Bearer $PERSONAL_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}'
```

認証は優先順位として、まず `X-API-Key` ヘッダーをチェックし、見つからない場合に `Authorization` ヘッダーの Bearer トークンをチェックします。

### セッション所有権制御

- **管理者（admin key / bootstrap admin）**: 全てのセッションにアクセス可能
- **一般ユーザー（personal API token / GitHub認証）**: 自分のセッションのみアクセス可能

```bash
# 管理者は全ユーザーのセッションを検索
curl -X GET http://localhost:8080/search \
  -H "X-API-Key: $AGENTAPI_AUTH_ADMIN_KEY"
```

## 権限の詳細

| 権限 | 説明 | 対応エンドポイント |
|------|------|-------------------|
| `session:create` | セッション作成 | `POST /start` |
| `session:list` | セッション一覧表示・検索 | `GET /search` |
| `session:read` | セッション状態の参照 | `GET /sessions/:sessionId` |
| `session:update` | セッションの更新 | `PATCH /sessions/:sessionId` |
| `session:delete` | セッション削除 | `DELETE /sessions/:sessionId` |
| `admin` | 管理操作（ユーザー管理、システム設定等） | `/admin/*` |

## セキュリティベストプラクティス

1. **強力なキー**: ランダムで十分な長さのキーを使用
2. **環境変数で管理**: `AGENTAPI_AUTH_ADMIN_KEY` は設定ファイルに書かず環境変数で渡す
3. **最小権限の原則**: 通常の利用には personal API token を発行し、admin key の使用を限定
4. **定期的なローテーション**: admin key は環境変数を更新して再起動することでローテーション
5. **ログ監視**: 認証失敗や権限違反のログを監視

## エラーハンドリング

### 認証エラー

```bash
# 無効なAPIキー
HTTP/1.1 401 Unauthorized
{"message": "Authentication required"}
```

### 認可エラー

```bash
# 権限不足
HTTP/1.1 403 Forbidden
{"message": "Insufficient permissions"}

# セッション所有権違反
HTTP/1.1 403 Forbidden
{"message": "Access denied"}
```

## トラブルシューティング

1. **認証が401になる**
   - `AGENTAPI_AUTH_ADMIN_KEY` が設定されているか確認（起動ログに `[AUTH_INIT] Admin key authentication enabled` が出力される）
   - ヘッダー名が `X-API-Key` であることを確認
   - personal API token を使う場合はトークンが失効・取り消されていないか確認

2. **403が返る**
   - ユーザーのロールと権限設定を確認
   - 他ユーザーのリソースにアクセスしていないか確認

3. **レガシーな `auth.static` 設定の警告が出る**
   - static認証は廃止済みです。設定から `auth.static` セクションを削除し、`AGENTAPI_AUTH_ADMIN_KEY` に移行してください
# Principal 認証リンク修復ランブック

このランブックでは、GitHub Principal の削除や Identity のリンク変更後に、ログインで意図しないユーザー ID が使われる場合の確認方法と修復手順を説明します。

## ID の役割

GitHub 認証では、次の ID を区別する必要があります。

- `principal.ID`: 複数の GitHub Identity や他の認証 Identity をまとめる Principal の ID
- `application_user_id`: 既存の個人リソースとの互換性を保つため、認証後の `user.ID()` として使う ID
- GitHub Identity: `(connection_id, github_user_id)` と Principal の対応

Principal に `application_user_id` が設定されている場合、GitHub Identity が目的の Principal に紐づいていても、`GET /user/info` の `principal_id` には `application_user_id` が返ります。空の場合は Principal 自身の ID が使われます。

## `delete-principal` の削除範囲

次のコマンドは、指定した Principal レコードと、その Principal を直接参照する GitHub Identity を削除します。

```bash
ccplant client user delete-principal <principal-id>
```

次のデータは削除・更新しません。

- 別の Principal が保持する `application_user_id`
- 既存のログインセッション
- 個人リソースやその所有者 ID
- Team membership のスナップショット

このため、削除した ID が別の Principal の `application_user_id` に残っていると、その別の Principal でログインしても `GET /user/info` に削除済みの ID が返ることがあります。削除済み Principal が同じ UUID で再生成されたとは限りません。

## 状態の確認

同じ認証情報で次の2つを確認します。

```text
GET /user/info
GET /users/me/github-identities
```

たとえば、前者が `old-user-id`、後者が `existing-principal-id` を返す場合、次の対応が残っている可能性があります。

```text
Principal existing-principal-id
  application_user_id: old-user-id
  identities:
    - connection_id: ...
      github_user_id: ...
```

`/users/me/github-identities` が返す `principal_id` が、残したいPrincipalです。

## 管理コマンドによる修復

### GHECなど既存IdentityのログインIDだけを直す

既存Principalの `application_user_id` を空にします。

```bash
ccplant client user repair-principal <existing-principal-id> \
  --clear-application-user-id
```

次回ログインから、Principal自身のIDが認証後のユーザーIDとして使われます。

### 組み込みGitHub Identityも同じPrincipalへ追加する

`application_user_id` の解除とIdentity追加を一度に実行します。

```bash
ccplant client user repair-principal <existing-principal-id> \
  --clear-application-user-id \
  --identity-connection-id <github-connection-id> \
  --identity-github-user-id <numeric-github-user-id> \
  --identity-login <github-login>
```

数値GitHub User IDはGitHub APIで確認できます。

```bash
gh api users/<github-login> --jq .id
```

対象ConnectionのAPI URLが組み込みGitHubのAPI URLと一致する必要があります。既定のGitHub.com構成では、`base_url` が `https://github.com` のConnectionも互換対象です。

Identityが別Principalにすでに属している場合、APIは `409 Conflict` を返し、何も変更しません。移動元を確認し、再割当てが正しい場合だけ次のフラグを追加します。

```text
--reassign-identity
```

このフラグは既存Identityのアクセストークンデータを保持したまま、参照先Principalを変更します。

## 管理APIによる修復

管理権限を持つ認証情報で次のAPIを呼び出します。

```http
POST /admin/principals/{principalId}/repair
Content-Type: application/json
```

```json
{
  "clear_application_user_id": true,
  "identities": [
    {
      "connection_id": "<github-connection-id>",
      "github_user_id": 123456,
      "login": "<github-login>"
    }
  ]
}
```

既存Identityを別Principalから移す場合だけ、対象Identityに次を指定します。

```json
{
  "reassign_existing": true
}
```

APIは、対象PrincipalとConnectionの存在、GitHub User IDが正の整数であること、リクエスト内に重複Identityがないことを変更前に検証します。

## 実行後の確認

修復前から存在するセッションは古いユーザーIDを保持しているため、対象ユーザーをすべてログアウトさせてからログインし直します。その後、各ログイン方式で次を確認します。

1. `GET /user/info` の `principal_id` が残したPrincipal IDである
2. `GET /users/me/github-identities` のIdentityが残したPrincipal IDを参照している
3. 必要なセッション、設定、Session Profileなどへアクセスできる

確認が終わるまで、残したPrincipalに対して `delete-principal` を実行しないでください。

## 個人リソースへの影響

`repair-principal` はセッションや設定などのリソース所有者を移管しません。`application_user_id` を空にすると、旧 `application_user_id` を所有者とする個人リソースへの互換アクセスはなくなります。旧IDのリソースを引き続き利用する必要がある場合は、解除前に所有権移管の要否を確認してください。

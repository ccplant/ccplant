# Team trigger から個人スコープへセッションをルーティングする設計

## 背景と目的

現在、team scope の Webhook / SlackBot が作るセッションは、トリガー自身の所有情報をそのまま引き継ぐ。

- Webhook: `WebhookSessionService` が `webhook.UserID()`, `webhook.Scope()`, `webhook.TeamID()` を `LaunchUseCase` に渡す
- SlackBot: `SlackBotEventHandler` が `bot.UserID()`, `bot.Scope()`, `bot.TeamID()` を `LaunchUseCase` に渡す

このため、レンダリング後の session tags に `username=<CCPlant username>` があっても、セッションの所有者は変わらない。

本機能では、**team scope の Webhook / SlackBot に限り**、レンダリング後の `username` tag が CCPlant の有効なユーザーに一致し、そのユーザーがトリガーの team に所属する場合、セッションをそのユーザーの user scope で起動する。

## 提案する振る舞い

### ルーティング規則

| Trigger scope | `tags.username` | 解決結果 | Session scope / owner |
|---|---|---|---|
| `user` | なし/あり | 評価しない | 現行どおり trigger owner の `user` scope |
| `team` | なし/空 | 対象外 | 現行どおり `team` scope |
| `team` | あり | active、team member、`session:create` あり | `user` / 解決した user ID |
| `team` | あり | ユーザーなし、inactive、非 member、権限なし | セッションを作らず routing error |

`username` が指定されたのに解決できない場合は team scope へフォールバックしない。typo や退職済みユーザーを共有スコープへ誤配送することを防ぐため、fail-closed とする。

username の比較は、初期実装では `UserRepository.FindByUsername` と同じ完全一致とする。大文字小文字の正規化を導入する場合はユーザー登録側と一緒に一意制約を定義し、ルーター単独では行わない。

## API 設計

既存の `session_config.tags` を利用するため、破壊的な API 変更は不要である。

```json
{
  "name": "Team issue router",
  "scope": "team",
  "team_id": "acme/platform",
  "session_config": {
    "tags": {
      "username": "{{.issue.assignee.login}}",
      "repository": "{{.repository.full_name}}"
    }
  }
}
```

SlackBot でも同様に `session_config.tags.username` を使う。Slack の `<@U...>` は CCPlant username ではないため、Slack user ID から CCPlant username への対応付けは本機能の範囲外である。テンプレートの入力に username を供給する連携、または固定値を利用する。

将来、暗黙の tag semantics を避けたい場合は、後方互換な明示設定を追加できる。

```json
"session_config": {
  "routing": {
    "user_tag": "username",
    "on_unresolved": "reject"
  }
}
```

ただし第一段階では要件どおり予約 tag `username` を使用し、設定項目は増やさない。

## コンポーネント設計

### 1. 共通 identity resolver

`internal/usecases/session/identity_resolver.go` に Webhook / SlackBot 共通の resolver を置く。

```go
type TriggerIdentity struct {
    UserID string
    Scope  entities.ResourceScope
    TeamID string
    Teams  []string
}

type TriggerIdentityRequest struct {
    TriggerUserID string
    TriggerScope  entities.ResourceScope
    TriggerTeamID string
    TriggerTeams  []string
    Tags          map[string]string
}

type TriggerIdentityResolver struct {
    users repositories.UserRepository
}

func (r *TriggerIdentityResolver) Resolve(
    ctx context.Context,
    req TriggerIdentityRequest,
) (TriggerIdentity, error)
```

処理順は次のとおり。

1. trigger scope が `team` 以外なら現行 identity を返す
2. `strings.TrimSpace(tags["username"])` が空なら現行 identity を返す
3. `UserRepository.FindByUsername` で CCPlant user を取得する
4. `user.Status() == active` を検証する
5. `user.UserType() != service_account` を検証する
6. `user.IsMemberOfTeam(triggerTeamID)` を検証する
7. `user.HasPermission(session:create)` を検証する
8. user scope identity を返す
   - `UserID = string(user.ID())`
   - `Scope = user`
   - `TeamID = ""`
   - `Teams = user の現在の全 team memberships`

個人スコープへ切り替わった後も、そのユーザーが通常の API から user-scoped session を作る場合と同じ settings / credentials / default SessionProfile を適用する。そのため `Teams` は Webhook/SlackBot 作成者のキャッシュ値ではなく、解決対象ユーザーの現在の membership から構成する。

### 2. Webhook への組み込み

`WebhookSessionService` に resolver を注入し、tags のテンプレート展開と caller-provided tags の merge が終わった直後に identity を解決する。

解決後の identity を以下へ渡す。

- `LaunchRequest.UserID`, `Scope`, `TeamID`, `Teams`
- memory の owner/scope
- default / selector-based SessionProfile の検索条件
- credential source の既定値

`reuse` と `max_sessions` の検索条件には identity を追加する。現在は tags のみなので、異なるユーザーが同じ repository/thread tag を持つ場合に他人の session を reuse しうるためである。

```go
ReuseIdentity: &entities.SessionIdentityFilter{
    Scope: identity.Scope,
    UserID: identity.UserID,
    TeamID: identity.TeamID,
}
```

または既存 `SessionFilter` の `Scope/UserID/TeamID` を `LaunchUseCase` 内で設定する。

### 3. SlackBot への組み込み

`SlackBotEventHandler` でも tags のレンダリング後、session limit / reuse 判定より前に同じ resolver を呼ぶ。

SlackBot は thread reuse を `LaunchUseCase` の外で処理しているため、次の filter を必須にする。

- `slack_channel`
- `slack_thread_ts`
- 解決後の `Scope/UserID/TeamID`

同一 Slack thread の途中で `username` が変わった場合は、別 owner の session として新規起動する。既存 session へ別ユーザーのメッセージを流さない。

非同期 goroutine の前に identity 解決を完了し、routing error を Slack thread に返信できるようにする。エラー文には username の存在は含めてよいが、ユーザーの status や membership の詳細は漏らさず、`username could not be routed to a personal session` のような同一メッセージにする。

### 4. DI

`proxyServer` が保持する `UserRepository` を次へ注入する。

- `webhook.NewHandlers(..., userRepo)`
- `slackbot.NewSlackBotEventHandler(..., userRepo)`

`cmd/server.go` の Webhook handler / Slack Socket manager の両方を更新する。現在の `MemoryUserRepository` はプロセスローカルなので、複数 replica で username 解決を保証するには user repository の永続化または共有キャッシュが前提となる。すでに認証ユーザーが replica 間で共有される構成なら、その実体を注入する。

## セキュリティと認可

- **team trigger のみ**が owner を指定できる。user-owned trigger から別ユーザーへの impersonation は禁止する。
- 対象ユーザーは trigger の `team_id` の member に限定する。admin であっても membership がなければ対象外とする。
- active status と `session:create` permission を delivery 時点で再評価する。
- service account は個人 scope の対象外とする。
- `username` tag は session に残し、監査可能にする。さらに以下の system tags を追加する。
  - `trigger_scope=team`
  - `trigger_team_id=<team>`
  - `routed_username=<username>`
  - `routing_source=webhook|slackbot`
- ログには webhook/slackbot ID、team ID、username、結果、session ID を構造化して残す。秘密や payload 全体は記録しない。

## エラーと delivery 記録

resolver の sentinel error を定義する。

- `ErrRoutingUserNotFound`
- `ErrRoutingUserIneligible`
- `ErrRoutingUserNotTeamMember`
- `ErrRoutingUserPermissionDenied`

外部応答は情報漏えい防止のため一律の routing error に変換する。内部ログと Webhook delivery record では error code を保持する。

- Webhook: delivery status を `failed`、session ID は空にする
- SlackBot: session を作らず thread に汎用エラーを通知する
- dry-run: resolved `scope`, `user_id`, `team_id` と routing error を返す。ただし API caller が team resource を閲覧できる既存認可を通過していること

## 競合・再利用・上限

`max_sessions` の意味は trigger 全体の上限を維持する。つまり個人 routing 後も `webhook_id` / `slackbot_id` 単位の総数で制限する。

一方、reuse は必ず owner identity を含める。これにより以下を防止する。

- 同一 tag を持つ別ユーザーの session の reuse
- team session と personal session の混同
- Slack thread 中の routing target 変更時の越境

SlackBot の pending dedup key も `channel:thread:scope:userID/teamID` にする。target が異なるイベントを誤って drop しないためである。

## テスト計画

### Resolver unit tests

- team + username なしは team identity のまま
- user trigger + 他人の username は無視
- active member + session:create は user identity へ変換
- unknown / inactive / suspended / service account / non-member / permission なしは reject
- 解決対象ユーザーの全 teams が返る
- username の前後空白を除去する

### Webhook tests

- webhook-level / trigger-level tag merge 後の username を使う
- GitHub / custom webhook の双方で personal session が作られる
- `RunServerRequest` の `UserID=user.ID`, `Scope=user`, `TeamID=""`
- memory / default profile が対象ユーザーの scope で選ばれる
- unresolved 時に delivery failed、session 未作成
- reuse が別 owner の session を選ばない
- max session は trigger 全体で維持される

### SlackBot tests

- rendered username で personal session が作られる
- unresolved 時に session 未作成、Slack に汎用エラー
- 同じ thread・同じ owner は reuse
- 同じ thread・異なる owner は新規 session
- pending dedup key が identity を含む

### Integration tests

- team Webhook/SlackBot 作成者と routing target が異なるケース
- target user の user settings、personal credentials、default SessionProfile が適用される
- target user が team を離れた後は新規 delivery が reject される

## ロールアウト

1. resolver、identity-aware filter、監査 tag を feature flag 配下で実装する
2. dry-run で既存 team triggers の `username` tag 使用状況と解決可否を確認する
3. Webhook で有効化し、delivery failure と session owner を監視する
4. SlackBot で有効化する
5. 問題がなければ feature flag を既定 enabled にする

feature flag 例: `TEAM_TRIGGER_USERNAME_ROUTING_ENABLED=false`。既存設定で偶然 `username` tag をメタデータ用途に使っている場合の挙動変更を避けるため、段階導入を推奨する。

## 実装順序

1. `TriggerIdentityResolver` と unit tests
2. `SessionFilter` / `LaunchUseCase` の identity-aware reuse 対応
3. Webhook 組み込み、delivery / dry-run 拡張
4. SlackBot 組み込み、thread reuse / pending key 修正
5. OpenAPI と management skill の例・仕様更新
6. integration tests と feature flag rollout

## 設計上の判断

- username から直接 UserID を組み立てず、必ず repository で canonical user を取得する
- routing 判定はテンプレート処理ではなく application use case に置き、Webhook と SlackBot で同じ認可を使う
- routing failure は fallback せず reject する
- resource owner（Webhook/SlackBot）は変更せず、作成される Session の identity だけを変更する
- reuse は tags だけでなく identity boundary を含める

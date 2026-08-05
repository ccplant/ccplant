# Team trigger から個人スコープへセッションをルーティングする設計

## 背景と目的

現在、team scope の Webhook / SlackBot が作るセッションは、トリガー自身の所有情報をそのまま引き継ぐ。

- Webhook: `WebhookSessionService` が `webhook.UserID()`, `webhook.Scope()`, `webhook.TeamID()` を `LaunchUseCase` に渡す
- SlackBot: `SlackBotEventHandler` が `bot.UserID()`, `bot.Scope()`, `bot.TeamID()` を `LaunchUseCase` に渡す

このため、レンダリング後の session tags に `username=<CCPlant username>` があっても、セッションの所有者は変わらない。

本機能では、**team scope の Webhook / SlackBot に限り**、レンダリング後の `username` tag がチーム管理の routing allowlist に一致した場合、対応する user ID の user scope でセッションを起動する。

重要: 現在の CCPlant には、Webhook delivery 時に「その username が実在する本人である」「その人が現在も team member である」と証明できる認証・ユーザーディレクトリがない。したがって本設計における検証は本人確認ではなく、**信頼された管理面で事前登録された routing rule との照合**である。

## 提案する振る舞い

### ルーティング規則

| Trigger scope | `tags.username` | 解決結果 | Session scope / owner |
|---|---|---|---|
| `user` | なし/あり | 評価しない | 現行どおり trigger owner の `user` scope |
| `team` | なし/空 | 対象外 | 現行どおり `team` scope |
| `team` | あり | team routing allowlist に完全一致 | `user` / 登録済み user ID |
| `team` | あり | allowlist にない、または mapping が無効 | セッションを作らず routing error |

`username` が指定されたのに解決できない場合は team scope へフォールバックしない。typo や allowlist から削除済みの target を共有スコープへ誤配送することを防ぐため、fail-closed とする。

username の比較は完全一致とする。大文字小文字の正規化は行わない。

## API 設計

イベントから得る値には既存の `session_config.tags` を利用する。加えて、信頼境界となる allowlist を team-owned trigger の設定に明示する。

```json
{
  "name": "Team issue router",
  "scope": "team",
  "team_id": "acme/platform",
  "personal_routing": {
    "tag": "username",
    "targets": {
      "alice": "alice",
      "bob": "user-0187"
    },
    "on_unresolved": "reject"
  },
  "session_config": {
    "tags": {
      "username": "{{.issue.assignee.login}}",
      "repository": "{{.repository.full_name}}"
    }
  }
}
```

`targets` は `CCPlant username -> session owner UserID` の対応であり、Webhook/SlackBot の管理 API からのみ変更できる。Webhook payload や Slack message はこの mapping 自体を変更できない。

SlackBot でも同様に `session_config.tags.username` を使う。Slack の `<@U...>` は CCPlant username ではないため、必要なら `targets` の key に Slack user ID を使い `tag` を `slack_user_id` にする。外部入力をそのまま owner ID として採用してはならない。

`personal_routing` がない既存 trigger は routing を行わない。偶然 `username` tag をメタデータ用途に使っている既存設定の挙動は変わらない。

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

type PersonalRoutingConfig struct {
    Tag          string
    Targets      map[string]string // external value -> canonical UserID
    OnUnresolved string
}

type TriggerIdentityResolver struct {
    // No UserRepository dependency: it is not an identity authority in no-auth mode.
}

func (r *TriggerIdentityResolver) Resolve(
    ctx context.Context,
    req TriggerIdentityRequest,
) (TriggerIdentity, error)
```

処理順は次のとおり。

1. trigger scope が `team` 以外なら現行 identity を返す
2. `personal_routing` がなければ現行 identity を返す
3. `strings.TrimSpace(tags[personalRouting.Tag])` が空なら現行 identity を返す
4. 値を `personalRouting.Targets` で完全一致検索する
5. 未登録なら routing error を返す
6. 登録された canonical UserID で user scope identity を返す
   - `UserID = personalRouting.Targets[value]`
   - `Scope = user`
   - `TeamID = ""`
   - `Teams = []`（認証由来の membership は存在しない）

個人スコープへ切り替わった後は、canonical UserID に紐づく user settings / credentials / default SessionProfile を適用する。これらが未作成でも session 起動自体を禁止する根拠にはしない。必要な credential がなく provision に失敗した場合は通常の session 起動エラーとして扱う。

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

resolver は Webhook / SlackBot entity に永続化された `personal_routing` のみを入力にするため、`UserRepository` の注入は不要である。routing 設定は既存の Kubernetes-backed Webhook / SlackBot repository に保存され、replica 間で共有される。

## セキュリティと認可

- **team trigger のみ**が owner を指定できる。user-owned trigger から別ユーザーへの routing は禁止する。
- incoming event が owner ID を直接指定することは禁止し、必ず静的な `targets` mapping を通す。
- no-auth mode では本人性、在籍、team membership、active status、permission は検証できない。これらを検証済みと表現しない。
- routing の権限根拠は、Webhook/SlackBot 設定を変更できる管理面の credential と webhook signature / Slack Socket Mode の真正性である。
- 管理面にも認証がない deployment では、この機能は security boundary にならない。user scope は isolation ではなく namespace として扱うか、機能を無効化する。
- `username` tag は session に残し、監査可能にする。さらに以下の system tags を追加する。
  - `trigger_scope=team`
  - `trigger_team_id=<team>`
  - `routed_username=<username>`
  - `routing_source=webhook|slackbot`
- ログには webhook/slackbot ID、team ID、username、結果、session ID を構造化して残す。秘密や payload 全体は記録しない。

## エラーと delivery 記録

resolver の sentinel error を定義する。

- `ErrRoutingTargetNotConfigured`
- `ErrRoutingTargetInvalid`

外部応答は情報漏えい防止のため一律の routing error に変換する。内部ログと Webhook delivery record では error code を保持する。

- Webhook: delivery status を `failed`、session ID は空にする
- SlackBot: session を作らず thread に汎用エラーを通知する
- dry-run: resolved `scope`, `user_id`, `team_id` と routing error を返す。管理 API が無認証で公開される構成では user ID の露出を避け、match の成否だけを返す

## 競合・再利用・上限

`max_sessions` の意味は trigger 全体の上限を維持する。つまり個人 routing 後も `webhook_id` / `slackbot_id` 単位の総数で制限する。

一方、reuse は必ず owner identity を含める。これにより以下を防止する。

- 同一 tag を持つ別ユーザーの session の reuse
- team session と personal session の混同
- Slack thread 中の routing target 変更時の越境

SlackBot の pending dedup key も `channel:thread:scope:userID/teamID` にする。target が異なるイベントを誤って drop しないためである。

## テスト計画

### Resolver unit tests

- team + routing config なしは team identity のまま
- team + username なしは team identity のまま
- user trigger + 他人の username は無視
- allowlist match は登録された UserID の user identity へ変換
- allowlist mismatch / 空の mapped UserID は reject
- repository lookup や membership check を行わない
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
- allowlist を削除した後は新規 delivery が reject される

## ロールアウト

1. resolver、identity-aware filter、監査 tag を feature flag 配下で実装する
2. dry-run で team triggers の routing value と allowlist match を確認する
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

- username から直接 UserID を組み立てず、team-owned の静的 mapping で canonical UserID に変換する
- routing 判定はテンプレート処理ではなく application use case に置き、Webhook と SlackBot で同じ routing policy を使う
- routing failure は fallback せず reject する
- resource owner（Webhook/SlackBot）は変更せず、作成される Session の identity だけを変更する
- reuse は tags だけでなく identity boundary を含める
- 認証なしでは user の本人性や team membership を検証できないことを明示し、user scope を認可境界として過信しない

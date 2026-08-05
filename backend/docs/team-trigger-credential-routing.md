# Team trigger の triggered user と認証ファイル選択設計

## 目的

team scope の Webhook / SlackBot が作るSessionで、trigger設定の所有者とeventを発生させたユーザーを区別する。

- `UserID`: trigger設定の所有者。現在の挙動を維持
- `TriggeredUserID`: delivery eventを発生させたユーザー
- `Scope` / `TeamID`: Sessionのアクセス範囲。team triggerではteamのまま

GitHub webhook payloadの`user.login`などを`TriggeredUserID`としてSessionへ渡し、`credential_source: triggered_user`で、そのユーザーのmanaged credential filesを選択できるようにする。

## 現状

Webhook / SlackBot entityの`UserID`はtrigger設定の所有者を表し、Session起動時にも同じ値を`RunServerRequest.UserID`へ渡している。

既存の`credential_source`は次の値を持つ。

| 値 | managed credential filesの所有元 |
|---|---|
| `session_user` | `RunServerRequest.UserID` |
| `team` | `RunServerRequest.TeamID` |
| `none` | 注入しない |
| 未指定 | user scopeはsession user、team scopeは注入なし |

event由来usernameを`UserID`へ上書きすると、個人API key、settings、実行ユーザーnamespace、`AGENTAPI_USER_ID`などにも影響し、trigger設定所有者の情報も失われる。したがって`UserID`は変更せず、別フィールドを追加する。

## 提案

### `TriggeredUserID`を追加する

`RunServerRequest`、`LaunchRequest`、Session entity、永続化metadataに`TriggeredUserID`を追加する。

```go
type RunServerRequest struct {
    UserID          string // trigger設定の所有者
    TriggeredUserID string // eventを発生させたユーザー
    Scope           ResourceScope
    TeamID          string
    // ...
}
```

team triggerからの起動例:

```text
UserID:          webhook.UserID() / slackbot.UserID()
TriggeredUserID: rendered tags["username"]
Scope:           team
TeamID:          webhook.TeamID() / slackbot.TeamID()
```

`TriggeredUserID`はSessionのownership判定には使用しない。アクセス可否は引き続き`Scope=team`と`TeamID`で決まる。

### `credential_source: triggered_user`を追加する

| 値 | managed credential filesの所有元 |
|---|---|
| `session_user` | trigger設定所有者である`UserID` |
| `triggered_user` | event actorである`TriggeredUserID` |
| `team` | `TeamID` |
| `none` | 注入しない |

Kubernetes session managerの選択処理は次のようにする。

```go
switch req.CredentialSource {
case "session_user":
    credentialOwner = req.UserID
case "triggered_user":
    credentialOwner = req.TriggeredUserID
case "team":
    credentialOwner = req.TeamID
case "none":
case "":
    if req.Scope == entities.ScopeUser || req.Scope == "" {
        credentialOwner = req.UserID
    }
}
```

`credential_source=triggered_user`なのに`TriggeredUserID`が空の場合は起動を拒否する。`session_user`やteam credentialへfallbackしない。

## EventからのTriggeredUserID解決

要件どおり、render済みの`tags.username`を使用する。

```json
{
  "scope": "team",
  "team_id": "acme/platform",
  "session_config": {
    "tags": {
      "username": "{{.user.login}}"
    },
    "params": {
      "credential_source": "triggered_user"
    }
  }
}
```

GitHub eventの種類によってフィールドが異なる場合は、既存のtag templateで`{{.sender.login}}`や`{{.issue.user.login}}`などを指定する。

解決順序:

1. session configのtagsをdelivery payloadでrenderする
2. `tags["username"]`の前後空白を除去する
3. 空でなければ`TriggeredUserID`へ設定する
4. `UserID`はtrigger entityの`UserID`のまま維持する
5. `credential_source=triggered_user`で値が空ならrejectする

`tags.username`がない場合でも、`credential_source`が`triggered_user`でなければ従来どおり起動できる。

## 認証がない構成での意味

no-auth構成では、payloadのusernameがCCPlantに登録済みか、本人か、team memberかは検証できない。GitHub webhook署名が保証するのはpayloadの送信元と改ざんされていないことまでであり、GitHub loginとCCPlant credential ownerの対応はnaming conventionである。

初期実装ではrenderしたusernameをそのままcanonical credential owner IDとして扱う。`MemoryUserRepository`でのlookupは行わない。

対応するmanaged credential Secretが存在しなければ、既存実装どおりcredentialなしで起動する案もあるが、`credential_source=triggered_user`を明示した意図を尊重し、初期実装では起動失敗とする方が安全である。具体的には`credential source resolved but credential files not found`をdelivery errorとして記録する。

## セキュリティ

team scope Sessionに個人credentialを注入すると、team Sessionを操作できるユーザーからcredentialを使用できる。この機能は明示的なopt-inとする。

- team triggerの既定`credential_source`は引き続き`none`
- `tags.username`だけではcredentialを注入しない
- `credential_source=triggered_user`を明示した場合だけ注入する
- Webhook signature / Slack Socket Modeの検証を必須とする
- `TriggeredUserID`をSecret名へ変換するときは既存sanitize処理を使う
- sanitize後の衝突を防ぐため、Secret annotation内のraw owner IDも照合する
- log / Session metadataに`user_id`, `triggered_user_id`, `credential_source`を記録する
- credential内容はログに出さない

管理APIも無認証の場合、この設定はsecurity boundaryにならない。その構成で個人credentialをteam Sessionへ注入する機能は有効化すべきではない。

## 各triggerへの組み込み

### Webhook

`WebhookSessionService`でsession config merge後にtagsをrenderし、`tags.username`から`TriggeredUserID`を解決する。

```go
LaunchRequest{
    UserID:          webhook.UserID(),
    TriggeredUserID: strings.TrimSpace(tags["username"]),
    Scope:           webhook.Scope(),
    TeamID:          webhook.TeamID(),
    CredentialSource: renderedParams.CredentialSource,
}
```

GitHub / custom webhookの両方で同じ処理を使う。

### SlackBot

`SlackBotEventHandler`でもrender済み`tags.username`から`TriggeredUserID`を設定する。Slack user IDとCCPlant usernameが異なる場合は、template入力へcanonical usernameを供給する別機能が必要になる。

### Scheduleと手動起動

event actorが存在しないため`TriggeredUserID`は空のままにする。`credential_source=triggered_user`はvalidation errorとする。

## Session reuseとlimit

reuse filterには既存tagsに加えて`TriggeredUserID`を含める。同じrepository/threadでもevent actorが異なる場合、異なるcredentialを持つ新規Sessionを作る。

SlackBotのpending dedup keyにも`TriggeredUserID`を含める。

`max_sessions`はtrigger全体の上限を維持し、`webhook_id` / `slackbot_id`単位で数える。

## API / persistence変更

- `SessionParams.CredentialSource`
  - enumへ`triggered_user`を追加
- `RunServerRequest` / `LaunchRequest`
  - `TriggeredUserID`
- Session implementation / Kubernetes metadata
  - raw `triggered_user_id`をannotationへ保存
- Session filter
  - reuse用に`TriggeredUserID`を追加
- provision settings
  - 必要なら監査用`TriggeredUserID`を追加
- OpenAPI、Webhook/SlackBot import/export、management skillsを更新

## テスト計画

- team webhookでrendered `tags.username`が`TriggeredUserID`になる
- `UserID`はtrigger設定所有者のまま
- Sessionは`Scope=team`, `TeamID=trigger.TeamID`のまま
- `credential_source=session_user`は従来どおり`UserID`のcredentialを読む
- `credential_source=triggered_user`は`TriggeredUserID`のcredentialを読む
- `TriggeredUserID`空で`triggered_user`指定ならfallbackせずreject
- team triggerでcredential source未指定ならcredentialなし
- triggered userが異なる既存Sessionをreuseしない
- managed credential Secretがない場合はdelivery error
- 一般user-managed filesはteam scope Sessionへ注入されない

## 実装順序

1. `TriggeredUserID`をSession作成経路と永続化へ追加
2. `credential_source=triggered_user`を追加
3. rendered `tags.username`からのTriggeredUserID解決を追加
4. Webhook / SlackBotへ組み込む
5. reuse、audit metadata、OpenAPI、import/exportを更新
6. unit / integration tests

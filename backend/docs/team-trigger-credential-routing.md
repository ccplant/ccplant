# Team trigger の実行ユーザーと認証ファイル選択設計

## 目的

team scope の Webhook / SlackBot が作るSessionについて、次のidentityを分離する。

- `TriggerUserID`: Webhook / SlackBot設定を作成したユーザー
- `UserID`: delivery eventから解決した、そのSessionの実行ユーザー
- `Scope` / `TeamID`: Sessionのアクセス範囲。team triggerではteamのまま

GitHub webhook payloadの`user.login`などを`UserID`としてSessionへ渡し、`credential_source: session_user`を指定すると、そのユーザーのmanaged credential filesを利用できるようにする。

## 現状

Webhook / SlackBot entityの`UserID`はtrigger作成者を表し、Session起動時にも同じ値を`RunServerRequest.UserID`へ渡している。したがって現状のteam triggerでは、trigger作成者とsession userが暗黙に同一である。

既存の`credential_source`は次の値を持つ。

| 値 | managed credential filesの所有元 |
|---|---|
| `session_user` | `RunServerRequest.UserID` |
| `team` | `RunServerRequest.TeamID` |
| `none` | 注入しない |
| 未指定 | user scopeはsession user、team scopeは注入なし |

このままevent由来usernameを`UserID`へ入れると、trigger作成者を参照する手段が失われる。また、eventにusernameがない場合にtrigger作成者へ暗黙fallbackすると、`session_user`の意味がdeliveryごとに変わってしまう。

## 提案

### Session identityに`TriggerUserID`を追加する

`RunServerRequest`、Session entity、永続化metadata、provision settingsに`TriggerUserID`を追加する。

```go
type RunServerRequest struct {
    UserID        string // eventから解決したsession user
    TriggerUserID string // trigger設定の作成者。通常の手動起動では空
    Scope         ResourceScope
    TeamID        string
    // ...
}
```

team triggerからの起動例:

```text
UserID:        payload由来のusername
TriggerUserID: webhook.UserID() / slackbot.UserID()
Scope:         team
TeamID:        webhook.TeamID() / slackbot.TeamID()
```

`TriggerUserID`はownership判定には使用しない。Sessionへのアクセス可否は引き続き`Scope=team`と`TeamID`で決まる。

### `credential_source: trigger_user`を追加する

| 値 | managed credential filesの所有元 |
|---|---|
| `session_user` | event由来の`UserID` |
| `trigger_user` | trigger設定作成者の`TriggerUserID` |
| `team` | `TeamID` |
| `none` | 注入しない |

Kubernetes session managerの選択処理は次のようにする。

```go
switch req.CredentialSource {
case "session_user":
    credentialOwner = req.UserID
case "trigger_user":
    credentialOwner = req.TriggerUserID
case "team":
    credentialOwner = req.TeamID
case "none":
case "":
    if req.Scope == entities.ScopeUser || req.Scope == "" {
        credentialOwner = req.UserID
    }
}
```

`trigger_user`なのに`TriggerUserID`が空の場合は設定エラーとして起動を拒否する。別ownerへfallbackしない。

## EventからのUserID解決

要件どおり、render済みの`tags.username`をUserIDとして使用する。

```json
{
  "scope": "team",
  "team_id": "acme/platform",
  "session_config": {
    "tags": {
      "username": "{{.user.login}}"
    },
    "params": {
      "credential_source": "session_user"
    }
  }
}
```

GitHub payloadの種類によってユーザーフィールドは異なりうるため、固定JSON pathではなく既存のtag template renderingを利用する。例えばイベントに応じて`{{.user.login}}`、`{{.sender.login}}`、`{{.issue.user.login}}`を設定できる。

### 解決規則

1. session configのtagsをdelivery payloadでrenderする
2. `tags["username"]`の前後空白を除去する
3. 空でなければその文字列を`Session.UserID`とする
4. `TriggerUserID`には常にtrigger entityの`UserID`を設定する
5. `username` tagが設定されているのにrender結果が空の場合はtrigger作成者へfallbackしない

`username` tag自体を設定していない既存triggerは、互換性のため現在どおりtrigger作成者を`UserID`として起動する。

空の場合の挙動は明示設定にする。

```json
"session_config": {
  "tags": {"username": "{{.user.login}}"},
  "on_user_id_unresolved": "reject"
}
```

初期実装は`reject`のみを許可する。`trigger_user` fallbackが必要なら、将来`on_user_id_unresolved: use_trigger_user`として明示的に追加する。

## 認証がない構成での意味

現在のno-auth構成では、payloadのusernameがCCPlantに登録済みか、本人か、team memberかは検証できない。GitHub webhookでは署名により「GitHubから受信したpayloadである」ことは確認できるが、`user.login`とCCPlant UserIDの対応までは保証しない。

したがって初期実装では、renderしたusernameをそのままcanonical UserIDとして扱う。これはidentity verificationではなくnaming conventionである。

managed credential Secretが存在しなければ、既存実装どおりcredentialなしで起動する。usernameの存在確認には使わない。

## セキュリティ

`credential_source: session_user`をteam triggerで使うと、event payloadに現れたusernameの個人credentialがteam sessionへ注入される。team sessionを閲覧・操作できるユーザーからそのcredentialを利用できるため、明示的なopt-inが必要である。

- team triggerの既定`credential_source`は引き続き`none`
- `tags.username`だけではcredentialを注入しない
- 管理者が`credential_source: session_user`を明示した場合だけ注入する
- Webhook signature / Slack Socket Modeの検証を必須とする
- `UserID`をSecret名へ変換するときは既存sanitize処理を使う
- sanitize後の衝突を防ぐため、可能ならSecret annotation内のraw owner IDも照合する
- log / session metadataに`user_id`, `trigger_user_id`, `credential_source`を記録する
- credential内容はログに出さない

管理APIも無認証の場合、この設定はsecurity boundaryにならない。その構成で個人credentialをteam sessionへ注入する機能は有効化すべきではない。

## 各triggerへの組み込み

### Webhook

`WebhookSessionService`でsession config merge後にtagsをrenderし、`tags.username`からUserIDを解決する。

```go
LaunchRequest{
    UserID:        resolvedUserID,
    TriggerUserID: webhook.UserID(),
    Scope:         webhook.Scope(),
    TeamID:        webhook.TeamID(),
    CredentialSource: renderedParams.CredentialSource,
}
```

GitHub / custom webhookの両方で同じ処理を使う。

### SlackBot

`SlackBotEventHandler`でも同じtemplate resolverを使う。Slack user IDとCCPlant UserIDが異なる場合は、template入力へ対応表からcanonical usernameを追加する別機能が必要になる。両者が同じ運用なら`{{.event.user}}`を使用できる。

### Scheduleと手動起動

- Schedule: `TriggerUserID=schedule.UserID()`を設定する。`UserID`は現行どおりschedule ownerを維持する
- 手動起動: `TriggerUserID`は空。`credential_source=trigger_user`はvalidation error

これにより`trigger_user`の意味が外部trigger全体で一貫する。

## Session reuseとlimit

reuse filterには既存tagsに加えて`UserID`を含める。同じrepository/threadでもevent由来userが異なる場合、異なるcredentialを持つ新規Sessionを作る。

SlackBotのpending dedup keyにもresolved `UserID`を含める。

`max_sessions`はtrigger全体の上限を維持し、`webhook_id` / `slackbot_id`単位で数える。

## API / persistence変更

- `WebhookSessionConfig`
  - `on_user_id_unresolved`
- `SessionParams.CredentialSource`
  - enumへ`trigger_user`を追加
- `RunServerRequest` / `LaunchRequest`
  - `TriggerUserID`
- Session implementation / Kubernetes labels or annotations
  - raw `trigger_user_id`を保存
- provision settings
  - 必要なら監査用`TriggerUserID`を追加
- OpenAPI、Webhook/SlackBot import/export、management skillsを更新

## テスト計画

- team webhookで`user.login`がSession.UserIDになる
- Sessionは`Scope=team`, `TeamID=trigger.TeamID`のまま
- `TriggerUserID`はtrigger作成者になる
- `credential_source=session_user`はevent由来UserIDのcredentialを読む
- `credential_source=trigger_user`はtrigger作成者のcredentialを読む
- unresolved user IDはtrigger作成者へfallbackせずreject
- `trigger_user`かつ`TriggerUserID`空はreject
- team triggerでcredential source未指定ならcredentialなし
- user IDが異なる既存Sessionをreuseしない
- user ID以外のteam settings / ownershipが変わらない
- managed credential Secretなしでもcredentialなしで起動できる
- 一般user-managed filesはteam scope Sessionへ注入されない

## 実装順序

1. `TriggerUserID`をSession作成経路と永続化へ追加
2. `credential_source=trigger_user`を追加
3. rendered `tags.username`からのUserID解決とunresolved policyを追加
4. Webhook / SlackBot / Scheduleへ組み込む
5. reuse、audit metadata、OpenAPI、import/exportを更新
6. unit / integration tests

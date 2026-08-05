# Team trigger の username による認証ファイル選択設計

## 目的

team scope の Webhook / SlackBot がレンダリングした session tag に `username` がある場合、そのユーザー用に保存された managed credential files をセッションへ注入する。

セッションの scope と team ownership は変更しない。

## 既存機能

`SessionParams.CredentialSource` / `RunServerRequest.CredentialSource` が、セッションに注入する managed credential files の所有元を選択する。既存値だけではsession user以外の任意ユーザーを、session identityから独立して指定することはできない。

| `credential_source` | 認証ファイルの所有元 |
|---|---|
| `session_user` | `RunServerRequest.UserID` |
| `team` | `RunServerRequest.TeamID` |
| `none` | 注入しない |
| 未指定 | user scope は session user、team scope は注入なし |

Kubernetes session manager は、選択したownerに対応する `agentapi-agent-files-<sanitized-owner>` Secretを読み、provision settingsのmanaged filesへ埋め込む。

この仕組みが扱うのは認証ファイルだけである。`agentapi-user-files-*` の一般user-managed filesはuser scope sessionにしか注入されない。

## 提案する振る舞い

team scope triggerに明示的なcredential routing設定を追加する。

```json
{
  "name": "Team issue router",
  "scope": "team",
  "team_id": "acme/platform",
  "credential_routing": {
    "tag": "username",
    "targets": {
      "alice": "alice",
      "bob": "user-0187"
    },
    "on_unresolved": "reject"
  },
  "session_config": {
    "tags": {
      "username": "{{.issue.assignee.login}}"
    }
  }
}
```

`targets` は外部イベントから得た値と、managed credentialsのcanonical owner IDとの静的mappingである。イベント中の値を直接credential ownerとして採用しない。

### 解決結果

一致した場合、`LaunchRequest` を次のように構築する。

```text
UserID:           trigger.UserID（変更しない）
Scope:            team
TeamID:           trigger.TeamID
Teams:            [trigger.TeamID]
CredentialOwnerID: targets[tags[tag]]
```

既存の`credential_source=session_user`は`RunServerRequest.UserID`しか参照できない。`UserID`をrouting対象へ差し替えると、認証ファイルだけでなく個人API key、settings、実行ユーザーnamespace、`AGENTAPI_USER_ID`などにも影響する。そのため、認証ファイルの参照先だけを表す内部フィールド`CredentialOwnerID`を追加する。この値が空でなければKubernetes session managerは既存の`CredentialSource`解決より優先して使用する。management APIの通常session start requestには公開しない。

Sessionの作成者、アクセス範囲、team settings、team memory/profileの選択はteam scopeのまま維持する。

### 既存設定との互換性

- `credential_routing` がなければ現在の挙動を維持する
- `username` tagだけではroutingしない
- `credential_routing` と明示的な `session_config.params.credential_source` の同時指定はvalidation errorにする
- user scope triggerでは `credential_routing` を禁止する

## 「検証」の意味

現在のno-auth構成では、usernameの本人性、在籍、team membershipは検証できない。`MemoryUserRepository` もプロセスローカルであり、credential routingのidentity authorityには使えない。

検証するのは次の項目だけである。

1. triggerがteam scopeである
2. routing tagの値が静的な`targets`に完全一致する
3. mapped owner IDが空でない
4. 設定が管理APIを通じて保存された正しい形式である

対象のcredential Secretが存在しないことはvalidation errorにしない。ユーザーがまだ認証ファイルを登録していない正常ケースがあるため、既存実装どおりファイルなしで起動する。ただしdelivery record / logに`credential_files_found=false`を残すと運用しやすい。

管理API自体が無認証なら、allowlistもセキュリティ境界にはならない。その構成では本機能を無効にするか、Webhook/SlackBot設定の変更経路をAPI keyなどで保護する必要がある。

## セキュリティ上の注意

team scope sessionはteamから閲覧できるため、個人credentialを注入すると、そのcredentialをteam sessionの利用者が使用できる。これは個人sessionへのroutingより強い共有である。

そのため以下を必須とする。

- credential routingはdefault disabled
- team-ownedの静的allowlistを必須にする
- 外部payloadからowner IDを直接指定させない
- routing対象ユーザーが、このteamでcredentialを共有することに同意している運用を前提にする
- logとsession tagsへ`credential_owner`、`credential_routing_source`、`trigger_team_id`を記録する
- credential内容、Secret名の生値、ファイル内容はログへ出さない

より厳格にする場合は、ユーザー側に`team_id`単位のcredential sharing consentを永続化し、allowlistとの両方が一致したときだけ注入する。このconsent storeがない初期実装では、team管理者によるallowlistを唯一の許可情報とする。

## 実装箇所

### Domain / persistence

WebhookとSlackBotに共通の設定型を追加する。

```go
type CredentialRoutingConfig struct {
    Tag          string            `json:"tag"`
    Targets      map[string]string `json:"targets"`
    OnUnresolved string            `json:"on_unresolved"`
}
```

Kubernetes-backed repositories、controller DTO、OpenAPI、import/exportへ追加する。

### 共通resolver

`internal/usecases/session/credential_router.go` にpureなresolverを置く。

```go
func ResolveCredentialOwner(
    triggerScope entities.ResourceScope,
    tags map[string]string,
    config *entities.CredentialRoutingConfig,
) (ownerID string, matched bool, err error)
```

`UserRepository`には依存しない。

### Webhook

`WebhookSessionService`でtagsのtemplate renderingとmergeが完了した後に解決する。match時は次を変更する。

- `LaunchRequest.CredentialOwnerID = credentialOwner`

`Scope`, `TeamID`, `Teams`は既存のteam identityを維持する。

### SlackBot

`SlackBotEventHandler`でもrendered tagsから同じresolverを呼び、同じcredential用フィールドだけを変更する。

Slack thread reuse時は、既存sessionの`credential_owner` tagもfilterへ含める。同じthreadでrouting targetが変わった場合、別のcredentialを既存Podへ後付けできないため、新規sessionを作る。

pending dedup keyにもcredential ownerを含める。

## エラー方針

- tagなし/空: routingしない。team scopeの通常起動
- allowlist mismatch: `on_unresolved=reject`なら起動しない
- invalid config: management APIで400、delivery時に検出した場合は起動しない
- credential Secretなし: credentialなしで起動し、監査情報へ記録

未解決時にteam credentialやtrigger作成者のcredentialへfallbackしない。

## テスト計画

- routingなしのteam triggerは`CredentialSource`未指定のまま
- allowlist matchでscope/team/UserIDを維持し、CredentialOwnerIDだけが設定される
- allowlist mismatchはreject
- user scopeでcredential routingを拒否
- explicit credential sourceとの競合を拒否
- WebhookとSlackBotで同じresolver結果になる
- credential ownerが異なるSlack thread sessionをreuseしない
- Kubernetes session managerがmapped ownerの`agentapi-agent-files-*`だけを読む
- 一般user-managed filesはteam sessionへ注入されない

## 実装順序

1. `CredentialRoutingConfig`とpure resolver
2. Webhook/SlackBot persistence・management API・OpenAPI
3. Webhook launchへの組み込み
4. SlackBot launch/reuse/dedupへの組み込み
5. audit tags、delivery record、feature flag

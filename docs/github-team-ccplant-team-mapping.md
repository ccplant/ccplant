# GitHub Team と ccplant Team のマッピング設計

## 背景と目的

現在の ccplant は、認証時に取得した GitHub Team の `organization/team-slug` を ccplant の Team ID として扱う箇所が多い。この方式では、たとえば GHES の `dev/cc-users` を利用している ccplant Team に、`github.com` 上の別 Team のメンバーも所属させることができない。また、GitHub の organization や slug の変更が ccplant 内のリソース所有者 ID に波及する。

本設計では次を実現する。

- ccplant Team を GitHub Team から独立した主体（principal）として識別する。
- 1 つの ccplant Team に、GitHub 接続をまたいだ複数の GitHub Team を対応付ける。
- 対応する GitHub Team のいずれかに所属するユーザーを ccplant Team のメンバーとして認可する。
- ccplant Team と初期マッピングをサーバー設定から冪等に自動作成する。
- 既存の `org/team-slug` Team ID と TeamConfig を段階的に移行できるようにする。

この機能は「認証元の GitHub Team」を ccplant Team の識別子として流用せず、外部グループを ccplant Team の membership source として扱う。

## 用語

- **ccplant Team**: ccplant 内のリソース所有・認可単位。
- **Team principal ID**: ccplant Team に割り当てる不変かつ一意な ID。例: `team_01J...`。表示名や slug 変更の影響を受けない。
- **Team key**: API や設定で人が指定する一意な論理名。例: `cc-users`。既存互換期間は `team_id` として扱うこともできる。
- **GitHub connection**: `github.com` または特定 GHES への接続。既存の GitHub Connection の `id` で識別する。
- **External team binding**: `(connection_id, organization_pattern, team_slug_pattern)` と ccplant Team の対応付け。organization と team slug は完全一致または glob パターンで指定する。

## 提案するデータモデル

### ccplant Team

`TeamConfig` を設定の寄せ集めではなく ccplant Team の永続レコードとして拡張する。

```go
type TeamConfig struct {
    principalID    string
    teamKey        string
    displayName    string
    externalTeams  []ExternalTeamBinding
    serviceAccount *ServiceAccount
    envVars        map[string]string
}

type ExternalTeamBinding struct {
    Provider       string // 初期値は "github"
    ConnectionID   string // GitHub Connection の不変 ID
    OrganizationPattern string
    TeamSlugPattern     string
}
```

永続化する JSON の例:

```json
{
  "schema_version": 2,
  "principal_id": "team_01JTEAM7AM3NQKPF6QJ8K58XW",
  "team_key": "cc-users",
  "display_name": "CC users",
  "external_teams": [
    {
      "provider": "github",
      "connection_id": "github-enterprise",
      "organization_pattern": "dev",
      "team_slug_pattern": "cc-users"
    },
    {
      "provider": "github",
      "connection_id": "github-com",
      "organization_pattern": "ccplant-*",
      "team_slug_pattern": "*-contributors"
    }
  ],
  "service_account": null,
  "env_vars": {}
}
```

`principal_id` は作成時にサーバーが生成し、更新 API や config reconciliation では変更不可とする。ユーザー principal と同じ名前空間に置く場合は型付き ID (`usr_...`, `team_...`) にし、将来の ACL の subject を `(principal_type, principal_id)` で表現できるようにする。今回の実装ではユーザー用 `githubPrincipal` Secret を流用せず、TeamConfig 内に保持する。ユーザー principal は「複数 GitHub identity を同一人物に束ねるもの」、Team principal は「リソース所有・membership の単位」でライフサイクルが異なるためである。

binding の正規化規則は次のとおりとする。

- `connection_id` は必須で、存在し enabled な GitHub Connection を参照する。
- `organization_pattern` と `team_slug_pattern` は trim 後に小文字化する。
- パターン構文は既存 `team_role_mapping` と同じ glob (`*`, `?`) に統一し、正規表現は受け付けない。`*` は `/` をまたがず、それぞれ organization または team slug の 1 要素内だけに一致する。
- 両方に wildcard がない binding は完全一致として扱う。
- 同じ GitHub Team が複数の ccplant Team にマッチする設定は不正とする。起動時に静的に重複を判定し、wildcard 同士など静的に判定できない組み合わせは、GitHub API から Team 一覧を取得して展開・検証する。
- GitHub API による検証ができない状態で、未検証の wildcard binding を認可には使用しない。
- パターンは設定上の意図として保存し、解決済み Team は connection 内の numeric team ID とともに別 index/cache に保存する。slug rename 後は再展開で追従し、古い解決結果は TTL 後に認可しない。

### ID と表示名の分離

新規リソースの `team_id` には最終的に Team principal ID を保存する。利用者向け API は `team_principal_id`, `team_key`, `display_name` を返す。移行期間中は既存 API の `team_id` に team key (`org/team` など) を受け付け、resolver で principal ID に変換する。

```
GitHub identity ──membership──> External team binding
                                      │
                                      ▼
                               ccplant Team
                            principal_id = team_...
                                      │
                                      ▼
                         sessions / settings / memories
```

## 設定スキーマと自動作成

GitHub 接続自体と Team 定義を分離し、トップレベルに `team_discovery` と `teams` を追加する。`team_discovery` は GitHub Team 名の一部を ccplant Team key としてキャプチャし、Team principal を動的に作成する。`teams` は既知の ccplant Team を静的に宣言する用途に残す。

### 動的 Team discovery

GHES の `test/cc-users` から ccplant の `test` Team を作成する場合は、次のように設定する。

```yaml
team_discovery:
  - connection_id: ghes
    team_pattern: "{team}/cc-users"
    team_key: "{team}"
    display_name: "{team}"
```

この `{team}` は通常の wildcard ではなく名前付き capture である。GitHub Team `test/cc-users` に対して `{team} = test` となり、次の TeamConfig を初回観測時に作成する。

```json
{
  "principal_id": "team_01JTEAM7AM3NQKPF6QJ8K58XW",
  "team_key": "test",
  "display_name": "test",
  "external_teams": [
    {
      "provider": "github",
      "connection_id": "ghes",
      "organization_pattern": "test",
      "team_slug_pattern": "cc-users",
      "managed_by": "discovery"
    }
  ]
}
```

Team の生成契機は、対象 GitHub connection でユーザーの membership をロードしたときとする。たとえば Alice の GHES membership に `test/cc-users` が含まれていれば、resolver は discovery rule にマッチさせ、ccplant `test` Team がなければ principal を作成してから Alice をその Team のメンバーとして認可する。全 organization の事前列挙は不要である。

同時に複数ユーザーが初回ログインしても principal が二重作成されないよう、正規化済み `team_key` を一意キーとして atomic create を行う。競合した処理は作成済み TeamConfig を再取得する。`team_key` は capture 展開後に小文字化し、`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$` を満たす必要がある。不正な展開結果は認可せず監査ログへ記録する。

作成した TeamConfig には `discovery_rule_id` と capture 値も保存する。同じ key の Team がすでに存在しても、それが同じ discovery rule と capture から作成されたものなら再利用する。手動作成 Team や別 rule 由来 Team と key が衝突した場合は、既存 Team へ自動 binding せず fail closed にする。そうしないと、GitHub Team 名を作れるユーザーが既存 ccplant Team に参加できる可能性がある。

初期仕様では名前付き capture は organization または team slug の要素全体を表すものに限定し、部分 capture や正規表現は許可しない。たとえば `{team}/cc-users` と `myorg/{team}-cc-users` のうち、後者のような部分 capture は将来拡張とする。これにより Team key の予期しない生成や pattern ambiguity を避ける。

### 手動での追加マッピング

discovery で `test` Team を一度作成した後、管理者は Team 設定 API または UI から GHEC Team を追加できる。

```yaml
# TeamConfig の UI/API 表現。サーバーの bootstrap config ではない。
key: test
external_teams:
  - connection_id: ghes
    team_pattern: test/cc-users
    managed_by: discovery
  - connection_id: ghec
    team_pattern: myorg/test-cc-users
    managed_by: api
```

これ以降の membership 解決は次のようになる。

| GitHub connection | GitHub Team | ccplant Team |
|---|---|---|
| `ghes` | `test/cc-users` | `test` |
| `ghec` | `myorg/test-cc-users` | `test` |

GHES membership をロードした Alice と、GHEC membership をロードした Bob は、同じ `test` Team principal のメンバーになる。両方の identity が同じ user principal にリンクされている場合も結果は `test` 1 件に重複排除する。

field ownership は binding 単位で管理する。`managed_by: discovery` の binding は discovery rule が所有するため API から削除・変更できない。一方、同じ TeamConfig の `managed_by: api` binding は管理者が追加・更新・削除できる。したがって、Team が config 由来であることを理由に TeamConfig 全体を read-only にはしない。

### 静的 Team 宣言

GitHub Team と ccplant Team の対応があらかじめ分かっている場合は、`teams` で静的に宣言できる。

```yaml
teams:
  - key: cc-users
    display_name: CC users
    external_teams:
      - provider: github
        connection_id: github-enterprise
        organization_pattern: dev
        team_slug_pattern: cc-users
      - provider: github
        connection_id: github-com
        organization_pattern: ccplant-*
        team_slug_pattern: "*-contributors"
```

短縮記法として `team_pattern` も許可する。`organization_pattern` / `team_slug_pattern` との同時指定はエラーにする。

```yaml
teams:
  - key: cc-users
    external_teams:
      - connection_id: github-enterprise
        team_pattern: dev/cc-users
      - connection_id: github-com
        team_pattern: ccplant-*/*-contributors
```

短縮記法は最初の `/` で organization pattern と team slug pattern に分割する。空要素、`/` がない値、3 要素以上の値は設定エラーとする。これにより既存 `team_role_mapping` の `org/team` パターンと移行時の見た目を揃えられる。

### マッピング例

次の設定を例にする。

```yaml
teams:
  - key: cc-users
    display_name: CC users
    external_teams:
      - connection_id: ghes
        team_pattern: dev/cc-*
      - connection_id: github-com
        team_pattern: ccplant-*/*-contributors

  - key: platform-admins
    display_name: Platform administrators
    external_teams:
      - connection_id: ghes
        team_pattern: dev/platform-admins
```

この設定による解決結果は次のようになる。

| GitHub connection | 実際の GitHub Team | マッチする設定 | 所属する ccplant Team |
|---|---|---|---|
| `ghes` | `dev/cc-users` | `dev/cc-*` | `cc-users` |
| `ghes` | `dev/cc-admins` | `dev/cc-*` | `cc-users` |
| `ghes` | `dev/platform-admins` | `dev/platform-admins` | `platform-admins` |
| `github-com` | `ccplant/frontend-contributors` | `ccplant-*/*-contributors` | `cc-users` |
| `github-com` | `ccplant-labs/ai-contributors` | `ccplant-*/*-contributors` | `cc-users` |
| `github-com` | `ccplant/core` | なし | なし |
| `github-com` | `dev/cc-users` | なし | なし |
| `ghes` | `product/cc-users` | なし | なし |

同じ `dev/cc-users` という Team 名でも、`connection_id` が異なれば別の external Team である。上の例では `ghes` の `dev/cc-users` だけが `cc-users` にマッチし、`github-com` の `dev/cc-users` はマッチしない。

ユーザー単位では、以下のように解決する。

- Alice が GHES の `dev/cc-users` に所属していれば、ccplant の `cc-users` に所属する。
- Bob が github.com の `ccplant/frontend-contributors` に所属していても、ccplant の同じ `cc-users` に所属する。
- Carol が両方に所属していても、ccplant の所属 Team は重複せず `cc-users` 1 件になる。
- Dave が `github-com` の `ccplant/core` にしか所属していなければ、この設定から得る ccplant Team はない。
- GHES 側の Alice と github.com 側の Alice の identity が同じ user principal にリンクされている場合、両 connection の membership の和集合から ccplant Team を解決する。名前やメールアドレスが同じだけでは統合しない。

つまり、複数の external GitHub Team を 1 つの ccplant Team に束ねることはできるが、1 つの external GitHub Team を複数の ccplant Team に割り当てることはできない。たとえば別の ccplant Team にも `ghes: dev/cc-users` とマッチする pattern を追加すると、設定競合として reconciliation を失敗させる。

起動時に `TeamReconciler` が静的な `teams` 宣言を TeamConfig repository に反映し、`team_discovery` の構文と既存 Team との競合を検証する。discovery 対象の Team principal は起動時ではなく membership の初回観測時に作成する。

1. `key` で既存 TeamConfig を検索する。
2. 存在しなければ新しい Team principal ID を生成して作成する。
3. 存在すれば principal ID を維持したまま、宣言管理対象の `display_name` と `external_teams` を更新する。
4. GitHub Connection、パターン構文、展開後 binding の重複を検証する。不正な設定があれば起動を失敗させ、部分適用しない。
5. config から Team が消えても自動削除しない。既存リソースの orphan 化を避けるため、`managed_by: config` と最終観測世代を記録し、警告を出す。削除は明示 API と参照確認を伴う別操作にする。

複数 replica が同時起動するため、作成は compare-and-create とし、`key` の一意性を Kubernetes の決定的なリソース名または KV の unique constraint で担保する。既存の sanitized team ID だけを Secret 名に使う方式は衝突し得るため、`team-<sha256(key)[:32]>` のようなハッシュ名へ変更する。

Helm values にも同じ `team_discovery` と `teams` を追加し、設定 ConfigMap にそのままレンダリングする。秘密情報を含まないため Secret 化は不要である。

## membership 解決フロー

membership の解決はログイン時とトークン再認証時に行う。

1. GitHub identity を既存の仕組みで user principal に解決する。
2. その identity が属する `connection_id` を確定する。API URL だけで接続を推測しない。
3. 対象 connection の binding pattern に関係する organization の membership を GitHub API から取得する。organization pattern 自体が wildcard の場合は、ユーザーが所属する organization を列挙してから絞り込む。
4. membership の `(connection_id, organization, team_slug)` を正規化し、設定済みパターンと照合する。検証時に作成した `(connection_id, external_team_id) -> team_principal_id` index があればそれを優先する。
5. 一致した ccplant Team の principal ID を重複排除して `AuthorizationContext.TeamScope.Teams` に設定する。
6. Team ごとの権限を `TeamPermissions` に設定する。

ユーザーが GHES と github.com の identity を同じ user principal にリンクしている場合、保存済みかつ有効な各 identity の membership を解決し、その和集合を採用する。したがって GHES `dev/cc-users` で ccplant Team に参加しているユーザーも、同一 principal にリンクした github.com identity の Team binding から同じ ccplant Team に参加できる。未リンクの同名アカウントは同一人物とみなさない。

GitHub API 障害時に古い membership を無期限に認めるのは権限剥奪を遅らせるため、キャッシュには `observed_at` と connection ID を含める。推奨値は in-memory 30 秒、共有キャッシュ 5 分で、5 分を超えた stale entry は fail closed とする。API rate limit 時だけ猶予を設ける場合も上限を設定し、監査ログへ記録する。

既存の `team_role_mapping` はログイン可否・グローバル role のために当面維持できるが、ccplant Team membership の生成には使わない。最終的には権限を次の二層に分ける。

- platform role: admin など、インスタンス全体の権限。
- team role: ccplant Team ごとの `member` / `maintainer` と操作権限。

初期リリースでは external binding の一致を `member` とし、既存の TeamPermissions と同等の create/read/update/delete を与える。GitHub Team の `maintainer` / `member` は GitHub Team 内での役割であり、ccplant Team の管理権限へ暗黙に昇格させない。

## API

管理者向け API を追加する。

| Method | Path | 用途 |
|---|---|---|
| `GET` | `/admin/teams` | Team と binding の一覧 |
| `POST` | `/admin/teams` | Team principal の作成 |
| `GET` | `/admin/teams/{principal_id}` | Team の取得 |
| `PATCH` | `/admin/teams/{principal_id}` | 表示名・binding の更新 |
| `DELETE` | `/admin/teams/{principal_id}` | 参照がない Team の削除 |
| `GET` | `/teams` | 認証ユーザーが所属する Team の一覧 |

作成レスポンス例:

```json
{
  "principal_id": "team_01JTEAM7AM3NQKPF6QJ8K58XW",
  "key": "cc-users",
  "display_name": "CC users",
  "external_teams": [
    {
      "provider": "github",
      "connection_id": "github-enterprise",
      "organization_pattern": "dev",
      "team_slug_pattern": "cc-*"
    }
  ],
  "managed_by": "config"
}
```

config または discovery 管理の field/binding に対する API 更新は `409 Conflict` とし、設定変更を促す。ただし、同じ Team への API 管理 binding の追加は許可する。binding 更新時は、参照 connection の存在確認、重複制約、GitHub API による Team 存在確認を行う。GitHub API が一時的に利用できない場合に保存を許すなら `verification_status: pending` とし、pending binding は認可には使用しない。

## 実装構成

ドメインと repository を controller から独立させる。

- `entities.TeamConfig`: principal ID、key、表示名、external binding、管理元を保持。
- `TeamConfigRepository`: `FindByPrincipalID`, `FindByKey`, `FindByExternalTeam`, `Save`, `List` を提供。
- `TeamReconciler`: config の検証と冪等反映を担当。
- `TeamDiscoveryService`: membership に discovery rule を適用し、capture から Team key を生成して TeamConfig を atomic create する。
- `TeamMembershipResolver`: linked GitHub identities の membership を ccplant Team principal ID に変換。
- `GitHubMembershipService`: connection ごとの credential と API endpoint を使って membership を取得。
- auth middleware: resolver の結果だけを `AuthorizationContext` に設定。

connection-aware にするため、`GitHubTeamMembership` に少なくとも `ConnectionID` と `ExternalTeamID` を追加する。既存の `KubernetesUserTeamMappingRepository` のキーも username 単独ではなく `(connection_id, github_user_id)` とし、connection をまたぐ同名ユーザーの衝突を防ぐ。

パターン照合は既存の `matchTeamPattern` と同じ意味になる共通 `teampattern` package に切り出す。config validation、Team reconciler、認証時 resolver が同じコンパイル済み matcher を利用し、実装差による権限漏れを防ぐ。binding 数に比例した毎回の全走査を避けるため、connection ID と完全一致 organization を第一キーに index 化し、wildcard organization の matcher だけを別リストで評価する。

認可側では `TeamScope.Teams` と各 resource の `team_id` を principal ID に統一する。UI の選択肢には key/display name を表示し、送信値には principal ID を使う。セッション、schedule、webhook、SlackBot、memory、settings、profile、sandbox policy、API token、service account の Team ID はすべて同じ resolver を通す。

## 移行計画

### Phase 1: モデルと read compatibility

- TeamConfig に `schema_version`, `principal_id`, `team_key`, `external_teams` を追加する。
- v1 TeamConfig (`team_id: org/team`) の読み込み時に、決定的 ID `team_legacy_<sha256(team_id)>` と `team_key = team_id` を補う。保存時に v2 へ更新する。
- API 入力の既存 Team ID を key として解決できる compatibility resolver を追加する。
- 既存リソースは旧 Team ID のまま読めるよう、認可比較の直前に canonical principal ID へ解決する。

### Phase 2: config reconciliation と membership resolver

- `teams` 設定、起動時 reconciler、管理 API を追加する。
- connection-aware membership キャッシュと binding index を追加する。
- `/user` と `/teams` が canonical Team principal ID と表示情報を返す。
- dry-run の移行コマンドで旧 Team ID ごとの対象リソース数と衝突を表示する。

### Phase 3: resource ownership migration

- Team-scoped resource の `team_id` を principal ID に書き換える。
- session 実行トークン、HMAC forwarding header、永続 session metadata も同時に更新する。
- 全 resource repository が canonical ID を保存する状態になった後、旧 ID 入力を deprecation warning 付きにする。

### Phase 4: legacy mapping 廃止

- ccplant Team membership 用途での `team_role_mapping` と `org/team-slug` Team ID を廃止する。
- compatibility resolver と v1 reader は少なくとも 1 リリース残した後に削除する。

## セキュリティと運用上の判断

- membership は常に GitHub connection を含めて評価し、GHES と github.com の namespace を混同しない。
- user principal への identity linking を越えて、login や email の一致だけで membership を統合しない。
- disabled connection、期限切れ token、未検証 binding は認可に使用しない。
- config の typo で Team が消えないよう、reconciler は暗黙削除をしない。
- binding の追加・削除、解決結果、config reconciliation を actor、対象 Team principal、connection、external Team とともに監査ログへ残す。
- binding 削除は次回リクエストから即時反映できるよう index と membership キャッシュを invalidate する。
- GitHub token や membership API のレスポンス本文をログへ出さない。

## 受け入れ条件

- GHES connection の `dev/cc-users` と github.com connection の `ccplant/contributors` を同じ ccplant Team に設定できる。
- どちらか一方、または両方に所属する linked user principal が、同じ Team principal ID で Team resource にアクセスできる。
- 異なる connection 上の同名 organization/team は衝突しない。
- config による初回作成と再起動時の再適用で Team principal ID が変わらない。
- Team key や GitHub team slug の変更で、既存 ccplant resource の所有 Team が変わらない。
- 同じ external GitHub Team を 2 つの ccplant Team に割り当てようとすると設定検証が失敗する。
- `dev/cc-*` のような config pattern に一致する複数の GitHub Team を、1 つの ccplant Team membership source として扱える。
- wildcard により 1 つの GitHub Team が複数 ccplant Team にマッチする場合は、設定適用が失敗して認可には反映されない。
- discovery rule `{team}/cc-users` に `test/cc-users` がマッチすると、不変な principal ID を持つ ccplant `test` Team が初回 membership ロード時に作成される。
- discovery で作成された `test` Team に、管理者が GHEC `myorg/test-cc-users` binding を追加できる。
- 追加後、GHES `test/cc-users` と GHEC `myorg/test-cc-users` のメンバーが同じ `test` Team principal に解決される。
- membership 削除が共有キャッシュ TTL 以内に反映され、それ以降は fail closed になる。
- 既存 `org/team-slug` データが移行期間中も読み書きでき、dry-run で移行対象を確認できる。

## 未決事項

実装開始前に次を確定する。

1. Team principal ID の形式を UUID と ULID のどちらにするか。運用上の視認性から `team_` + ULID を推奨する。
2. 1 つの external GitHub Team を複数 ccplant Team に割り当てるユースケースを将来許可するか。初期仕様は一意制約を推奨する。
3. GitHub API 障害時の stale membership 猶予。セキュリティ優先の既定値は 5 分後 fail closed とする。
4. discovery rule の変更により、既存 Team key と一致しなくなった Team をどう表示するか。自動削除はせず `discovery_status: orphaned` として警告することを推奨する。
5. TeamConfig を現行 Secret に保存し続けるか、汎用 KV store に移すか。検索 index と一意制約を考えると KV store への移行を推奨する。

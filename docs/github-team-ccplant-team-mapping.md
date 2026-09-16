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
- **Team principal ID**: ccplant Team に割り当てる不変かつ一意な ID。形式は `team-` + ULID（例: `team-01K4QX7M9N2R8V5Y3C6D1F0GHA`）。表示名や slug 変更の影響を受けない。
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
  "principal_id": "team-01K4QX7M9N2R8V5Y3C6D1F0GHA",
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

`principal_id` は作成時にサーバーが生成し、更新 API や config reconciliation では変更不可とする。形式は `^team-[0-9A-HJKMNP-TV-Z]{26}$` とし、アンダースコアや URL エンコードが必要な文字は使用しない。ULID は UUID の36文字より短い26文字で、prefix 込みでも31文字に収まる。生成には暗号学的乱数を entropy とする monotonic ULID generator を使用する。ユーザー principal と同じ名前空間に置く場合も、ハイフン区切りの型付き ID (`user-...`, `team-...`) にし、将来の ACL の subject を `(principal_type, principal_id)` で表現できるようにする。今回の実装ではユーザー用 `githubPrincipal` Secret を流用せず、TeamConfig 内に保持する。ユーザー principal は「複数 GitHub identity を同一人物に束ねるもの」、Team principal は「リソース所有・membership の単位」でライフサイクルが異なるためである。

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
                            principal_id = team-...
                                      │
                                      ▼
                         sessions / settings / memories
```

## 設定スキーマと自動作成

GitHub 接続自体と Team 定義を分離し、トップレベルに `team_discovery` と `teams` を追加する。`team_discovery` はパターンに一致した GitHub Team の完全名を ccplant Team key として使い、Team principal を動的に作成する。`teams` は既知の ccplant Team を静的に宣言する用途に残す。

### 動的 Team discovery

GHES の `test/cc-users` から、同名の ccplant `test/cc-users` Team を作成する場合は次のように設定する。

```yaml
team_discovery:
  - connection_id: ghes
    team_pattern: "*/cc-users"
    team_key: "{organization}/{team_slug}"
```

`team_pattern` は GitHub Team の `organization/team-slug` に対する glob である。`team_key` では、マッチした実値を表す組み込み変数 `{organization}` と `{team_slug}` を利用できる。この例では GitHub Team `test/cc-users` に対して `organization = test`, `team_slug = cc-users` となり、次の TeamConfig を初回観測時に作成する。`team_key` を省略した場合も既定値は `{organization}/{team_slug}` とする。

```json
{
  "principal_id": "team-01K4QX7M9N2R8V5Y3C6D1F0GHA",
  "team_key": "test/cc-users",
  "display_name": "test/cc-users",
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

Team の生成契機は、対象 GitHub connection でユーザーの membership をロードしたときとする。たとえば Alice の GHES membership に `test/cc-users` が含まれていれば、resolver は discovery rule にマッチさせ、ccplant `test/cc-users` Team がなければ principal を作成してから Alice をその Team のメンバーとして認可する。全 organization の事前列挙は不要である。

同時に複数ユーザーが初回ログインしても principal が二重作成されないよう、正規化済み `team_key` を一意キーとして atomic create を行う。競合した処理は作成済み TeamConfig を再取得する。既定形式の `team_key` は小文字化された `organization/team-slug` とし、空要素や余分な `/` を含む展開結果は認可せず監査ログへ記録する。

作成した TeamConfig には `discovery_rule_id` とマッチした organization/team slug も保存する。同じ key の Team がすでに存在し、それが同じ discovery rule と GitHub Team から作成されたものなら再利用する。

principal ID を持たない旧形式の TeamConfig が同じ key で存在する場合は例外として、後述する legacy adoption を実行する。たとえば既存の ccplant `test/cc-users` は削除も再作成もせず、その TeamConfig に principal ID と discovery binding を追加して現在の Team として引き継ぐ。principal ID をすでに持つ手動作成 Team や、別 discovery rule 由来 Team と key が衝突した場合だけは、既存 Team へ自動 binding せず fail closed にする。そうしないと、GitHub Team 名を作れるユーザーが既存 ccplant Team に参加できる可能性がある。

初期仕様では `team_key` に指定できる変数を `{organization}` と `{team_slug}` に限定し、glob の `*` 自体を任意名で capture する機能や正規表現は導入しない。これにより Team key の生成規則を単純に保つ。

### 手動での追加マッピング

discovery で `test/cc-users` Team を一度作成した後、管理者は Team 設定 API または UI から GHEC Team を追加できる。

```yaml
# TeamConfig の UI/API 表現。サーバーの bootstrap config ではない。
key: test/cc-users
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
| `ghes` | `test/cc-users` | `test/cc-users` |
| `ghec` | `myorg/test-cc-users` | `test/cc-users` |

GHES membership をロードした Alice と、GHEC membership をロードした Bob は、同じ `test/cc-users` Team principal のメンバーになる。両方の identity が同じ user principal にリンクされている場合も結果は `test/cc-users` 1 件に重複排除する。

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
  "principal_id": "team-01K4QX7M9N2R8V5Y3C6D1F0GHA",
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
- v1 TeamConfig (`team_id: org/team`) の読み込み時は旧 Team ID を維持する。principal ID が必要になった時点で ULID を atomic に付与し、`team_key = team_id` とともに v2 へ更新する。
- API 入力の既存 Team ID を key として解決できる compatibility resolver を追加する。
- 既存リソースは旧 Team ID のまま読めるよう、認可比較の直前に canonical principal ID へ解決する。

#### Legacy Team adoption

既存環境に principal ID を持たない `test/cc-users` TeamConfig があり、discovery が GHES `test/cc-users` を観測した場合は次のように処理する。

1. `team_key = test/cc-users` で既存 TeamConfig を検索する。
2. `principal_id` が空であることを確認する。
3. 新しい `team-<ULID>` を候補 principal ID として生成する。
4. 既存 TeamConfig の service account、env vars、その他の設定を変更せず、`schema_version`, `principal_id`, `team_key`, discovery metadata、GHES `test/cc-users` binding だけを追加する。
5. Kubernetes `resourceVersion` または KV transaction を使って compare-and-swap で保存する。競合時は自分の候補 ULID を破棄して再取得し、先に保存された principal ID を正とする。
6. 既存 resource の `team_id: test/cc-users` はそのまま残し、compatibility resolver が canonical principal ID に変換して認可する。

この adoption は in-place schema upgrade であり、旧 TeamConfig Secret や Team-scoped resource を削除・再作成しない。したがって、過去の session、settings、memory、schedule、webhook、SlackBot、profile、sandbox policy、API token、service account は消えず、同じ Team から参照できる。

途中で処理が失敗した場合は TeamConfig を再取得する。principal ID が保存済みならその ULID を再利用し、未保存なら新しい候補 ULID で compare-and-swap を再試行する。binding の追加まで完了していない Team は `migration_status: pending` として扱い、既存の旧認可経路は維持する。migration 完了前に旧 Team ID の読み取りを無効化してはならない。

自動 adoption の対象は「principal ID が空で、discovery が観測した GitHub Team の完全名と legacy team ID が完全一致する TeamConfig」に限定する。曖昧な候補が複数ある場合や、別 principal がすでに割り当てられている場合は変更せず、管理者に衝突を通知する。

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
- discovery rule `*/cc-users` に `test/cc-users` がマッチすると、不変な principal ID を持つ同名の ccplant `test/cc-users` Team が初回 membership ロード時に作成される。
- principal ID のない既存 ccplant `test/cc-users` Team がある場合、新規 Team を作らず既存 TeamConfig を in-place adoption し、既存設定と Team-scoped resource を維持する。
- discovery で作成された `test/cc-users` Team に、管理者が GHEC `myorg/test-cc-users` binding を追加できる。
- 追加後、GHES `test/cc-users` と GHEC `myorg/test-cc-users` のメンバーが同じ ccplant `test/cc-users` Team principal に解決される。
- membership 削除が共有キャッシュ TTL 以内に反映され、それ以降は fail closed になる。
- 既存 `org/team-slug` データが移行期間中も読み書きでき、dry-run で移行対象を確認できる。

## 未決事項

実装開始前に次を確定する。

1. 1 つの external GitHub Team を複数 ccplant Team に割り当てるユースケースを将来許可するか。初期仕様は一意制約を推奨する。
2. GitHub API 障害時の stale membership 猶予。セキュリティ優先の既定値は 5 分後 fail closed とする。
3. discovery rule の変更により、既存 Team key と一致しなくなった Team をどう表示するか。自動削除はせず `discovery_status: orphaned` として警告することを推奨する。
4. TeamConfig を現行 Secret に保存し続けるか、汎用 KV store に移すか。検索 index と一意制約を考えると KV store への移行を推奨する。

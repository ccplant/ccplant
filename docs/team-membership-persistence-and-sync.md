# Team membership の永続化と GitHub 同期

## 背景

現在の ccplant Team membership は、認証時にリンク済み GitHub identity の token で
GitHub API を呼び出し、取得した GitHub Team membership を `TeamConfig.external_teams`
へ照合して解決している。結果は Kubernetes ConfigMap に保存されるが、有効期限 5 分の
キャッシュであり、期限切れ後の認証や API リクエストは再び GitHub API に依存する。

この構成には次の問題がある。

- GitHub API の障害や rate limit が ccplant の認証・認可に波及する。
- membership がリクエストを契機に変化し、管理者が反映タイミングを制御できない。
- キャッシュ期限後は membership が消えるため、永続的なチーム名簿として扱えない。
- どの同期で誰が追加・削除されたかを監査しにくい。

本設計では、ccplant Team ごとに membership を永続化し、チーム設定画面の「GitHub
からメンバーを同期」ボタン、または GitHub identity を伴う初回ユーザー作成・アカウント連携を
契機に GitHub Team から更新する。同期 API は Team ごとに 1 分に 1 回までとする。通常のログインや
認証済み API request では GitHub API を呼ばない。

## 要件

- `TeamConfig.external_teams` に設定したすべての GitHub Team のメンバーを取得する。
- 複数の GitHub Team に同じ user principal が存在しても 1 メンバーにまとめる。
- 同期済み membership は Pod 再起動後も保持する。
- 同期完了後の認可は保存済み membership を即時に参照する。
- 同期に失敗した場合は、直前の正常な membership を維持する。
- 同じ ccplant Team の同期は、利用者や replica が異なっても 60 秒に 1 回までにする。
- 同期の実行者、時刻、結果、追加・削除件数を記録する。
- GitHub identity を伴う初回ユーザー作成・アカウント連携時は、その identity が参加している
  GitHub Team に対応する ccplant Team の同期を自動で開始する。

初期リリースでは定期同期、GitHub webhook による自動同期、UI での手動メンバー追加は
対象外とする。

## 基本方針

### 外部スナップショットと認可用 membership を分ける

GitHub のメンバー一覧には、まだ ccplant にログインしておらず user principal が存在しない
利用者も含まれる。そのため、GitHub user ID を保持する外部スナップショットと、既存の
GitHub identity を user principal に解決した認可用 membership を同時に保存する。

```text
GitHub Team bindings
        |
        | 手動 sync / identity lifecycle sync
        v
external member snapshot (GitHub user ID)
        |
        | linked identity と照合
        v
ccplant Team membership (user principal ID)
        |
        v
AuthorizationContext.TeamScope
```

外部スナップショットを残すことで、同期後に初めてログイン・identity link した GitHub
ユーザーも GitHub API を再実行せず membership を解決できる。identity の作成・再リンク時に、
保存済みスナップショットを GitHub connection ID と GitHub user ID で検索し、認可用
membership を再構築する。

### GitHub connection を識別子に含める

既存の Team mapping は organization/team slug を connection 非依存で一致させるが、GitHub
user ID は GitHub.com と GHES の間で一意ではない。外部メンバーの一意キーは
`(connection_id, github_user_id)` とする。同一 user principal に複数 identity がリンクされて
いる場合は principal ID で重複排除する。

## データモデル

TeamConfig のレコードに名簿を同居させると、メンバー数に比例して設定更新時の競合とレコード
サイズが増える。このため membership 専用 repository を追加する。repository は
`kvstore.Store` を直接利用し、Team principal ID ごとに1レコードを保存する。Kubernetes、libSQL、
暗号化・replicated backend の違いは `kvstore.Store` 実装側で吸収する。

```go
type TeamMembershipSnapshot struct {
    TeamPrincipalID string
    Generation      int64
    BindingRevision string
    ExternalMembers []ExternalTeamMember
    Members         []TeamMember
    SyncedAt        time.Time
    SyncedBy        string // actor principal ID
}

type ExternalTeamMember struct {
    ConnectionID string
    GitHubUserID int64
    Login        string // 表示・監査用。認可キーには使わない
    Sources      []ExternalTeamRef
}

type ExternalTeamRef struct {
    Organization string
    TeamSlug     string
    Role         string // GitHub の member / maintainer。ccplant 権限には変換しない
}

type TeamMember struct {
    PrincipalID string
    Sources     []ExternalIdentityRef
}

type ExternalIdentityRef struct {
    ConnectionID string
    GitHubUserID int64
}
```

永続レコードには `schema_version` も持たせる。配列は決定的なキー順で sort し、差分とテストを
安定させる。`login` や GitHub Team role は監査情報であり、認可には principal ID のみを使う。

repository interface は少なくとも次を提供する。

```go
type TeamMembershipRepository interface {
    Get(ctx context.Context, teamPrincipalID string) (*TeamMembershipSnapshot, error)
    Replace(ctx context.Context, snapshot *TeamMembershipSnapshot, expectedGeneration int64) error
    FindTeamsByExternalIdentity(ctx context.Context, connectionID string, githubUserID int64) ([]string, error)
}
```

`Replace` は generation と `kvstore.Record.Version` による compare-and-swap とする。
`FindTeamsByExternalIdentity` 用の index は再構築可能な派生データ
とし、snapshot 更新と同一トランザクションにできない backend では、新 snapshot を正として
index を冪等に reconciliation する。

レコードkeyには可変な team name ではなく principal ID を用いる。
例: `agentapi-team-membership-team-01...`。GitHub user ID や login は機密 token ではないが、
チーム所属情報はアクセス制限すべきデータなので `kvstore.KindSecret` として保存する。

## 同期処理

同期は `TeamMembershipSyncService` に実装し、controller から GitHub/KV backend の詳細を
分離する。

1. actor が Team の管理権限を持つことを検証する。
2. Team principal ID に対する分散 rate limit を確保する。
3. TeamConfig と、その binding 内容から算出した `binding_revision` を読み込む。
4. binding ごとに、actor がその connection にリンクした GitHub identity/token を持つことを
   検証する。
5. GitHub REST API の `GET /orgs/{org}/teams/{team_slug}/members?role=all&per_page=100`
   を pagination し、必要なら各 membership の role を取得する。
6. 全 binding の結果を `(connection_id, github_user_id)` で統合する。
7. GitHub identity repository で user principal ID に解決し、principal ID で重複排除する。
8. TeamConfig を再取得し `binding_revision` が変わっていないことを確認する。
9. snapshot 全体を atomic replace する。保存後に identity index と認可キャッシュを invalidate
   する。
10. 追加・削除・未リンクの件数を監査ログへ記録してレスポンスする。

複数 binding のうち 1 件でも取得できない場合は保存しない。部分結果を保存すると、GitHub API
障害だけで大量のメンバー削除が発生するためである。空の Team が GitHub から正常に返った場合は
空 snapshot を保存し、既存メンバーを全削除する。

### 同期に使う credential

同期ボタンを押した actor の、各 `connection_id` にリンク済みの OAuth token を使う。サーバー
共通 token や別ユーザーの token へ暗黙にフォールバックしない。actor が全 binding を読み取れない
場合は `422 Unprocessable Entity` とし、不足している connection ID のみを返す。token 自体や
GitHub レスポンス本文はログへ出さない。

GitHub App の organization installation token へ将来切り替える場合も service の credential
provider を差し替え、snapshot と API 契約は維持する。

### 初回ユーザー作成・アカウント連携時の自動同期

次のイベントでは、保存済み membership の参照だけでなく自動同期を開始する。

- GitHub identity と同時に user principal を初めて作成したとき。
- 既存 user principal に新しい GitHub identity をリンクしたとき。

通常ログイン、token refresh、同じ identity での再認証では開始しない。GitHub identity を伴わない
Google 等からの初回ユーザー作成も対象 Team を判定できないため何もしない。

処理フローは次のとおりとする。

1. identity と token の保存を完了し、user principal との link を確定する。
2. その identity の token で GitHub REST API `GET /user/teams?per_page=100` を pagination する。
3. `(connection_id, organization, team_slug)` を正規化し、設定済み external binding と照合する。
4. 一致した ccplant Team principal ID を重複排除する。
5. Team ごとに `reason=identity_created` または `reason=identity_linked` として、手動同期と同じ
   `TeamMembershipSyncService`、credential 検証、全 binding の atomic replace、rate limit、lease
   を使って同期する。

自動同期の対象は「連携したユーザーが現在参加している GitHub Team」に対応する ccplant Team
だけである。設定済み Team 全件や、その connection 上の無関係な Team は同期しない。1つの GitHub
Team が同じ ccplant Team の複数 binding に一致しても operation は1件にまとめる。

identity の作成・link 自体は GitHub の一時障害や同期失敗を理由に rollback しない。初期実装では
identity 保存後、callback のレスポンスを返す前にベストエフォートで同期する。同期失敗後も手動
ボタンから再実行できる。Team が非常に多い場合に callback latency が問題になった時点で、永続
operation/outbox を導入してバックグラウンド処理へ移行する。

`/user/teams` の取得に失敗した場合は対象 Team を推測せず、自動同期 operation 全体を failed と
して監査する。既存 snapshot と identity link は維持する。GitHub Team への参加確認だけから本人を
部分的に snapshot へ追加することはせず、必ず対象 Team の全 binding を取得して snapshot 全体を
置換する。

連携完了後、クライアントは `/user` と Team 一覧を再取得する。同期に失敗した Team は保存済み
snapshot を維持し、設定画面から再同期できる。

### binding の扱い

初期実装の同期対象は organization/team slug が完全一致する binding に限定する。wildcard
binding は 1 binding が複数 GitHub Team に展開されるため、GitHub Team 一覧の列挙・競合検証・
展開結果の永続化が別途必要になる。wildcard が存在する Team の同期は `422` とし、UI に対象外
であることを表示する。wildcard 対応時は、同期前に pattern を具体的な Team ID 一覧へ展開し、
その展開結果も snapshot に保存する。

## rate limit と排他制御

制限単位は `(team_principal_id, operation=membership-sync)`、間隔は成功・失敗を問わず同期開始から
60 秒とする。ブラウザー側のボタン disable は補助であり、判定は必ず backend で行う。

単一 Pod の memory limiter では replica 間で制限できないため、共有 store に次を保存する。

```json
{
  "last_started_at": "2026-10-01T12:00:00Z",
  "lease_until": "2026-10-01T12:02:00Z",
  "operation_id": "01K..."
}
```

- atomic compare-and-set で、前回開始から 60 秒未満なら `429 Too Many Requests` を返す。
- レスポンスに `Retry-After`（秒）と `next_sync_at` を含める。
- 実行中 lease があれば `409 Conflict` を返し、同じ Team の同時同期を防ぐ。
- lease は GitHub API timeout より長い 2 分とし、process crash 後に自動解放される。
- GitHub API の `403/429` は upstream の reset 時刻を可能な範囲でレスポンスへ反映するが、
  ccplant 側の 60 秒制限とは別に扱う。

失敗を rate limit に含めることで、権限不足や GitHub 障害時の連打も GitHub API へ波及しない。
手動・自動は同じ制限枠を使う。自動同期が 60 秒枠や実行中 lease に当たった場合は、その回を
成功扱いでスキップする。手動 API は利用者へ即時フィードバックするため従来どおり `409` / `429`
を返す。

## API

既存 Team API の認可規則に合わせ、Team owner、Team member、platform admin のうち
`canManageTeam` を満たす actor に許可する。将来 member/maintainer を分離した場合は
`team:members:sync` permission に限定する。

### 状態取得

```http
GET /teams/{team}/members
```

```json
{
  "members": [
    {
      "principal_id": "user-01K...",
      "display_name": "Alice",
      "github_logins": ["alice"]
    }
  ],
  "unlinked_external_member_count": 3,
  "sync": {
    "status": "succeeded",
    "synced_at": "2026-10-01T12:00:08Z",
    "synced_by": "user-01J...",
    "next_sync_at": "2026-10-01T12:01:00Z"
  }
}
```

未同期の場合は `members: []`, `sync.status: "never"` を返す。外部ユーザーの login 一覧は
管理権限を持つ actor にのみ返し、一般的な `/user` レスポンスには含めない。

### 同期開始

```http
POST /teams/{team}/members/sync
Idempotency-Key: <uuid>
```

同期は通常数秒で完了するため初期実装は同期 HTTP 処理とし、成功時に `200 OK` で状態と差分を
返す。client disconnect で保存が中断されないよう service context にはサーバー側 timeout を使う。
大規模 Team で request timeout を超える場合は、同じ API を `202 Accepted` の非同期 operation
へ変更できるよう `operation_id` をレスポンスに含める。

```json
{
  "operation_id": "01K...",
  "synced_at": "2026-10-01T12:00:08Z",
  "next_sync_at": "2026-10-01T12:01:00Z",
  "member_count": 42,
  "unlinked_external_member_count": 3,
  "added_count": 2,
  "removed_count": 1
}
```

主なエラーは次のとおり。

| status | 条件 |
|---|---|
| `403` | Team 管理権限がない |
| `409` | 同期実行中、または同期中に binding が変更された |
| `422` | connection token 不足、完全一致でない binding、GitHub Team を参照できない |
| `429` | 前回開始から 60 秒未満。`Retry-After` を返す |
| `502` | GitHub API が失敗した。保存済み snapshot は維持する |

## UI

`設定 > チーム > GitHub チーム` に「メンバー」section を追加する。

- 最終同期日時、同期実行者、解決済みメンバー数、未リンク外部メンバー数を表示する。
- 「GitHub からメンバーを同期」ボタンを配置する。
- 押下直後は loading 表示にし、完了まで二重送信を防ぐ。
- 成功後は追加・削除件数を表示して membership 一覧を再取得する。
- `next_sync_at` まではボタンを disable にし、残り秒数を表示する。
- `429` の場合は server の `Retry-After` / `next_sync_at` で countdown を補正する。
- 他 replica・別ブラウザーからの実行を考慮し、画面上で有効でも backend の `409/429` を表示する。
- binding が 0 件、wildcard がある、必要な connection が未リンクの場合は理由を添えて disable
  またはエラー表示する。

フロントエンドの localStorage や timer は rate limit の正本にはしない。

## 認証・認可フローの変更

`GitHubConnectionsController.ResolveTeamMemberships` と `GitHubAuthProvider` の通常認証時 GitHub
Team 取得を、保存済み membership の参照へ置き換える。例外は前述の identity を伴う初回ユーザー
作成・アカウント連携イベントだけとする。

1. 認証した identity を `(connection_id, github_user_id)` として確定する。
2. membership index から該当する Team principal ID を取得する。
3. linked identity が複数ある場合は Team principal ID の和集合を作る。
4. `AuthorizationContext.TeamScope.Teams` へ設定する。

snapshot が未作成でも通常認証から GitHub API へフォールバックしない。未同期 Team の GitHub
binding からは membership を付与せず、自動同期完了後の再取得で付与する。一方、Team owner と
platform admin の管理アクセスは既存どおり保持し、初回同期を実行可能にする。

同期による削除は次のリクエストから有効にする。既存の API key/session が TeamScope を token 内へ
固定している場合は、各リクエストで保存済み membership と再照合するか、同期時に関連 session を
失効させる。初期実装では権限剥奪を即時反映しやすい前者を推奨する。

## 整合性と失敗時の動作

- snapshot は全 binding の取得成功後に 1 回だけ置換する。
- 同期中に binding が変わった場合は保存せず、再同期を促す。
- identity link/unlink は snapshot を変更せず、認可用 `Members` と index のみ再構築する。
- identity 作成・link 後の自動同期失敗は identity transaction を rollback せず、snapshot も変更しない。
- Team 削除時は membership snapshot と index を同時に削除対象にする。
- snapshot repository が読めない場合は fail closed とし、owner/admin の復旧操作だけ許可する。
- 保存済み snapshot に有効期限は設けない。membership の鮮度は `synced_at` として UI・監査へ
  明示し、変更反映は明示同期に委ねる。

「永続化」を要件とするため、GitHub 障害や長期間同期されていないことだけを理由に既存 membership
を自動失効させない。ただし運用ポリシーとして最大許容期間が必要になった場合は、別設定で
`max_membership_age` を導入し、期限超過時に fail closed とする。

## 監査・可観測性

監査イベント `team_membership.sync_started` / `succeeded` / `failed` を追加し、次を記録する。

- operation ID、Team principal ID、actor principal ID
- 起動理由 (`manual`, `identity_created`, `identity_linked`)
- binding revision と対象 binding 数（token は記録しない）
- 開始・終了時刻、追加・削除・未リンク・総メンバー数
- 失敗分類 (`credential_missing`, `github_rate_limited`, `github_unavailable`,
  `binding_changed`, `persistence_failed`)

metrics には同期回数、失敗数、所要時間、GitHub API request 数、最終成功からの経過時間を追加する。
個々の login や user ID は metric label にしない。

## 移行手順

1. membership repository、同期 service、状態取得 API を追加する。この段階では認可は旧経路のまま。
2. identity lifecycle の自動同期 trigger を追加する。
3. UI に同期ボタンを追加し、各 Team で初回 snapshot を作れるようにする。
4. shadow mode で旧 resolver の結果と snapshot の差分を記録する。
5. snapshot が存在する Team から保存済み membership を認可の正本へ切り替える。
6. 全 Team の移行後、通常認証時の GitHub Team API 呼び出しと TTL 付き
   `agentapi-user-team-mapping` ConfigMap を廃止する。

切り替え期間中に snapshot がない Team は旧 resolver を使える feature flag を用意する。ただし
Team ごとに snapshot が一度作成された後は旧 resolver へ戻さない。これにより空 Team の正常同期を
「未同期」と誤認して旧 membership を復活させることを防ぐ。

## テスト方針

- domain: connection をまたぐ重複排除、未リンク identity、空 Team、決定的 sort。
- repository: atomic replace、generation conflict、Pod 再起動相当の再生成、index reconciliation。
- service: 複数 binding の全成功、部分失敗時の非更新、binding revision conflict。
- rate limit: 同一 Team の並行実行、複数 replica 相当、60 秒境界、lease timeout。
- lifecycle trigger: 初回作成、identity link、通常ログインでは非発火、対象 Team の絞り込み、
  rate limit 時のスキップ。
- authorization: 保存済み追加・削除の即時反映、未同期時 fail closed、owner/admin の初回同期。
- API: `403/409/422/429/502`、`Retry-After`、token/レスポンス本文を漏らさないこと。
- frontend: loading、countdown、別 client による `429`、成功差分、各 disable 理由。

## 受け入れ条件

- 同期成功後に Pod を再起動しても Team membership が維持される。
- GitHub identity を伴う初回ユーザー作成・アカウント連携では、本人が参加する GitHub Team に
  対応する ccplant Team だけが自動同期される。
- 通常のログイン・token refresh・認証済み API request では GitHub Team API が呼ばれない。
- 自動同期が失敗してもユーザー作成・identity link は成功し、既存 snapshot は維持される。
- 手動同期と自動同期が競合しても、Team 単位の60秒制限により重複実行されない。
- ボタン押下で設定済み GitHub Team の現在のメンバーが保存され、追加・削除が次の request から
  認可へ反映される。
- いずれかの binding 取得に失敗しても、直前の正常な名簿が変化しない。
- 同一 Team に対する 60 秒以内の再同期は、Pod や actor が異なっても `429` になる。
- 同期中の binding 変更が古い結果で上書きされない。
- 未リンク GitHub user が後から identity をリンクすると、GitHub 再同期なしで Team に解決される。
- 同期履歴から actor、時刻、追加・削除件数、失敗理由を確認できる。

## 実装順序

1. `TeamMembershipSnapshot` と repository、identity index。
2. 分散 rate limiter/lease と `TeamMembershipSyncService`。
3. identity lifecycle trigger。
4. members/status/sync API と監査イベント。
5. GitHub チーム設定画面の状態表示・同期ボタン・countdown。
6. 保存済み membership resolver と request ごとの認可再照合。
7. shadow mode、段階的切り替え、旧 user-team cache の削除。

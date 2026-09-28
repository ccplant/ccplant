# Secret settings feature design

## Summary

AgentAPI Proxy の Personal / Team Settings に `Secrets` 機能を追加する。Secret を独立した top-level resource として公開せず、既存 Settings の一部として作成・更新・削除し、同じ設定 scope で利用する。

Settings 画面では秘密値と用途をまとめて設定できる。

- セッションの環境変数として渡す
- セッション内の file として配置する

SlackBot の bot token / app token も同じ Secrets API に任意の KV として保存する。最初の bot の作成・編集画面から Secret を作成し、その Secret と key mapping を同じ scope の別 bot から再利用する。Secret に `slackbot` のような用途別 kind は持たせず、独立した Slack credential model/repository/API も作らない。

秘密値は保存後に再表示しない。通常の Settings API、ログ、イベント、MCP tool、simulation、dry-run にも含めない。エージェントが Secret API を使って任意の値を読む機能や、値をプロンプトへ埋め込む機能は提供しない。

## User experience

Settings の side navigation に `Secrets` を追加する。

```text
Settings
├── Personal / Team scope switcher
├── Agents
├── AI Providers
├── Environment Variables
├── Files
├── Secrets                 <- new
├── MCP Servers
└── ...
```

`/settings/personal/secrets` と `/settings/team/:team/secrets` は既存の Settings shell と scope switcher を利用する。Team では既存どおり対象 team を選択し、team settings を変更できる権限がある場合だけ編集を許可する。side navigation では「セッション環境」group の Environment Variables と Session Files の間に配置し、専用 API で即時保存するため dirty field は持たせない。

Secrets 一覧には次だけを表示する。

- 表示名
- key 名と configured 状態
- key ごとの投影方法（environment / file / none）
- 更新日時
- 参照中の SlackBot（該当する場合）

値は password input で新規入力し、保存後は空に戻す。「変更」は新しい値で置換し、「削除」は確認 dialog を表示する。ブラウザの localStorage、URL、analytics、error reporting に値を保存しない。

作成フォーム例:

```text
Name:          GitHub token
Key:           github-token
Value:         ********
Expose to agents:
  ( ) Do not mount
  (x) Environment variable   GITHUB_TOKEN
  ( ) File                   /home/agentapi/.config/example/token
```

一つの Secret は任意の複数 key/value を持てる。各 key は projection なしの KV として保持するか、env/file としてエージェントへ投影できる。SlackBot 画面は `bot-token` と `app-token` の二 key を持ち、projection のない Secret を同じ API で作成する。

projection のない key は保存されるだけで、全セッションへ自動注入されない。値を利用できるのは、SlackBot のように Secret ID と key を保持し、server-side resolver が明示的に解決する consumer だけとする。

## Settings model

`entities.Settings` に Secret settings を追加する。

```go
type SecretSetting struct {
    ID          string
    Name        string
    Values      map[string]string // storage/internal resolution only
    Projections []SecretProjection
    Version     int64
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type SecretProjection struct {
    Key         string
    Type        string // none | env | file
    EnvName     string
    Path        string
    Permissions string // 0400 | 0600
}

type SecretSettingMetadata struct {
    ID          string             `json:"id"`
    Name        string             `json:"name"`
    Keys        []string           `json:"keys"`
    Projections []SecretProjection `json:"projections,omitempty"`
    Version     int64              `json:"version"`
    CreatedAt   time.Time          `json:"created_at"`
    UpdatedAt   time.Time          `json:"updated_at"`
}
```

`SettingsResponse` には `secret_settings []SecretSettingMetadata` だけを含める。storage/internal entity と response DTO を分離し、`Value` を誤って serialize できない構造にする。

ID は server-generated UUID/ULID とする。SlackBot からは表示名ではなく ID を参照し、rename の影響を避ける。

Secret は Settings の所有 scope を暗黙に継承するため、Secret 独自の `scope`、`owner_id`、`team_id`、ownership transfer は持たない。Personal から Team へ移す場合は、移動先で新しい値を設定し、参照を付け替えてから元を削除する。

## API

Secret は Settings の一機能だが、通常の Settings 一括 PUT に秘密値を混ぜない。既存フォームの保存や古い client が値を消す事故を防ぐため、Settings 配下に write-only の操作 endpoint を設ける。

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/settings/:name/secrets` | metadata のみ一覧 |
| `POST` | `/settings/:name/secrets` | secret setting を追加 |
| `PATCH` | `/settings/:name/secrets/:id` | name、projection を更新 |
| `PUT` | `/settings/:name/secrets/:id/values` | key/value map を全置換 |
| `DELETE` | `/settings/:name/secrets/:id` | 未参照の場合に削除 |

これらは top-level resource API ではなく、既存 Settings controller の sub-feature として実装する。`:name` の user/team 解決と認可は既存 `/settings/:name` と同じ処理を必ず共用する。

作成要求:

```json
{
  "name": "GitHub token",
  "values": {"github-token": "ghp_..."},
  "projections": [{
    "key": "github-token",
    "type": "env",
    "env_name": "GITHUB_TOKEN"
  }]
}
```

応答:

```json
{
  "id": "sec_01...",
  "name": "GitHub token",
  "keys": ["github-token"],
  "projections": [{
    "key": "github-token",
    "type": "env",
    "env_name": "GITHUB_TOKEN"
  }],
  "version": 1,
  "created_at": "...",
  "updated_at": "..."
}
```

create/value update の応答にも値を含めず、`Cache-Control: no-store` を付ける。update には `base_version` を要求し、競合時は `409 Conflict` を返す。

CLI を追加する場合も `settings secret` namespace とする。

```text
ccplant client settings secret list --scope personal
ccplant client settings secret create --name github-token --from-file ./token --env GITHUB_TOKEN
ccplant client settings secret set-values sec_... --from-file github-token=./token --base-version 1
ccplant client settings secret delete sec_...
```

秘密値を command line argument に置かず、stdin または file から受け取る。

## Storage and encryption

Secret settings は既存 `SettingsRepository` と同じ settings document に保存する。ただし値は `env_vars` と同様に key ごとに既存 `EncryptionServiceRegistry` で envelope encryption し、metadata とは別 field に格納する。

```json
{
  "secret_settings": [
    {
      "id": "sec_01...",
      "name": "GitHub token",
      "projections": [{"key": "github-token", "type": "env", "env_name": "GITHUB_TOKEN"}],
      "encrypted_values": {"github-token": {
        "encrypted_value": "...",
        "algorithm": "...",
        "key_id": "...",
        "encrypted_at": "...",
        "version": 1
      }}
    }
  ]
}
```

- encryption service が `noop` の production 構成では Secret settings の保存を拒否する。
- Kubernetes backend でも application-level encryption を行う。
- libSQL/replicated backend でも settings document の移行方式にそのまま従う。
- Settings 全体の save 時は、request に含まれない既存 Secret settings を必ず保持する。
- secret value、復号後 payload、暗号鍵をログへ出さない。

同じ settings document を更新する既存 API と競合しないよう、repository update は storage resource version を使った optimistic concurrency と retry を行う。Secret endpoint は読み込み・変更・保存を一つの use case に閉じ込める。

## Session injection

セッション作成時に、launch scope に対応する Settings から projection が `env` または `file` の Secret settings を解決する。

```text
Personal session -> personal Settings Secrets
Team session     -> selected Team Settings Secrets
```

Team session に personal Secret を自動混入させず、Personal session に Team Secret も混入させない。Schedule、Webhook、SlackBot、Session Profile、direct start、External Session Manager は最終的な session scope/team ID を使い、共通 `SettingsSecretResolver` を通る。

解決は session launch ごとに行い、`(secret ID, version)` と material の snapshot を既存の保護された provision settings に固定する。

- 新規セッションは最新値を使う。
- 実行中セッションには hot reload しない。
- restart/resume は再現性のため元の snapshot を使う。
- 「最新 Settings で再作成」は既存の settings reload/restart 操作へ明示的に統合する。
- External Session Manager には親で解決した snapshot を渡し、remote manager に Settings 全体の読取権限を与えない。

### Environment projection

- env name は `^[A-Za-z_][A-Za-z0-9_]*$`。
- `PATH`、`HOME`、`LD_PRELOAD`、AgentAPI control token などの予約名は禁止する。
- secret value には通常 env input の shell character 制限を適用しない。shell 展開せず process environment として渡す。
- plaintext `env_vars`、request environment、profile environment、内部生成 env と名前が衝突した場合は、暗黙に上書きせず session launch をエラーにする。

### File projection

- absolute path のみ許可し、clean 後の path が許可 root 配下であることを確認する。
- `/proc`、`/sys`、`/dev`、service account token、control files、managed credential の予約 path は禁止する。
- default mode は `0600`。`0400` と `0600` だけを許可する。
- provisioner は symlink traversal を拒否し、atomic write する。
- credentials、profile files、user-managed files、別 Secret と path が衝突した場合は launch を拒否する。
- Secret file は managed-file sync の対象外とし、session から Settings へ書き戻さない。

既存 `SessionSettings.Files` に `Source` / `Sync=false` を追加するか、`SecretFiles` を分離し、provisioner が Secret file を同期対象にしないことを型で保証する。

## SlackBot integration

SlackBot の作成・編集画面に「Slack 認証 Secret」を置き、次の二つから選択する。

1. `新しい Secret を作成`: 表示名、bot token、app token を入力する。画面は Settings Secrets API で任意 KV Secret を作成し、その ID と key mapping を bot に設定する。
2. `既存の Secret を再利用`: 同じ Personal / Team scope の別 SlackBot が参照している Secret を選ぶ。

```text
SlackBot: Incident Bot
Slack Secret:
  (x) Create new
      Name: Production Slack App
      Bot token: ********
      App token: ********
  ( ) Reuse existing
      [ Production Slack App ▼ ]
```

作成後、token は表示しない。SlackBot 一覧・編集画面では Secret の表示名と configured 状態だけを表示する。同じ Secret を参照する bot は同じ Slack App / Socket Mode connection を利用できる。

### Secret reference

新しい domain entity や repository は追加しない。`SlackBot` に Settings Secret の参照だけを追加する。

```go
type SlackSecretRef struct {
    SecretID    string `json:"secret_id"`
    BotTokenKey string `json:"bot_token_key"`
    AppTokenKey string `json:"app_token_key"`
}
```

`SlackBot` は token や Kubernetes Secret name/key ではなく `SlackSecretRef` を保持する。一つの Secret は複数 bot から参照できる。Personal Secret は同じ user の bot、Team Secret は同じ team の botだけが利用でき、scope をまたぐ共有は許可しない。

SlackBot から参照するときに次を検証する。Secret 自体には用途を表す kind や Slack 固有 validation を持たせない。

- bot token key と app token key が同じ Secret に存在する。
- 二つの key は異なり、値が configured である。
- 参照する key に env/file projection がない。
- Secret と bot の Settings scope が一致する。

初回の作成導線では key を `bot-token` / `app-token` として自動設定する。再利用時は既存 bot の `SlackSecretRef` をそのままコピーするため、利用者に key mapping を再入力させない。Settings の Secrets 一覧には通常の KV Secret として metadata を表示する。agent/MCP から値を取得することはできない。

### API flow

新しい Slack Secret を使う場合、frontend は次の順に既存 API を呼ぶ。

```text
1. POST /settings/:name/secrets
2. POST /slackbots with slack_secret=<created secret/key mapping>
```

Secret 作成 request:

```json
{
  "name": "Production Slack App",
  "values": {
    "bot-token": "xoxb-...",
    "app-token": "xapp-..."
  }
}
```

bot 作成 request:

```json
{
  "name": "Incident Bot",
  "slack_secret": {
    "secret_id": "sec_01...",
    "bot_token_key": "bot-token",
    "app_token_key": "app-token"
  }
}
```

再利用候補は、同じ scope の SlackBot 一覧にある `SlackSecretRef` を Secret ID でまとめ、`GET /settings/:name/secrets` の metadata と結合して表示する。任意 Secret を直接選ぶ UI にはせず、「一度 SlackBot で設定した Secret をほかの bot で使い回す」という導線にする。Slack 専用 CRUD API は追加しない。rotation も `PUT /settings/:name/secrets/:id/values` で対象 KV を更新する。

Secret 作成後に bot 作成が失敗した場合、frontend は Secret を自動削除せず「Secret は作成済み」と表示して再試行または手動削除を選ばせる。server-side transaction のために別 model/API を追加しない。

- create/reuse 時に bot と Secret の owner Settings scope、key mapping、projection なしを検証する。
- token 更新時は、該当 Secret worker を graceful reconnect する。
- event が失われても既存 reconcile loop が `(secret ID, version)` の変化を検出して収束する。
- 参照中 Secret の削除は `409 Conflict` とし、参照 bot の ID を metadata として返す。
- bot 削除時に Secret は自動削除しない。他の bot が再利用でき、最後の参照が消えた後も明示削除まで保持する。
- simulation、GET/list、監査ログには Secret ID、表示名、configured、version だけを含める。

### Socket Mode worker topology

同じ Slack Secret で bot ごとに Socket Mode connection を開くと、Slack が event を connection 間で分配し、参照する全 bot が同じ event を評価できない。そのため worker は bot ID ではなく Secret ID を単位にする。

```text
Slack Secret A
  -> one Socket Mode connection
      -> event dispatcher
          -> SlackBot 1 filters / templates
          -> SlackBot 2 filters / templates
          -> SlackBot 3 filters / templates
```

- `SocketManager` は active bot を Secret ID で group 化し、Secret ごとに一 worker を起動する。
- dispatcher は受信 event を、その Secret を参照する全 active bot に渡す。
- bot ごとの channel/user/event filter、dedup、`max_sessions` は現在の単位を維持する。
- Secret を共有する bot の追加・削除・pause は dispatcher の対象だけを更新し、connection は維持する。
- token rotation または Secret 参照の変更時だけ該当 worker を reconnect する。
- 同じ event から複数 bot が意図的に条件一致した場合は、それぞれ起動してよい。UI に共有時の挙動を説明する。

### Storage and migration

Slack token は通常の Secret settings と同じ Settings document、暗号化、versioning を使う。Slack 専用 repository/storage key は追加しない。resolver は Secret ID、owner scope、指定 key、projection なしを確認してから二つの値を復号する。

既存の inline `bot_token` / `app_token` と Kubernetes `bot_token_secret_name` は read compatibility を維持する。既存 bot の編集時または移行 command で、現在の token pair から KV Secret を作り、その bot を Secret/key 参照へ切り替える。同じ token pair を持つ bot を自動でまとめることはせず、管理者が明示的に既存 bot の Secret を選んだ場合だけ共有する。移行中も token を API/CLI output に出さない。

## Relationship to existing settings

### Environment Variables

既存 `env_vars` は非機密の値向けとして残す。値を GET しない現在の動作は維持するが、UI では認証 token や password には Secrets を案内する。Secret env と通常 env の同名設定は保存時にも警告し、launch 時には必ず拒否する。

### Files

既存 `/files` は内容を GET でき、session から同期し戻せるため Secret file と統合しない。Settings の navigation 上は隣接させ、違いを説明する。

### Managed credentials

Codex/Claude credentials は所定 path、credential source selection、session からの更新同期という固有 semantics があるため維持する。Secret file と path が衝突した場合は拒否する。

### Session Profiles

MVP では Secret の定義と投影先は Personal / Team Settings に置く。Session Profile ごとの Secret 選択・override は追加しない。必要になった場合は profile が同じ owner Settings 内の Secret ID を参照する拡張を別途設計する。

## Authorization and failure behavior

Secret 専用 ownership/transfer permission は追加せず、既存 Settings の閲覧・更新権限を使う。metadata の閲覧は Settings read、作成・更新・削除は Settings write と同じ認可にする。内部 resolver だけが復号値へアクセスできる。

SlackBot 画面から Secret を新規作成・rotation・削除する操作にも既存 Settings write 権限を要求する。既存 Secret の選択には Settings metadata read と SlackBot create/update の両方を要求する。内部 Slack worker は bot に保存された Secret/key 参照だけを復号できる。

- Secret/value 不在、復号失敗: session を作らず `422`。
- projection の形式不正: Settings 保存時に `400`。
- 実行時 collision/予約 target: `422`。
- stale `base_version`: `409` と current version。
- Slack Secret 解決失敗: worker を開始せず reason code を状態へ記録。
- 権限不一致と存在しない Settings Secret を区別すべきでない場面では `404` に統一する。

error、audit、metric には値を含めない。監査情報は actor、settings name、Secret ID、operation、version、結果だけにする。

## Rollout plan

1. `Settings` entity/repository に暗号化 Secret settings と metadata-only controller API を追加する。
2. frontend Settings shell に Personal/Team 対応の Secrets section を追加する。
3. 共通 resolver と env/file injection を全 session launch path に追加する。
4. SlackBot に Secret ID 参照、Secrets API を使う create/reuse UI、Secret 単位 worker と rotation reconcile を追加する。
5. CLI と legacy SlackBot token の移行 command を追加する。

feature flag で Secrets UI/API、session injection、SlackBot integration を個別に有効化する。旧 SlackBot 形式の read compatibility は少なくとも一つの deprecation cycle 維持する。

## Test plan

- Settings repository: encryption/decryption、noop rejection、round-trip、concurrent update、通常 Settings PUT で値を保持。
- API: Personal/Team 認可、metadata redaction、`Cache-Control: no-store`、値が response/error/log に残らないこと。
- UI: scope switching、値を DOM/localStorage に保持しないこと、Secret の create/rotate/delete。
- launch: 全起動経路、scope isolation、env/file injection、collision、reserved target、rotation snapshot。
- provisioner: permission、atomic write、symlink/path traversal、Secret file が sync されないこと。
- External Session Manager: remote に Settings access を与えず snapshot が渡ること。
- SlackBot: SlackBot 画面からの Slack Secret 作成 / 既存 bot の Secret 再利用、key/scope/projection validation、参照中削除拒否、rotation reconnect、reconcile fallback、legacy compatibility。
- shared Slack worker: Secret ごとに connection が一つだけ作られ、event が参照中の全 bot へ dispatch され、bot ごとの filter/dedup/max_sessions が独立して動くこと。
- end-to-end: Team Settings Secret を team session に投影でき、SlackBot 画面で作成した Slack Secret を複数 team bot が再利用でき、API、events、logs に値が出ないこと。

## Decisions

- Secret は top-level resource にせず、Personal / Team Settings の機能とする。
- Secret は用途別 kind を持たない任意の複数 key/value とし、key ごとに任意の env/file projection を持てる。
- Secret の投影は Settings scope の全新規 session に適用する。
- Secret 値は read-back 不可とし、更新は置換のみとする。
- SlackBot 認証には同じ Secrets API と storage を使い、最初の SlackBot 画面から KV Secret を作成し、ほかの bot はその Secret/key mapping を再利用する。
- `SlackCredential` のような別 entity/repository/API は追加しない。
- 共有 Slack Secret ごとに Socket Mode connection を一つだけ持ち、event を参照 bot 全体へ dispatch する。
- 実行中 session は Secret を hot reload せず、SlackBot worker だけ Slack Secret rotation 時に reconnect する。

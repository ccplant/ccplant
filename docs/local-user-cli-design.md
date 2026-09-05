# CLI によるローカルユーザー作成の設計

状態: 実装済み・Fly.io 開発環境で検証予定

## 目的と対象

管理者が `agentapi-proxy client user create` で、GitHub アカウントを持たないユーザーを作成できるようにする。任意の名前を指定でき、作成したユーザーは発行された API トークンで既存 API を利用できる。バックエンドの再起動や複数レプリカへの振り分けでも同じ ID と権限を使用する。

ここでいうユーザーは CCPlant のローカルユーザーであり、OS アカウントではない。初版は通常ユーザーと管理者を対象にする。パスワード認証、GitHub ユーザーの代理作成・統合、チーム所属の編集、ユーザー削除・改名は対象外。認証済み管理者による API 操作とし、最初の管理者には既存の Bootstrap Admin を使用する。

## 現状と不足

- `backend/cmd/client.go` は Cobra ベースの CLI。ユーザー作成コマンドはない。
- `backend/internal/interfaces/controllers/user_controller.go` は `/user/info` の取得を扱う。
- `UserRepository` の実装は `MemoryUserRepository`。登録しても永続化されず、`SimpleAuthService` のユーザーマップとも独立している。
- `SimpleAuthService.validateAPITokenLocked` は個人トークンの所有者 ID からユーザーを都度生成する。永続化された表示名、状態、ロールを読む構造ではない。
- 既存の `/api-tokens` は個人トークンの所有者を呼び出し元に固定するため、管理者が別ユーザーの初期トークンを発行できない。
- 名前付きトークンは Kubernetes Secret に保存され、起動時ロードと定期同期で認証に反映される。他レプリカの失効反映は即時ではない。
- GitHub のユーザー ID はログイン名。自由入力の ID をそのまま採用すると、既存ユーザーの個人リソースへのアクセスにつながる。
- `UserTypeRegular` は定義済みだが `User.Validate()` の許可リストに含まれない。ローカルユーザー用に `UserTypeLocal = "local"` を追加し、生成・復元・検証を統一する。既存型の整理は別変更とする。

## CLI 仕様

以下は新設するコマンドの利用例。

```bash
# 管理者の接続先と認証情報は既存の --endpoint / 環境変数で指定
agentapi-proxy client user create --username alice --display-name 'Alice'
# => {"id":"local:alice","username":"alice","role":"user","status":"active"}

agentapi-proxy client user create --username operator --role admin
agentapi-proxy client user get local:alice

# 初期トークンは別操作。秘密値は指定ファイルだけに書き込む
agentapi-proxy client user token create local:alice \
  --name initial --expires-in 720h --secret-file ./alice-token
agentapi-proxy client user token list local:alice
agentapi-proxy client user token revoke local:alice TOKEN_ID
```

| 引数 | 仕様 |
| --- | --- |
| `--username` | 必須。`[a-z][a-z0-9_-]{0,62}`。大文字や前後空白を黙って変換せずエラーにする |
| `--display-name` | 任意。省略時は username。1〜128 Unicode 文字、制御文字は禁止 |
| `--email` | 任意。メールアドレス構文を検証。ログインや既存アカウントとの紐付けには使わない |
| `--role` | `user`（既定）または `admin`。権限の自由入力は初版では提供しない |
| `--expires-in` | トークン発行用。既定 720h、正の期間かつ最大 8760h。サーバーでも検証 |
| `--secret-file` | トークン発行時に必須。API 呼び出し前に排他的に作成し、0600 で開く。既存ファイル・シンボリックリンクは拒否 |

出力は JSON、診断は標準エラー。ユーザー作成の成功には秘密値を含めない。トークン発行の標準出力には token_id、所有者、期限だけを含める。HTTP エラー、ファイル出力失敗は非ゼロ終了とする。ユーザー作成とトークン発行を分け、トークン発行の失敗時にユーザー作成を繰り返す必要をなくす。

## ID と永続化

ID はサーバーが `local:<username>` として決定する。任意の既存 ID を指定するオプションは設けない。username はローカルユーザー内で一意、display_name と email は重複可能とする。

`LocalUserRepository` を新設し、`Create` と `GetByID` に限定する。初版の永続実装は既存トークン保存方式に合わせて Kubernetes Secret API とし、1 ユーザーを 1 Secret に保存する。Fly.io の API 構成では Kubernetes 互換アダプターを通して libSQL に保存される。Secret 名は `agentapi-local-user-<ID の SHA-256 hex>`。メタデータには id、username、display_name、email、role、status、created_at、created_by を持たせる。秘密トークンはユーザーの Secret に保存しない。

同じ ID の作成は Kubernetes Create の AlreadyExists を 409 に変換し、更新や upsert はしない。事前検索だけで一意性を保証しない。永続バックエンドが使えない場合は 503 とし、メモリへの保存で成功扱いにしない。Kubernetes を利用しない構成への永続実装は後続対応とする。

`local:` は認証プロバイダー間で予約する。導入時には静的認証設定、Bootstrap Admin、既存トークン所有者、個人リソースの所有者 ID に同じ名前空間が使われていないことを検査する。衝突があれば導入を停止し、自動的に新ユーザーへ割り当てない。設定追加・認証経路でも非ローカル identity に `local:` を許可しない。既存の GitHub ID や所有権は変更しない。

## 管理 API

すべての経路で認証と `PermissionAdmin` を要求する。ユースケース内でも同じ認可を行い、呼び出し元の偽装で既存トークン作成ユースケースを流用しない。

| API | 成功時 | 内容 |
| --- | --- | --- |
| `POST /admin/users` | 201 | username、display_name、email、role を受け取り、作成した公開ユーザー情報を返す |
| `GET /admin/users/:id` | 200 | ローカルユーザー情報の取得 |
| `POST /admin/users/:id/api-tokens` | 201 | name、expires_in を受け取り、本人用のトークンを発行。秘密値はこの応答だけに含める |
| `GET /admin/users/:id/api-tokens` | 200 | 対象ユーザーのトークン一覧。秘密値は返さない |
| `DELETE /admin/users/:id/api-tokens/:tokenId` | 204 | 所有者一致を確認して失効。別ユーザーのトークン ID は 404 |

未認証は 401、管理権限なしは 403、不正入力・未知のフィールドは 400、未登録のローカルユーザーは 404、重複は 409、永続層の障害は 503。`POST /admin/users` は任意の id、permissions、provider、team_id を受け付けない。トークン管理 API の対象は登録済みローカルユーザーだけに限定する。

トークン生成と保存には既存の `apitoken` と `APITokenRepository` を使う。`user_id` は作成対象、`created_by` は実際の管理者、scope は `user`。既存の個人トークン API の所有者指定制限を緩めない。

## 認証と権限

ローカルユーザーの権限は role から一意に決定する。`user` は session:create/read/update/delete、`admin` はそれらと admin。各リソースの所有者チェックは引き続き必要で、通常ユーザーが他人のセッションを操作できることを意味しない。

個人トークン所有者が `local:` の場合は、永続リポジトリからユーザーを取得し、有効状態とトークン期限を検証する。存在しないユーザーや読み出し障害で従来のユーザー自動生成へフォールバックしない。ネットワーク I/O を既存の `SimpleAuthService.mu` のロック中に実行せず、必要なトークン情報をコピーした後で読む。

認証後の ID・表示名は永続ユーザーから取得し、実効権限はユーザーの現在の権限とトークン権限の積集合にする。`RoleAdmin` は `HasPermission` で全権限を許可するため、保存済み role を無条件に認証結果へコピーしない。実効権限に admin が含まれる場合だけ管理者ロールを設定し、`IsAdmin()` と `/user/info` の結果を合わせる。

初期トークンには対象ユーザーの権限を付与する。既存 `/api-tokens` での本人による追加発行も実効権限を超えられないよう維持する。既存の GitHub・静的キー・チームトークンは予約 ID の検証以外、従来の認証を維持する。

ユーザー情報は初版ではリクエスト時に永続層から取得する。一方、トークンについては既存の同期方式を使うため、作成直後は別レプリカで一時的に 401 になる場合がある。失効も同期間隔の遅延を持つ。CLI/API はこの性質を説明し、全レプリカでの即時有効化を保証しない。

## 失敗時の扱いと秘密値

- ユーザー作成の応答が失われた場合は `user get local:alice` で結果を確認する。再作成は 409 とし、既存ユーザーを変更しない。
- トークン発行は非冪等。CLI は POST を自動再送しない。通信断時はトークン一覧で確認し、不明な発行分を失効してから新規発行する。秘密値の再取得 API は作らない。
- 保存後に認証キャッシュ登録が失敗した場合、発行済みであることを API 応答の activation_pending に示す。秘密値を含む 201 を返し、定期同期による有効化を待つ。既存ユースケースの「保存後にエラー」をそのまま汎用 500 にしない。
- ファイルへの書き込みに失敗した場合は、発行済み token_id と失効手順を標準エラーへ出し、非ゼロ終了する。秘密値を代わりに標準出力へ表示しない。
- 秘密値を含む API 応答には `Cache-Control: no-store` を付け、アクセスログや監査ログでリクエスト・応答本文を記録しない。保存方式は既存の Secret 内平文方式を踏襲し、ハッシュ保存への移行はこの変更に含めない。
- 監査には実行管理者、対象 ID、操作、結果、時刻、リクエスト ID を残す。メールアドレスや秘密値は不要。

## 実装順序と受け入れ条件

1. LocalUser の DTO・生成・復元・検証、リポジトリと依存注入を追加。名前空間検査と Secret の RBAC を確認する。
2. 管理ユースケースと controller、ルート、API 仕様を追加。トークン生成処理は共通化し、認可と所有者決定を分離する。
3. ローカルユーザーの認証経路を追加し、表示名、状態、権限制限を接続する。
4. `backend/pkg/client` と `backend/cmd/client_user.go` に API クライアントと CLI を追加。Bootstrap Admin の運用ドキュメントから案内する。

実装時に以下を検証する。

- 管理者による作成・取得・発行・失効、一般ユーザーの 403、未認証の 401。
- 同名の同時作成で成功は 1 件だけ。再起動後も ID と属性が保持される。
- ID 衝突や不正入力で既存ユーザー・個人リソースを取得できない。
- 発行トークンで `/user/info` が正しい名前と管理者判定を返し、自分のセッションを作成できる。
- 通常ユーザーのトークンで管理 API、他ユーザーのセッション操作を拒否する。
- 管理者の権限を制限したトークンで RoleAdmin による権限拡大が起きない。
- 存在しない・無効なローカルユーザー、期限切れ・失効トークン、永続層障害を拒否する。
- 別レプリカで同期後に利用・失効が反映され、既存認証の回帰テストが通る。
- 通信断、キャッシュ登録失敗、秘密ファイル書き込み失敗で、結果確認・失効ができ、ログへ秘密値が出ない。

実装の完了条件は、Fly.io 開発環境でこの CLI を使ってローカルユーザーとトークンを発行し、そのトークンでセッション作成 API が成功することである。

## Fly.io 開発環境での検証

2026-09-05 UTC にコミット `7f4ebc8eb85989c99d69e2529e72e3f12a40f4ca` の API イメージを `ccplant-api-dev` へデプロイした。Fly マシン内の CLI で `local:e2e-7f4ebc8` を作成し、対象ユーザー用の有効期限 1 時間のトークンを発行した。そのトークンを `Authorization: Bearer` に指定した `POST /start` は HTTP 200 を返し、セッション ID `25f3e5fc-15ce-476d-aecc-771afbd65b63` が発行された。

検証後、セッションの削除を要求し、発行した API トークンを失効した。トークンの秘密値は Fly マシン上の mode 0600 の一時ファイルだけに書き込み、外部出力には含めていない。ローカルユーザー自体は初版に削除 API がないため、トークンを持たない検証記録として残る。

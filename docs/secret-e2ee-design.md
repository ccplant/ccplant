# Secret E2EE design

## Decision

Settings Secrets を、control plane が復号できる application-level encryption から、作成 client と認可された実行先だけが復号できる envelope encryption に移行する。

ここでの「end」は次の二種類である。

- human end: secret を登録・rotation・共有する browser / CLI
- execution end: secret を実際に使う Session Runtime、Slack worker などの workload

API server、Settings repository、Redis、Kubernetes API / Secret、DB、backup、operator は平文と復号鍵を持たない。workload は利用時に平文を必要とするため、侵害された実行先から secret を守ることは E2EE の対象外とする。

完全にユーザー端末だけを復号端点にすると、Schedule、Webhook、SlackBot、再起動などの無人実行はユーザー不在時に開始できない。このため既定は **workload-assisted E2EE** とする。より強い保護が必要な secret には、後述の `interactive` policy を opt-in で用意する。

## Current state and gap

現状の Settings Secret は write-only UI/API だが、E2EE ではない。

1. browser は `values` を平文 JSON として API に送る。
2. `SettingsController` と `SecretSetting.Values` は平文を扱う。
3. `KubernetesSettingsRepository` が server-side `EncryptionService` で暗号化・復号する。
4. session launch 時に `KubernetesSessionManager.resolveProjectedSecrets` が平文を復元する。
5. 平文を含む `SessionSettings` が Kubernetes Secret に保存され、provisioner が読む。
6. SlackBot resolver も Settings repository から平文を得る。

現在の envelope encryption / KMS は disk、DB、backup の漏えいには有効だが、API process、KMS を利用できる operator、誤ログ、server-side vulnerability には平文を露出する。E2EE 導入後も storage encryption は defense in depth として残す。

## Goals and non-goals

Goals:

- control plane が secret value と scope key の平文を一度も扱わない。
- 通常の登録、session 起動、Schedule / Webhook、再起動では追加の password や承認を求めない。
- Personal / Team、複数端末、member 追加・削除、鍵 rotation、復旧を扱える。
- metadata、projection、参照関係は server が検証・検索できる。
- 既存の write-only contract を維持し、段階的に移行できる。

Non-goals:

- 実行中 workload、browser extension、端末 malware からの保護。
- secret を受け取る外部 API からの保護。
- secret の長さ、利用時刻、scope、projection など metadata の完全な秘匿。
- server が配信する frontend JavaScript 自体を server compromise から守ること。これが必要なら signed desktop/CLI client または別 origin の固定 E2EE client を使う。

## Threat model

| Threat | Protected | Notes |
| --- | --- | --- |
| DB / Kubernetes / backup dump | yes | ciphertext と wrapped key のみ |
| API / repository の read-only compromise | yes | private key を持たない |
| API process の runtime compromise | yes, for stored/forwarded values | 攻撃後に改変 frontend を配信できる場合は別問題 |
| Redis / internal message bus compromise | yes | ciphertext bundle のみ |
| cluster operator | control plane では yes | execution node / workload を操作できる operator は対象外 |
| unauthorized team member | yes | ACL と cryptographic recipient の両方で防ぐ |
| revoked member | future values yes | 過去に復号済み・持ち出した値は revoke 不可能 |
| malicious authorized user | no | 自身が利用可能な secret は workload 経由で利用・流出可能 |

## Cryptographic model

### Keys

```text
DeviceKey (DK)       deviceごとの X25519 key pair。private key は端末外へ出さない
ScopeKey (SK)        Personal / Team scope ごとのランダム 256-bit key
SecretKey (SEK)      secret version ごとのランダム 256-bit key
ExecutorKey (EK)     execution trust domain ごとの X25519 key pair
```

- payload: browser / Go の標準 API で実装しやすい AES-256-GCM を v1 とする。96-bit nonce は CSPRNG で毎回生成し、同じ鍵で再利用しない。
- asymmetric key wrapping: HPKE base mode (`DHKEM(X25519, HKDF-SHA256)`, `HKDF-SHA256`, `ChaCha20Poly1305`)。browser は十分に監査された HPKE library を使用し、独自実装しない。
- `SEK` は `SK` を wrapping key とする AES-KW、各 `EK` public key には HPKE で wrap する。`SK` は各 device public key に HPKE で wrap する。
- AAD に protocol version、scope ID、secret ID、secret version、value key、projection digest を canonical CBOR で入れ、ciphertext の scope 差替えを防ぐ。
- cryptographic format は algorithm agility のため version と suite を必須にし、独自暗号を作らない。

`SK` は human による編集・再共有用、`EK` wrapper は無人実行用である。control plane はどちらの private key も持たない。

### Stored envelope

値は key 単位ではなく secret version 全体を canonical CBOR map として暗号化する。key 名と projection は metadata として公開し、server-side validation を可能にする。

```json
{
  "format": "agentapi-secret/v1",
  "secret_id": "sec_...",
  "scope_id": "team:org/team",
  "version": 7,
  "suite": "HPKE-X25519-HKDF-SHA256-CHACHA20POLY1305+AES256GCM",
  "nonce": "base64url...",
  "ciphertext": "base64url...",
  "aad_digest": "base64url...",
  "recipients": [
    {"type": "scope", "kid": "sk_...", "wrapped_key": "..."},
    {"type": "executor", "kid": "ek_k8s_prod_2026_01", "wrapped_key": "..."}
  ]
}
```

storage encryption はこの envelope 全体をさらに暗号化してよいが、E2EE の保証には数えない。

## Key custody without daily friction

### First device

初回の Secret 画面で browser が WebCrypto により `DeviceKey` を生成する。private key は non-extractable とし IndexedDB に保存する。通常の閲覧・作成では user prompt は発生しない。

鍵紛失を避けるため、初回設定完了前に次のいずれかを一度だけ要求する。

1. 別 device を追加する（推奨）。
2. recovery key（128 bit 以上のランダム値）を download / password manager に保存する。
3. enterprise policy が許せば passkey PRF から recovery wrapping key を導出する。

passkey PRF は対応状況に差があるため唯一の経路にしない。login passkey の署名鍵を暗号鍵として流用しない。localStorage、cookie、server には private key / recovery key を保存しない。

CLI は OS keychain を利用する。headless CLI は encrypted key file と環境変数ではない対話入力を fallback にする。

### Returning user and a new device

- 登録済み device: login 後に自動 unlock。追加操作なし。
- 新 device: 既存 device に QR / short code で approval request を送り、新 device public key に `SK` を rewrapする。
- 既存 device がない: recovery key で復旧する。
- どちらもない: ciphertext は復旧不能。server-side reset は既存 secret を削除して再登録するだけで、復号はしない。

### Team scope

Team は一つの `SK` と member-device recipient set を持つ。

- member 追加: team admin の端末が新 member device に `SK` を wrapする。操作は membership 画面で一度だけ。
- member 削除: 新 `SK` を生成し、残存 device に再配布する。全 secret の payload 再暗号化は不要で、`SEK` の scope wrapper だけを lazy rewrapする。
- 削除 member が過去に取得した `SK` / value は回収できない。高リスク変更時は secret value 自体の rotation を UI で促す。
- admin 不在で無人運用を止めないよう、最低二人の key admin または recovery policy を team 作成時に推奨する。

## Executor trust domains

`ExecutorKey` は「その secret を平文で利用してよい実行環境」を表す。全 cluster 共通鍵にはしない。

例:

- Kubernetes namespace / environment 単位の Session Runtime
- External Session Manager 単位
- Slack Socket worker deployment 単位

private key は executor 側の TPM / KMS / HSM で non-exportable に保持する。KMS decrypt API を使う場合、API server の service account には decrypt 権限を与えず、attested workload identity だけに付与する。公開鍵と `kid` は登録時に署名付きで control plane へ登録する。

重要: KMS が plaintext data key を API server に返す現在の構成は E2EE ではない。decrypt caller を execution workload に限定する。

複数 executor への無制限な wrap は blast radius を広げる。Secret / Profile ごとに allowed executor trust domains を metadata として保持し、既定は現在の scope の default executor のみとする。新しい ESM への初回利用時だけ「この実行先に許可」確認を表示する。

## Data flows

### Create / rotate

```text
Browser/CLI                         Control plane                    Storage
    | generate SEK                       |                              |
    | encrypt values with SEK            |                              |
    | wrap SEK to SK + allowed EKs       |                              |
    |--- metadata + envelope ----------->| validate IDs/AAD/version     |
    |                                    |--- ciphertext only --------->|
    |<--- metadata, version --------------|                              |
```

API は plaintext `values` を受け付けず `envelope` と `keys` を受け取る。Content-Type は `application/vnd.agentapi.secret-envelope+json` とし、body logging / tracing を明示的に禁止する。

### Session launch

```text
Control plane                 Session Runtime / provisioner            Agent
    | create session + nonce            |                                |
    |--- ciphertext envelope ---------->| verify session/scope/AAD        |
    |                                   | unwrap SEK with ExecutorKey     |
    |                                   | decrypt in memory               |
    |                                   | write env/file immediately      |
    |                                   | zero buffers / remove bundle    |
    |                                   |------------------------------->|
```

- `resolveProjectedSecrets` は平文を返さず `EncryptedSecretBundle` を選択するだけにする。
- session settings Kubernetes Secret には ciphertext bundle のみを入れる。
- provisioner が PID 1 起動直前に decrypt し、env/file projection を適用する。
- restart は同じ ciphertext snapshot を再利用でき、control plane の復号は不要。
- ESM へはその ESM の `EK` recipient がある envelope だけ配送する。
- plaintext を status、events、command ack、crash dump に含めない。buffer は可能な範囲で `clear` / zeroizeする。

### SlackBot and other server consumers

現在の Slack worker が control plane process 内で動くなら、そのままでは E2EE にできない。worker を独立 workload に分離し、Slack executor key と最小権限 identity を与える。control plane は bot config と ciphertext を渡し、worker だけが token を復号する。

同じ原則を model provider key、GitHub credential、MCP header へ広げる。server-side consumer を control plane に残したまま「E2EE」と表示しない。

### Interactive policy

特に強い secret には `release_policy: interactive` を選べる。この場合 `SEK` を executor に事前 wrapせず、session start ごとに browser/CLI が session の ephemeral public key と attestation を確認して rewrapする。ユーザーが不在なら session は `waiting_for_secret_release` で待機し、TTL 後に失敗する。

既定の `automatic` は無人実行可能、`interactive` は control plane と事前登録 executor key の同時侵害にも強い、という trade-off を UI に明記する。

## API and model changes

### Models

```go
type SecretSetting struct {
    ID            string
    Name          string
    Keys          []string
    Projections   []SecretProjection
    Envelope      SecretEnvelope // ciphertext only
    ReleasePolicy string         // automatic | interactive
    Version       int64
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

domain entity から `Values map[string]string` を削除する。plaintext を表現できる共通 Settings type を持たないことを重要な安全境界にする。

別 resource として次を追加する。

- `DeviceKey {id, user_id, public_key, created_at, last_used_at, revoked_at}`
- `ScopeKeyEnvelope {scope_id, key_version, device_id, wrapped_scope_key}`
- `ExecutorKey {id, trust_domain, public_key, attestation, status}`
- `SecretRecipient {secret_id, secret_version, recipient_kid, wrapped_secret_key}`

### Endpoints

```text
POST   /users/me/e2ee/devices
GET    /users/me/e2ee/devices
DELETE /users/me/e2ee/devices/:id
GET    /settings/:name/e2ee/scope-key-envelope
POST   /settings/:name/e2ee/device-grants
GET    /settings/:name/secrets/:id/envelope
POST   /settings/:name/secrets                  ciphertext create
PUT    /settings/:name/secrets/:id/envelope     ciphertext rotate
POST   /settings/:name/secrets/:id/recipients   rewrap only
```

Server は以下を検証する。

- authenticated principal と scope ACL
- secret ID、scope ID、base version、recipient key が登録済み・active
- metadata から再計算した AAD digest
- envelope / ciphertext size、recipient count、suite allowlist
- automatic secret に対象 executor recipient があること

Server は plaintext の形式・Slack token prefix などを検証できない。これは encrypt 前に client、decrypt 後に consumer が検証し、consumer error は値を含めず返す。

## UX

通常フローは現状とほぼ同じにする。

- 初回だけ「この端末で暗号化を有効化」し、device key を自動生成する。
- Secret 作成フォームは同じ。保存 button で client-side encrypt する。
- 一覧は name、keys、projection、configured、共有先、鍵状態だけを表示する。
- 登録済み端末では session 起動に追加 prompt はない。
- 新しい execution domain のみ consent dialog を出し、domain 名・運用者・environment を表示する。
- 鍵がない端末では値の置換は可能（新値を新 envelope として作る）が、既存値の key 追加や再共有はできない。
- 「E2EE」badge は全 recipient が E2EE 対応の secret にだけ表示する。migration 中は `server-encrypted` と区別する。

ユーザーに recovery phrase を毎回入力させたり、秘密値の利用ごとに passkey prompt を出したりしない。端末紛失時の挙動は設定画面で事前に明示する。

## Metadata integrity and malicious control plane

暗号化だけでは malicious server が古い envelope を戻す rollback や recipient を差し替える攻撃を完全には防げない。

- client は scope ごとの monotonic `key_version` と secret `version` を保存し、低い version を警告・拒否する。
- metadata + ciphertext + recipient set を creator device key で署名する。
- executor は署名、AAD、session scope、allowed trust domain を検証する。
- append-only transparency log に envelope digest を記録し、client が consistency proof を確認する方式を phase 3 で追加する。

初期 phase では rollback detection は「同じ端末で観測済みの version まで」と明記し、完全な malicious-server resistance を宣伝しない。

## Migration

### Phase 0: boundaries and observability

- request/response/body tracing の secret endpoint 除外を test で固定する。
- plaintext secret type の利用箇所を inventory 化する。
- capability `secret_encryption: server | e2ee-v1` を API に追加する。

### Phase 1: dual read, E2EE write

- device / scope / executor key registry と client crypto library を追加する。
- repository と session settings が envelope を保持できるようにする。
- provisioner と独立 Slack worker に decrypt を実装する。
- 新規 secret は E2EE のみ。既存 server-encrypted secret は引き続き read する。

### Phase 2: assisted migration

既存値は server がすでに復号可能なので、黙って「E2EE 化済み」とはしない。ユーザーが migration を開始した時だけ、一時 migration worker が旧値を読み、対象 executor public key と user scope public keyへ encryptする。監査 event を残し、成功後に旧 ciphertext を削除する。

より強い保証が必要な利用者には UI で値の再入力・rotation を推奨する。rotation 後だけ `origin: client-encrypted` と表示する。

### Phase 3: remove plaintext path

- `SecretSetting.Values`、server decrypt、平文 `SessionSettings` projection を削除する。
- plaintext create/update media type を `410 Gone` にする。
- storage scan で legacy record が 0 であることを確認して旧 KEK access を外す。
- transparency / rollback protection を有効化する。

rollback は envelope を理解しない旧 binary への downgrade を禁止する。migration 前に backup restore drill を行う。

## Failure handling

| Condition | Behavior |
| --- | --- |
| device key unavailable | read metadata only; recovery / device approval / replace value を案内 |
| executor recipient missing | launch 前に `secret_executor_not_authorized`; ciphertext は配送しない |
| executor key rotated | public key 登録後、authorized device が background rewrap。旧鍵は grace period 後 revoke |
| corrupted envelope / AAD mismatch | fail closed; generic error と audit event。平文 fallback 禁止 |
| team member revoked | scope key rotate job を開始。完了まで新規共有を止める |
| recovery material lost |既存値は復旧不能。metadata を残して値の再登録を許可 |
| crypto API unsupported | secret write を無効化し対応 browser / CLI を案内。server plaintext fallback 禁止 |

## Security requirements

- CSP、Trusted Types、dependency pinning、SRI 相当を E2EE UI に適用し、XSS を最優先で防ぐ。
- secret input は analytics、session replay、browser persistence、React error payload から除外する。
- executor private key は control plane container / service account から参照不能にする。
- envelope は session ID と generation に bind し、別 session への replay を拒否する。
- decrypt failure で legacy plaintext へ fallback しない。
- secret value を argv に置かない。可能なら file descriptor / stdin、env/file は必要な consumer のみに限定する。
- audit log は secret ID、version、recipient KID、actor、結果だけを記録する。

## Verification and acceptance criteria

Automated tests:

- API / repository / Redis / Kubernetes fake client を横断し、canary plaintext byte sequence が server-side persistence と logs に一度も現れない。
- control plane の全 configured KMS/API credential を与えても E2EE fixture を復号できない。
- wrong scope、version、projection digest、executor、session generation では復号に失敗する。
- nonce uniqueness、recipient revoke、key rotation、concurrent version conflict を property / fuzz test する。
- browser crypto と Go/Rust provisioner の test vector interoperability を固定する。
- Schedule、Webhook、resume、ESM、SlackBot が user offline で成功する。
- interactive secret は approval なしに開始しない。

Operational acceptance:

- packet capture、DB dump、Kubernetes Secret dump、Redis dump に平文がない。
- API server memory dump から保存済み secret plaintext が得られない。
- executor compromise の blast radius がその trust domain に限定される。
- device loss / member removal / executor rotation の runbook と復旧訓練が完了している。

## Recommended first implementation slice

最初から全 consumer を同時移行せず、Personal Secret の session env/file projection に限定する。

1. DeviceKey、Personal ScopeKey、Kubernetes Session Runtime の ExecutorKey を実装する。
2. frontend create/rotate と provisioner decrypt の test vector を作る。
3. `SecretSetting.Values` と別経路で `EncryptedSecretBundle` を session に渡す。
4. E2EE secret を opt-in beta とし、Schedule / restart を含めて検証する。
5. Team sharing、ESM、SlackBot の順に executor trust domain を追加する。
6. 全 consumer 対応後に E2EE を default、新規 plaintext write を停止する。

この順序なら通常ユーザーの操作は初回 device enrollment だけで、無人実行を維持しながら control plane から平文と復号能力を除去できる。

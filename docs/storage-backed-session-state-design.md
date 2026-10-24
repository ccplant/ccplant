# Storage-backed session state design

## 1. Goal

Session Profile で既存の scope-owned Storage を選び、セッションを削除・再作成しても
agent のローカル状態を引き継げるようにする。1 個の Storage の異なる領域を複数の
container path へ mount できることを、汎用の Storage attachment として先に実現する。

この機能は同じ論理 session を復元する `resume_from` / session snapshot の置き換えではない。
Storage は Profile を使う複数 session 間で agent home の状態を継承するために使い、
snapshot は特定 session の ACP conversation と workspace を正確に復元するために使う。

## 2. Current constraints

現在の `StorageAttachment` は `storage_id`, `mount_path`, `read_only` を持つが、次の制約がある。

- API validation とフロントの editor が同じ `storage_id` の複数指定を拒否する。
- Profile と start request の merge key も `storage_id` である。
- Kubernetes Session Manager は attachment ごとに EmptyDir と sync worker を作る。
- object snapshot の file path は常に Storage root 相対であり、Storage 内の subtree を選べない。
- `~/.codex` と `~/.claude` は reserved path である。
- Storage restore は profile settings と credential を生成した後に走るため、agent home を
  mount すると古い設定や credential が生成済みファイルを上書きし得る。

単に `storage_id` の重複チェックだけを外すと、同じ remote manifest に対して複数の
sync worker が別々の local tree を走査し、片方にしかない file をもう片方が削除として
commit する。この変更は不可とする。

## 3. Storage mount model

### 3.1 API model

`StorageAttachment` に Storage 内の mount 元を表す `storage_path` を追加する。

```json
{
  "storage_id": "storage-1",
  "storage_path": "session-state/codex",
  "mount_path": "/home/agentapi/.codex",
  "read_only": false
}
```

- `storage_path` は normalized relative path とする。先頭 `/`, `.`, `..`, 空 segment は拒否する。
- 省略または空文字は Storage root を意味し、既存 attachment と互換にする。
- `mount_path` は従来どおり container 内の絶対 path とする。
- 同じ `storage_id` を複数回指定できる。
- target の `mount_path` は session 内で一意かつ相互に非 overlap とする。
- 同じ Storage の source range は、完全一致か非 overlap のどちらかだけを許可する。
  親子関係にある `storage_path` は二重 sync を避けるため拒否する。

完全一致する `(storage_id, storage_path)` を複数の `mount_path` に指定した場合は alias とする。
alias 群は 1 個の local volume と 1 個の sync worker を共有し、同じ内容が全 target から見える。
同じ source group 内で `read_only` が混在することは許可するが、1 個でも writable mount があれば
sync worker は write-back を行う。全 mount が read-only の場合だけ pull-only とする。

Profile と明示的な start request の merge key は `storage_id` から normalized `mount_path` へ変更する。
これにより同じ Storage の複数 mount を保持したまま、request は特定 target の attachment だけを
置換できる。旧 client が送る `storage_path` なしの payload も同じ規則で扱う。

### 3.2 Runtime representation

認可・scope・Pool・manifest の解決は Storage ごとに一度だけ行い、data plane には次の group を渡す。

```go
type ResolvedStorageMountGroup struct {
    StorageID  string
    StoragePath string
    Kind       StorageKind
    Revision   int64
    Files      []StorageFile // StoragePath を除いた group 相対 path
    Mounts     []StorageMountTarget
}

type StorageMountTarget struct {
    MountPath string
    ReadOnly  bool
}
```

保存済み SessionSettings は当面 flat attachment も読めるようにし、decode 後に group 化する。
新規 settings は group 形式で保存する。これにより Pod restart でも volume と mount の対応が安定する。

Kubernetes では group ごとに EmptyDir を 1 個作り、その volume を target 数だけ mount する。
provisioner は group ごとに一度 restore / scan / pull / write-back する。Storage API へ送る file path は
`storage_path + local relative path` とし、manifest response は prefix で絞り込んで group 相対 path に戻す。
API に `prefix` query を追加して manifest 全件転送を避ける。

native runtime では 1 個の staging directory を作り、各 target に bind mount する。bind mount を
利用できない実行環境は multi-target attachment capability を advertise せず、親 proxy が start を
`400 storage_mount_capability_unsupported` で拒否する。

## 4. Session Profile setting

Storage の低レベル attachment editor とは別に、Profile に intent を表す設定を追加する。

```json
{
  "config": {
    "session_state": {
      "enabled": true,
      "storage_id": "storage-1"
    }
  }
}
```

`enabled=false` でも `storage_id` は保存し、再度有効にしたときに選択を維持する。
有効時は profile と同じ scope の `ready` な Storage が必須である。Pool 制約も通常 attachment と
同じタイミングで検証する。

Profile 解決後、選択された agent type に応じて server が次の managed attachment を生成する。

| agent type | storage path | container path | 保存対象 |
| --- | --- | --- | --- |
| `codex-acp` | `session-state/codex` | `/home/agentapi/.codex` | rollout、session index、state DB |
| `claude-acp` | `session-state/claude` | `/home/agentapi/.claude` | project conversation state |

Profile が agent type を固定していない場合は両方を生成する。同じ Storage を異なる source/target の
組として使うため、multi-path mount が必要になる。将来 agent を追加するときは server-side mapping を
増やし、Profile schema を変更しない。

managed attachment と `config.params.storages` の target が衝突した場合は validation error とし、暗黙の
上書きはしない。`session_state` は Profile inheritance の通常の scalar として、子で指定した object が
親を置換する。

## 5. Safe persistence boundary

agent home 全体をそのまま同期してはいけない。認証情報と現在の Profile から生成される設定を
別 session に持ち越すためである。managed session-state group には server-defined include/exclude policyを
付け、client から変更できないようにする。

初期 policy は次のとおりとする。

- Codex include: `sessions/**`, `session_index.jsonl`, `state_*.sqlite*`
- Claude include: `projects/**`
- 常時 exclude: `auth.json`, `.credentials.json`, model connection identity、hooks、settings、skills、plugin、cache
- symlink、socket、device、64 MiB を超える file は従来どおり同期しない

restore 順序を次に変更する。

1. Storage volume を空の agent home target に mount する。
2. managed session-state を restore する。
3. `SetupSettings` / `CompileSettings` で現在の Profile の設定を生成する。
4. managed credential を書き込み、connection identity を検証する。
5. agent を起動して sync worker を開始する。

これにより永続状態より現在の Profile・credential が常に優先される。generic attachment の restore
順序は変更しない。

Codex の SQLite DB と WAL は個別 file の last-write-wins では整合性を保証できない。そのため managed
session state を writable attach する session は Storage 単位の exclusive lease を取得する。既に別の
active session が lease を持つ場合、start は `409 session_state_storage_in_use` を返す。lease は canonical
session resource に記録し、正常終了、削除、期限切れ reconciler で解放する。generic Storage attachment の
既存 multi-writer semantics は変更しない。

## 6. Frontend

Session Profile の Storage section の先頭に「エージェントの状態を保持する」toggle と Storage selector を
追加する。selector は Profile と同じ user/team scope の Storage のみを表示する。

- toggle OFF: selector は disabled だが値を保持する。
- toggle ON で未選択: save を無効化し、Storage 作成画面への link を表示する。
- agent type 固定時は実際に保持する対象を表示する。
- agent type 未固定時は「Codex と Claude の状態を同じ Storage 内の別領域に保存」と表示する。
- team scope では同時に 1 session だけ利用できることと、team member が履歴へアクセスできることを
  warning として表示する。

同じ画面の詳細設定として既存 `StorageAttachmentEditor` は残す。ここでは同じ Storage を再選択可能にし、
各 row に任意の `storage_path` field を追加する。追加 button は「未使用 Storage があるか」ではなく、
Storage が 1 件以上あるかだけで enable にする。client validation は ID 重複ではなく source range と
target range を検証する。

## 7. Authorization and information exposure

- Storage と Profile の scope は一致必須とし、既存の ownership check を再利用する。
- team Storage を選ぶと会話状態も team resource になる。UI と API description に明示する。
- `GET /session-profiles` は Storage metadata を展開せず ID のみ返す。
- session settings には object-store credential を含めない。
- managed include policy は request payload から指定不可とし、任意 path を agent home から吸い上げる
  API にしない。

## 8. API and compatibility changes

- `StorageAttachment` schema に optional `storage_path` を追加する。
- `SessionProfileConfig` schema に optional `session_state` を追加する。
- storage manifest list に optional `prefix` を追加する。
- session start の 400/409 error code を OpenAPI に追加する。
- External Session Manager capability に `storage_multi_mount` と `managed_session_state_v1` を追加する。
- capability のない manager では `session_state.enabled=true` を silently ignore しない。
- 既存 Profile、start request、保存済み SessionSettings は `storage_path=""` として読み込める。

agentapi 自体の API 変更はない。この機能は親 proxy の start/Profile/storage contract と runtime volume
構成だけで完結する。

## 9. Rollout plan

1. `storage_path` の schema、validation、grouping、settings backward compatibility を導入する。
2. Kubernetes と native runtime に multi-target mount を実装し、capability を公開する。
3. prefix-aware restore/sync と、同一 source group に sync worker が 1 個だけ作られることを検証する。
4. managed include policy、restore ordering、exclusive lease を実装する。
5. Profile API に `session_state` を追加する。
6. フロントの toggle/selector と汎用 attachment editor を更新する。
7. feature flag `storage_backed_session_state` で有効化し、既存 Storage workload に影響がないことを
   確認後に default-on とする。

## 10. Verification

- 同じ Storage の別 subtree を Codex/Claude path に同時 mount できる。
- 同じ `(storage_id, storage_path)` の alias は同じ volume を参照し、sync worker は 1 個だけである。
- source/target の親子 overlap、reserved path の通常 attachment、scope/Pool 不一致を拒否する。
- Profile/start merge が target path ごとに置換され、同じ Storage の他 mount を失わない。
- legacy attachment と保存済み flat SessionSettings を復元できる。
- credential、hooks、settings、skills が object storage に upload されない。
- restore 後に現在の Profile 設定と credential が優先される。
- writable managed state の二重起動は 409 になり、異常終了後に lease が回収される。
- read-only alias と generic multi-writer Storage の既存挙動を壊さない。
- Pod restart は同じ resolved mount group を再利用する。
- capability のない External Session Manager で明示的な error を返す。

## 11. Non-goals

- Storage を強整合な共有 filesystem にすること。
- 複数 session から同じ Codex SQLite state を同時更新すること。
- `resume_from`、automatic suspend、session snapshot を Storage に統合すること。
- agent home 全体、credential、Profile 生成物を永続化すること。

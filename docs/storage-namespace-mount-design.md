# Tag-scoped Storage namespace mount design

## 1. Goal

1 個の Storage を物理的には共有しつつ、セッションごとに解決した独立 namespace を mount できるようにする。
代表例は、同じ Storage を Session Profile に設定し、`repository=owner/repository` tag ごとに別の内容を
`/home/agentapi/workdir` へ復元・保存することである。

既存の resource ownership を表す `scope=user|team` とは別の概念なので、API と実装ではこの機能を
**Storage namespace** と呼ぶ。namespace は認可境界ではなく、認可済み Storage 内のデータ分離境界である。

## 2. Recommended API

既存の `StorageAttachment` に optional な `namespace` を追加する。

```json
{
  "storage_id": "storage-1",
  "storage_path": "repositories",
  "namespace": {
    "mode": "tags",
    "tag_keys": ["repository"]
  },
  "mount_path": "/home/agentapi/workdir",
  "read_only": false
}
```

- `storage_path` は従来どおり固定の base path。省略時は Storage root とする。
- `namespace.mode` の初期値は `tags` のみとする。
- `tag_keys` は順序を意味に含めず、server で辞書順に正規化する。重複と空配列は拒否する。
- 指定 key が実効 session tags に存在しない、空文字である、または上限を超える場合は起動を
  `400 storage_namespace_tag_missing` または `400 storage_namespace_invalid` で拒否する。
- `namespace` を省略した attachment は現在と同じ固定 `storage_path` を使うため後方互換である。
- `namespace` と client が確定値を渡すための field は分離する。client から `resolved_storage_path` は
  受け付けない。

Session Profile では上記設定をそのまま保持できる。リポジトリ単位の UI には、詳細な tag key editor より
先に「データの分け方: 共有 / リポジトリごと」を提供し、「リポジトリごと」を
`tag_keys=["repository"]` に変換する。詳細設定では複数 key を指定可能にする。

## 3. Namespace resolution

Profile、start request、trigger が持つ tags をすべて現在の優先順位で merge した**後**、Storage の ownership、
team、Pool、state を検証し、その後に namespace を解決する。Profile 選択前の tags や Profile の base tags
だけで解決してはいけない。

直接 `repository` の値を path として連結しない。path traversal、表記揺れ、slug 化による衝突を避けるため、
次の canonical form を用いる。

1. tag key を byte-wise の昇順に並べる。
2. key と value を UTF-8 の length-prefixed encoding で直列化する。
3. `SHA-256(canonical form)` を計算し、lower-case base32 で表す。
4. resolved path を
   `<storage_path>/.ccplant/namespaces/v1/<first-2-chars>/<digest>` とする。

例として `repository=acme/api` の表示名は UI 上では `repository=acme/api` のままだが、実データ path は
hash にする。表示名や元 tag は manifest 内の予約 metadata file に保存せず、Storage metadata index に
次の情報として保存する。

```json
{
  "storage_id": "storage-1",
  "namespace_id": "<digest>",
  "tag_keys": ["repository"],
  "tag_fingerprint": "<digest>",
  "display": "repository=acme/api",
  "created_at": "...",
  "last_used_at": "..."
}
```

生の tag 値を metadata に保存することは検索性と情報露出の trade-off がある。初期実装では
`repository` のように UI 表示を許可した tag key のみ `display` に保存し、それ以外は fingerprint のみにする。
tag key/value はそれぞれ 128/512 bytes、key 数は 8、canonical form は 4 KiB を上限とする。

## 4. Runtime contract

control plane で解決後、既存の data plane contract へ確定済み path を渡す。

```go
type StorageNamespaceSelector struct {
    Mode    string   `json:"mode"`
    TagKeys []string `json:"tag_keys"`
}

type StorageAttachment struct {
    StorageID   string                    `json:"storage_id"`
    StoragePath string                    `json:"storage_path,omitempty"`
    Namespace   *StorageNamespaceSelector `json:"namespace,omitempty"`
    MountPath   string                    `json:"mount_path"`
    ReadOnly    bool                      `json:"read_only,omitempty"`
}

type ResolvedStorageAttachment struct {
    StorageAttachment
    ResolvedStoragePath string `json:"resolved_storage_path"`
    NamespaceID        string `json:"namespace_id,omitempty"`
}
```

External Session Manager と provisioner に渡す SessionSettings では `Namespace` を除き、既存の
`storage_path` に `ResolvedStoragePath` を格納して pin する。これにより Pod restart、suspend/resume、retry で
tags が変化しても同じ namespace を使う。API の session metadata には selector と `namespace_id` を残すが、
内部 path は通常の response には出さない。

既存 restore/sync は確定済み `storage_path` prefix を扱えるため、data plane の同期アルゴリズムは変更しない。
source overlap validation と Profile/start attachment merge は namespace 解決後の path と正規化済み
`mount_path` に対して行う。同じ Storage、同じ namespace、同じ mount target は override、異なる namespace は
独立 attachment として扱う。

## 5. Isolation and concurrency

- Storage の read/write 認可を先に行う。tag を知っていても別 scope の Storage にはアクセスできない。
- tag 値は path に使わない。`/`, `..`, Unicode、非常に長い値を含んでも escape や衝突を起こさない。
- writable mount は `(storage_id, namespace_id)` 単位で lease を取得する。同じ namespace の 2 writer は
  `409 storage_namespace_in_use`、別 namespace の writer は同時実行可能とする。
- read-only mount は writer と共存可能だが、開始時 revision の snapshot として扱い、実行中の自動 refresh はしない。
- 同じ namespace に複数 target を alias mount する場合は 1 volume / 1 sync worker を共有する。
- namespace delete は実行中 lease がなく、明示 API で指定された場合だけ行う。Storage resource の削除以外で
  自動的に namespace を消さない。

Object Storage が条件付き write を提供する場合は lease と manifest revision を CAS で更新する。提供しない
backend では control plane の lease repository を使い、sync 完了時にも期待 revision を検証する。

## 6. API and UI additions

追加 API:

- `GET /storages/:id/namespaces`: namespace ID、許可済み display、revision、size、last used、active lease を一覧する。
- `DELETE /storages/:id/namespaces/:namespace_id`: lease がない namespace を削除する。
- file API は既存どおり内部 path を扱う管理用途に限定し、通常 UI は namespace API を使う。

Storage attachment editor:

- 「分け方」を `Storage 全体を共有` / `リポジトリごと` / `タグを指定` から選択する。
- `リポジトリごと` では、repository tag がない起動は失敗する旨を表示する。
- `タグを指定` では tag key のみ入力させ、値は session 起動時に決まることを表示する。
- Storage 詳細画面で namespace ごとの容量、最終利用、使用中 session を表示し、個別削除できるようにする。

## 7. Compatibility and rollout

1. schema、canonical encoder、resolver、validation を追加する。feature flag
   `storage_tag_namespaces` は default-off とする。
2. resolved SessionSettings の pin と restart test を追加する。
3. namespace lease と revision conflict を実装する。
4. Profile editor の「リポジトリごと」と namespace 管理 UI を追加する。
5. External Session Manager capability `storage_tag_namespace_v1` を追加する。未対応 manager へは明示的に
   `400 storage_namespace_capability_unsupported` を返す。
6. metrics と orphan cleanup を確認後 default-on にする。

既存 attachment は migration 不要である。既存の固定 `storage_path=foo` を repository namespace 化すると、
保存先は新規 path になり、旧データは自動移動しない。UI で「新しい空 namespace から開始」と明示し、必要なら
管理者向け copy API を別途提供する。

## 8. Verification

- 同じ Storage と mount path を使っても、異なる `repository` tag の内容が相互に見えない。
- 同じ tag set は key の入力順によらず同じ namespace ID になる。
- key 欠落、空値、上限超過、未知 mode、重複 key を起動前に拒否する。
- traversal 文字、Unicode、slug が同じになる値でも namespace が衝突しない。
- Profile/start/trigger tags の merge 後の値で解決される。
- restart、retry、suspend/resume は pin 済み namespace を再利用する。
- 同じ namespace の 2 writer を拒否し、別 namespace の writer は並行実行できる。
- legacy attachment の path、同期、Profile/start merge behavior が変わらない。
- ownership scope、team、Pool、Storage state の既存検証を namespace が迂回しない。
- capability のない manager で設定を silently ignore しない。

## 9. Non-goals

- namespace を user/team の認可境界として使うこと。
- tag value を任意の raw storage path として指定すること。
- 複数 writer の変更を file 単位で merge すること。
- tag 変更時に旧 namespace のデータを自動移動または削除すること。
- namespace ごとに別の物理 Storage resource を作成すること。

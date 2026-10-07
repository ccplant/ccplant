# セッションのワークスペース化設計

## 1. 結論

特定のセッションの状態を別セッションへ引き継ぐ機能は、「元を残したまま fork」ではなく
**セッションを再利用可能なワークスペースへ変換する操作**として提供する。

テンプレート化が完了した元セッションは実行可能な session ではなくなる。

- workload を停止・削除する
- prompt、resume、restart、設定変更を禁止する
- 通常の active session 一覧から除外し、template 一覧へ移す
- 会話状態と workspace の snapshot を固定する
- template から何度でも別 ID の新規 session を作成できる

利用者が tarball のような「サスペンド用ファイル」を直接扱う方式にはしない。proxy が snapshot と
manifest を管理し、template ID で参照する。利用者向けに manifest を JSON export してもよいが、秘密を
含み得る snapshot 本体は download/upload させない。

新規セッション作成 API にすでに存在する `params.resume_from` は execution-plane の内部入力として残す。
公開 API では推測可能な session ID を直接受け付けず、認可済みの `workspace_id` を受け付ける。
旧 `/session-context-templates`、`context_template_id`、`templateize` は既存クライアント向けの互換 API として残す。

## 2. ライフサイクル

| 種別 | 実行可能 | workload | 状態変更 | 用途 |
| --- | --- | --- | --- | --- |
| active session | 可 | あり | 会話・workspace が変化 | 通常作業 |
| suspended session | resume 後に可 | なし | resume で同じ session を継続 | 一時停止 |
| context template | 不可 | なし | immutable | 新規 session の雛形 |

テンプレート化は suspend とは異なる不可逆のドメイン遷移とする。

```text
active/stable session --templateize--> preparing --> context template
                              |              |
                              +----failed----+--> original session remains usable

context template --instantiate--> new active session
                 --instantiate--> another new active session
```

template を session に「戻す」APIは提供しない。続きを作業したい場合も template から新しい session を
作成する。これにより template の再現性を保つ。

## 3. 現状の再利用箇所

現在のコードには以下がすでにある。

- `SessionParams.ResumeFrom` / `RunServerRequest.ResumeFrom`
- `AGENTAPI_RESUME_FROM` と `SessionSettings.Session.ResumeFrom`
- provisioner の `restoreSessionState(sourceID, cwd)`
- Claude ACP / Codex ACP の checkpoint と volume / S3 session state store
- suspend 前の checkpoint、および restart 時の同一 session ID からの復元

不足しているのは、session から template への所有権移動、immutable な snapshot key、変換中の排他、
template repository、派生 session の lineage、削除・quota、および UI である。現在の snapshot は session ID
で上書きされるため、そのまま template key に使ってはならない。

## 4. ドメインモデル

永続化 repository に次の entity を追加する。

```go
type SessionContextTemplate struct {
    ID                 string
    SourceSessionID    string
    SnapshotID         string
    SnapshotGeneration int64
    Name               string
    Description        string
    OwnerUserID        string
    Scope              ResourceScope
    TeamID             string
    AgentType          string
    Repository         *RepositoryInfo
    SourceHeadSHA      string
    Status             string // preparing, ready, deleting, failed
    CreatedAt          time.Time
    CreatedBy          string
    LastUsedAt         *time.Time
    UseCount           int64
    ErrorCode          string
}
```

`SnapshotID` は `tpl_<random-id>` のようなランダムで immutable な object key とする。既存の
`safeSessionStateID` が `/` と `\\` を拒否する制約にも合わせる。manifest は snapshot の在処だけを指し、
token、環境変数、credential、署名 URL は含めない。

template は reusable であり、instantiate しても `ready` のまま残る。派生関係は template 自体に session ID
配列を埋め込まず、new session 側の `context_template_id` と lineage repository へ記録する。

既定では期限切れにしない。user/team ごとに個数と合計 snapshot bytes の quota を設け、利用者が明示的に
削除する。将来、任意の retention policy を追加できる。

## 5. API

### 5.1 session を workspace に変換する

```http
POST /sessions/{sessionId}/workspace
Idempotency-Key: <uuid>
Content-Type: application/json

{
  "name": "認証障害調査済みテンプレート",
  "description": "調査結果と未commitの修正を含む",
  "wait_for_idle": true
}
```

これは元 session を利用不能にするため、UI は確認ダイアログを必須にする。API client は操作名そのものを
明示的な意思表示として扱い、`DELETE` のような追加確認 token は要求しない。

処理は次の順で行う。

1. `CanModifyResource`、agent capability、storage capability、quota を検証する。
2. session ID 単位の排他 lock を取り、status を `templating` にする。この時点から新規 prompt を拒否する。
3. running の場合は `wait_for_idle=true` なら turn 完了を最大 30 秒待つ。timeout なら変換を中止する。
4. immutable `SnapshotID` へ checkpoint する。
5. object の存在、size、checksum と secret 非包含を検証する。
6. DB transaction で template を `ready` にし、元 session route を `templated` tombstone にする。
7. workload と session 専用の実行 resource を削除する。template snapshot と監査 metadata は残す。

成功レスポンス:

```json
{
  "id": "tpl_...",
  "source_session_id": "ses_...",
  "status": "ready",
  "name": "認証障害調査済みテンプレート"
}
```

checkpoint または transaction commit より前に失敗した場合は status を元に戻し、session を利用可能な
状態に保つ。commit 後の workload cleanup 失敗は reconciler が再試行し、session を再利用可能には戻さない。

- suspended session は共有 backend に snapshot があれば server-side copy で変換する。
- session 専用 PVC しかない場合、source workload を resume して export する必要がある。MVP では
  `422 template_not_portable` とする。
- error / stopped session でも有効な最終 snapshot があれば変換できる。
- persistence 非対応、非 ACP agent、snapshot 不在は `422 template_unsupported` または
  `409 snapshot_unavailable` とする。

長時間化に備えて `202 preparing` と `GET /workspaces/{id}` を正式な API とする。

### 5.2 workspace から新規 session を作る

既存 `/start` に top-level field を追加する。

```http
POST /start
Idempotency-Key: <uuid>
Content-Type: application/json

{
  "workspace_id": "tpl_...",
  "params": {
    "message": "この状態を起点に別の方式を試してください"
  },
  "session_profile_id": "optional-profile"
}
```

`workspace_id` は `params.resume_from` と排他的にする。controller が workspace を認可し、内部の
`RunServerRequest.ResumeFrom` に `SnapshotID` を設定する。execution-plane は template repository を参照せず、
既存の restore 処理を使う。

instantiate は template を consume しない。成功時に `UseCount` / `LastUsedAt` と lineage を更新する。
更新失敗で session 作成を失敗扱いにしないよう、session 側の lineage を正とし、集計値は非同期に修復できる
派生データとする。

```json
{
  "session_id": "ses_new",
  "context": {
    "workspace_id": "tpl_...",
    "source_session_id": "ses_source"
  }
}
```

### 5.3 workspace の管理

```http
GET    /workspaces
GET    /workspaces/{id}
PATCH  /workspaces/{id}
DELETE /workspaces/{id}
```

PATCH で変更できるのは name と description のみで、snapshot は変更しない。同じ作業状態の新版が必要なら、
template から session を作成して作業し、その session を別 template に変換する。

DELETE は snapshot と manifest を削除する。既に作成済みの派生 session には影響しない。削除中の新規
instantiate は拒否し、参照中の restore が終わってから object を削除する。

manifest export は `schema_version`, `template_id`, source metadata のみを返す。同一 deployment 内で認可して
解決する reference であり、Bearer credential にはしない。

## 6. 認可と情報の扱い

- template 化には source session の `CanModifyResource` を要求する。
- user scope の template は owner のみ利用・変更・削除できる。
- team scope は作成時、instantiate 時、変更・削除時に team への権限を検証する。
- user/team scope 間で移動しない。template と異なる scope の session 作成も禁止する。
- tombstone は監査と古い URL の説明表示のため保持するが、runtime route として解決しない。
- snapshot archive から credential、managed secret、runtime token、GitHub token を除外する。pack 対象を
  allowlist 化し、会話状態、workspace、必要な ACP metadata だけにする。
- 新 session の credential、profile、MCP、model connection は新しい起動要求から解決する。
- source と destination の `AgentType` は初期リリースでは一致必須とする。Claude ACP と Codex ACP の
  相互変換は会話要約方式を別機能として設計する。

## 7. repository と branch の扱い

会話だけでなく作業ツリーも snapshot に含まれるため、destination で repository clone 後に snapshot を
展開すると uncommitted changes まで引き継げる。

- source と destination の repository full name は一致必須。
- destination で repository を明示しなければ template の repository metadata を継承する。
- branch 名は新 session 用に通常どおり作り、snapshot の作業ツリーをその上へ復元する。
- source HEAD と clone 後 HEAD が異なる場合は起動を失敗させ、黙って merge/reset しない。
- source snapshot に `.git` を含める現行仕様なら、clone と二重管理しないよう restore 順序と pack 対象を
  integration test で固定する。将来的には Git metadata と workspace delta を分離する。

## 8. 整合性と競合

- `templating` へ遷移した時点で prompt、restart、resume、設定変更を `409 session_templating` にする。
- template 化と delete/suspend/restart の競合は session ID lock と status compare-and-swap で一つだけ通す。
- snapshot write は temporary key + atomic rename、または multipart complete で公開する。
- DB が `ready` なのに object がない状態を作らない。orphan object は background GC で回収する。
- 同じ idempotency key の再送は同じ template を返す。別 payload なら `409` とする。
- template の instantiate と delete は repository lease/reference count で調停する。
- 変換完了後の `/sessions/{oldId}` は `410 Gone` と template ID を返す。runtime access で自動 resume しない。

## 9. UI / CLI

セッションの操作メニューに「テンプレートとして保存」を追加する。

確認画面には次を表示する。

- 「変換後、このセッションでは作業を続けられません」
- 「会話と作業中のファイルは固定され、テンプレートから新しいセッションを何度でも作れます」
- template の名前と説明
- repository、agent type、scope、概算 snapshot size

完了後は session 画面を template 詳細画面へ遷移する。通常の session 一覧からは消え、
「テンプレート」タブに表示する。template 詳細には「このテンプレートからセッションを作成」、利用回数、
作成元 metadata、削除を表示する。

CLI:

```bash
agentapi-proxy client session templateize SESSION_ID \
  --name "認証障害調査済みテンプレート" \
  --wait-for-idle

agentapi-proxy client template list
agentapi-proxy client template start TEMPLATE_ID \
  --message "別の方式を試してください"
agentapi-proxy client template delete TEMPLATE_ID
```

## 10. 実装順序

### Phase 1: template 化と復元

1. snapshot key を session ID から独立させる。
2. `SessionContextTemplateRepository`、session tombstone、lineage と DB migration を追加する。
3. checkpoint command に destination snapshot ID を渡せるようにする。
4. `templating` の排他、template 化 API、認可、quota、idempotency を実装する。
5. `/start.context_template_id` を追加し、内部で既存 `ResumeFrom` に変換する。
6. 共有 S3 backend 上で Claude ACP / Codex ACP、local / External Session Manager の integration test を
   追加する。session 専用 volume backend は capability check で明示的に非対応とする。

### Phase 2: 管理導線

1. template 一覧・詳細・削除 API、CLI、UI を追加する。
2. source tombstone と派生 session の lineage 表示を追加する。
3. quota、orphan GC、利用数 metrics を追加する。

### Phase 3: portable template

deployment 間の移送が必要になった時だけ、暗号化 snapshot export/import を別途設計する。tenant ごとの
envelope encryption、署名 manifest、サイズ制限、malware/path traversal 検査、鍵ローテーションが必要に
なるため、同一 deployment 内の MVP に混ぜない。

## 11. テスト計画

- domain/repository: session から template への一方向遷移、再利用、idempotency、quota、削除、GC
- authorization: user/team、非 member、scope 越境、削除権限
- checkpoint: idle、busy、timeout、suspended、backend failure、checksum mismatch
- concurrency: prompt/suspend/restart/delete との競合、同時 template 化、instantiate 中の template 削除
- start: 同じ template から複数作成、二重送信、allocation failure からの retry
- security: archive の credential 非包含、path traversal、推測した ID、署名 URL 非露出
- compatibility: template を使わない `/start` と既存 restart/suspend/resume が不変
- E2E: 会話と未 commit 変更を作る → template 化 → 元が 410 → 複数 child で同じ初期状態を確認
- ESM: manager が異なっても共有 S3 backend なら復元でき、session 専用 volume backend では
  `422 template_not_portable` を返す

## 12. 受け入れ条件

- stable session を template 化すると workload が削除され、元 session では操作を続けられない。
- 変換失敗時は元 session が利用可能な状態に戻る。
- template は immutable な同じ会話状態と workspace から何度でも新 session を作成できる。
- template から作成した各 session は互いに独立し、一方の変更が template や他方へ影響しない。
- 古い session URL は自動 resume せず、template 詳細への案内を返す。
- scope 越境と権限のない利用・変更・削除ができない。
- secret と runtime token は snapshot / manifest を介して child にコピーされない。
- local と External Session Manager で同じ公開 API semantics を持つ。
- template を使用しない既存の作成、suspend/resume、restart の挙動を変えない。

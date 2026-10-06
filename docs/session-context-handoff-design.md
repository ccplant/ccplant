# セッション間コンテキスト引き継ぎ設計

## 1. 結論

特定のセッションから別の新規セッションを起動するときは、利用者が tarball のような
「サスペンド用ファイル」を直接扱う方式ではなく、proxy が管理する immutable な
`SessionHandoff` を介して起動する。

`SessionHandoff` は次の二つを組み合わせる。

- ACP の会話状態と作業ディレクトリを含む、既存の session state snapshot
- 引き継ぎ元、作成者、scope、agent type、snapshot 世代などを記録する小さな manifest

実体の snapshot は既存の永続化 backend に置き、manifest は repository に保存する。
利用者に見せる「引き継ぎファイル」が必要なら manifest の JSON を export できるようにするが、
秘密情報を含み得る snapshot 本体は download/upload させない。

新規セッション作成 API にすでに存在する `params.resume_from` は execution-plane の内部入力として
残し、公開 API では推測可能な session ID を直接受け付けず、認可済みの `handoff_id` を受け付ける。

## 2. 用語と、既存の suspend との違い

| 操作 | 論理 session ID | workload | 利用目的 |
| --- | --- | --- | --- |
| suspend / resume | 同じ | 停止後に再作成 | 同じセッションを一時停止する |
| restart | 同じ | 再作成 | 設定を変え、同じ会話を続ける |
| handoff / fork | 新しい | 新規作成 | 元を残したまま、別セッションへ文脈を渡す |

したがって handoff のために元セッションを suspend する必要はない。元が `stable` なら checkpoint
だけを作成し、元 workload は動かしたままにする。元を止めたい場合は handoff 成功後に利用者が
別途 suspend する。二つを一つの破壊的操作にしない。

## 3. 現状の再利用箇所

現在のコードには以下がすでにある。

- `SessionParams.ResumeFrom` / `RunServerRequest.ResumeFrom`
- `AGENTAPI_RESUME_FROM` と `SessionSettings.Session.ResumeFrom`
- provisioner の `restoreSessionState(sourceID, cwd)`
- Claude ACP / Codex ACP の checkpoint と volume / S3 session state store
- suspend 前の checkpoint、および restart 時の同一 session ID からの復元

不足しているのは、source session に対する認可、snapshot の世代固定、作成時点の整合性、
引き継ぎの有効期限、派生関係の記録、および UI である。特に現在の snapshot は session ID で
上書きされるため、`resume_from=<source session ID>` のままでは「handoff を作った時点」ではなく、
後から更新された状態を復元し得る。

## 4. ドメインモデル

永続化 repository に次の entity を追加する。

```go
type SessionHandoff struct {
    ID                string
    SourceSessionID   string
    SnapshotID        string
    SnapshotGeneration int64
    OwnerUserID       string
    Scope             ResourceScope
    TeamID            string
    AgentType         string
    Repository        *RepositoryInfo
    SourceHeadSHA     string
    Status            string // preparing, ready, consumed, expired, failed
    CreatedAt         time.Time
    ExpiresAt         time.Time
    CreatedBy         string
    ConsumedBySessionID string
    ErrorCode         string
}
```

`SnapshotID` は `hof_<random-id>` のようなランダムで immutable な object key とする。既存の
`safeSessionStateID` が `/` と `\\` を拒否する制約にも合わせる。
source session ID を storage key に使わない。manifest は snapshot の在処だけを指し、token、環境変数、
credential、署名 URL は含めない。

初期リリースでは handoff は single-use とする。新セッション作成を受理した transaction 内で
`ready -> consumed` と `ConsumedBySessionID` を確定し、同じ idempotency key の再送だけを同じ結果として
許可する。将来 branch を複数作りたくなった場合は `max_uses` を追加できる。

## 5. API

### 5.1 handoff の作成

```http
POST /sessions/{sourceSessionId}/handoffs
Idempotency-Key: <uuid>
Content-Type: application/json

{
  "wait_for_idle": true,
  "expires_in_seconds": 86400
}
```

成功時は checkpoint 完了後に返す。

```json
{
  "handoff_id": "hof_...",
  "source_session_id": "ses_...",
  "status": "ready",
  "expires_at": "2026-10-07T12:00:00Z"
}
```

- source が `running` のとき、`wait_for_idle=true` は turn 完了まで最大 30 秒待ち、超過時は
  `409 session_busy` を返す。実行中に filesystem をコピーしない。
- source が `stable` / `active` なら execution-plane に checkpoint を要求する。
- source が `suspended` なら、backend が server-side copy に対応する場合は最後の suspend snapshot から
  immutable copy を作り、workload は起こさない。session 専用 PVC の場合は source workload を一時的に
  resume して export する必要があるため、MVP では `422 handoff_not_portable` とする。
- source が `error` / `stopped` でも有効な最終 snapshot があれば作成可能とする。
- persistence 非対応、非 ACP agent、snapshot 不在はそれぞれ `422 handoff_unsupported`、
  `409 snapshot_unavailable` とする。

checkpoint command は最終的に `snapshot_id` を受け取り、その key へ直接保存する。storage に
server-side copy がある backend では、いったん source ID に保存してから転送せず copy を使う。

長時間化する backend に備えて、後続版では `202 preparing` と
`GET /session-handoffs/{id}` を追加できる。MVP は同期 30 秒 timeout でよい。

### 5.2 handoff から新規 session を作る

既存 `/start` に top-level field を追加する。

```http
POST /start
Idempotency-Key: <uuid>
Content-Type: application/json

{
  "context_handoff_id": "hof_...",
  "params": {
    "message": "前の調査を引き継ぎ、実装を完了してください"
  },
  "session_profile_id": "optional-profile"
}
```

`context_handoff_id` は `params.resume_from` と排他的にする。controller が handoff を認可・consume し、
内部の `RunServerRequest.ResumeFrom` には `SnapshotID` を設定する。execution-plane は handoff repository
を参照せず、今と同じ restore 処理を使う。

レスポンスには系譜を返す。

```json
{
  "session_id": "ses_new",
  "context": {
    "handoff_id": "hof_...",
    "source_session_id": "ses_source"
  }
}
```

便利な UI/CLI 用に `POST /sessions/{sourceSessionId}/fork` も提供してよい。これは内部的に
handoff 作成と `/start` を順に呼ぶ façade であり、永続化モデルと失敗 semantics は共通にする。

### 5.3 manifest export（任意）

```http
GET /session-handoffs/{id}/manifest
```

返す JSON は `schema_version`, `handoff_id`, source metadata, expiry のみとする。このファイルは
snapshot そのものではなく、同一 deployment 内で認可して解決する capability reference である。
Bearer credential にはしないため、ファイルを得ただけでは利用できず、通常の scope 認可も必須とする。

## 6. 認可と情報の扱い

- handoff 作成には source session の `CanAccessResource` ではなく `CanModifyResource` を要求する。
- user scope の handoff は owner のみ利用できる。
- team scope の handoff は、作成時と利用時の両方で対象 team へのアクセスを検証する。
- handoff を user/team scope 間で移動しない。別 scope での新規作成も禁止する。
- source session が削除されても、TTL 内の handoff は snapshot と一緒に残す。監査上の source ID は残す。
- snapshot archive から credential、managed secret、runtime token、GitHub token を除外する。現在の
  session-state pack 対象を allowlist 化し、会話状態、workspace、必要な ACP metadata だけにする。
- 新 session の credential、profile、MCP、model connection は新しい起動要求から解決する。
  source の secret-bearing settings をコピーしない。
- source と destination の `AgentType` は初期リリースでは一致必須とする。Claude ACP と Codex ACP の
  相互変換は会話要約方式を別機能として設計する。

## 7. repository と branch の扱い

会話だけでなく作業ツリーも snapshot に含まれるため、destination で repository clone 後に snapshot を
展開すると uncommitted changes まで引き継げる。衝突を避けるため次を検証する。

- source と destination の repository full name は一致必須。
- destination 側で repository を明示しなければ source の repository metadata を継承する。
- branch 名は新しい session 用に通常どおり作り、snapshot の作業ツリーをその上へ復元する。
- source HEAD と clone 後 HEAD が異なる場合は起動を失敗させ、黙って merge/reset しない。
- source snapshot に `.git` を含める現行仕様なら、clone と二重管理しないよう restore 順序と pack 対象を
  integration test で固定する。将来的には Git metadata と workspace delta を分離する。

## 8. 状態遷移と整合性

```text
request -> preparing -> ready -> consumed
              |          |
              v          +-> expired
            failed
```

1. API が source の認可と agent capability を検証し、`preparing` を作る。
2. source session ID 単位の lock を取り、idle を確認する。
3. execution-plane に immutable `SnapshotID` への checkpoint を要求する。
4. object の存在、size、checksum を確認して manifest を `ready` にする。
5. `/start` は idempotency record と同じ transaction で handoff を予約する。
6. session route/allocation と provision settings の保存成功後に `consumed` にする。
7. allocation 前に失敗した場合は予約を戻す。allocation 後の失敗は同じ start idempotency key で再開する。

snapshot の object write は temporary key + atomic rename、または multipart complete で公開する。
DB が `ready` なのに object が無い状態を作らない。期限切れと未参照 snapshot は background GC が削除する。
既定 TTL は 24 時間、最大 7 日とする。

## 9. UI / CLI

セッションの操作メニューに「このコンテキストから新しいセッション」を追加する。

1. source が busy なら「現在の処理の完了後に作成」と表示する。
2. checkpoint 中は進捗表示し、元セッションが停止するとは表示しない。
3. agent type、repository、scope は固定値として確認画面に表示する。
4. profile、model、pool、最初の追加指示は変更可能にする。
5. 新セッション画面には「セッション X から引き継ぎ」のリンクを表示する。

CLI は以下を追加する。

```bash
agentapi-proxy client session fork SOURCE_SESSION_ID \
  --message "続きを実装してください" \
  --profile PROFILE_ID

agentapi-proxy client handoff create SOURCE_SESSION_ID --format json
agentapi-proxy client start --handoff-id HANDOFF_ID
```

通常利用は `session fork`、自動化や時間差引き継ぎは二段階 API を使う。

## 10. 実装順序

### Phase 1: 最小の安全な fork

1. snapshot key を session ID から独立させる。
2. `SessionHandoffRepository` と DB migration を追加する。
3. checkpoint command に destination snapshot ID を渡せるようにする。
4. handoff 作成 API、認可、TTL、idempotency を実装する。
5. `/start.context_handoff_id` を追加し、内部で既存 `ResumeFrom` に変換する。
6. まず共有 S3 backend 上で Claude ACP / Codex ACP、local / External Session Manager の integration test を
   追加する。session 専用 volume backend は capability check で明示的に非対応とする。

### Phase 2: 利用導線

1. `session fork` CLI と UI 操作を追加する。
2. source / child の lineage 表示を追加する。
3. 非同期 preparing、status API、GC metrics を追加する。

### Phase 3: portable handoff

deployment 間の引き継ぎが必要になった時だけ、暗号化された snapshot export/import を別途設計する。
tenant ごとの envelope encryption、署名 manifest、サイズ制限、malware/path traversal 検査、鍵ローテーションが
必要になるため、同一 deployment 内の MVP に混ぜない。

## 11. テスト計画

- domain/repository: 状態遷移、single-use、TTL、idempotency、GC
- authorization: user/team、非 member、scope 変更、source 削除後
- checkpoint: idle、busy、timeout、suspended、backend failure、checksum mismatch
- start: ready/expired/consumed handoff、二重送信、allocation failure からの retry
- security: archive の credential 非包含、path traversal、推測した ID、署名 URL 非露出
- compatibility: handoff を使わない `/start` と既存 restart/suspend/resume が不変
- E2E: source で会話と未 commit 変更を作る → fork → child で履歴と変更を確認 → source も継続可能
- ESM: source と destination が異なる manager でも共有 S3 backend なら復元でき、session 専用
  volume backend では `422 handoff_not_portable` を返す

## 12. 受け入れ条件

- 利用者がアクセス可能な stable session から、別 ID の session を作成できる。
- child は checkpoint 時点の会話状態と workspace を復元し、source は停止・変更されない。
- checkpoint 後に source が進んでも child の復元内容は変わらない。
- handoff の横取り、再利用、scope 越境、期限後利用ができない。
- secret と runtime token は snapshot / manifest を介して child にコピーされない。
- local と External Session Manager で同じ公開 API semantics を持つ。
- handoff を使用しない既存の作成、suspend/resume、restart の挙動を変えない。

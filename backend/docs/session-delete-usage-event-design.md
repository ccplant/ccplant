# セッション削除時の利用イベント記録 詳細設計

## 目的

`agentapi_session_status_events` からセッション数と状態別の利用期間を復元できるように、
セッション削除の完了を終端イベントとして記録する。

現状は通常の状態遷移を追記している一方、ローカルセッションの削除と一部の remote
session の削除完了を記録していない。最後のイベントが `active` または `stable` の場合、
任意時点の snapshot は削除後もそのセッションを active と判定する。この設計では、
削除成功時に `terminated` を記録し、active 区間を有限にする。

## 状態の意味

| 用語 | 意味 |
| --- | --- |
| active 状態 | `active` または `stable` |
| 削除要求時刻 | API が有効な DELETE を受理し、削除処理を開始した時刻 |
| 削除完了時刻 | workload の削除成功、または対象が既に存在しないことを確認した時刻 |
| 終端イベント | `status = 'terminated'` の session status usage event |

終端 status は新しい `deleted` ではなく、公開 DELETE 応答と既存の status 判定で使用している
`terminated` に統一する。`terminating` は非同期削除の進行中状態であり、終端ではない。

active 期間は、active 状態へ遷移した時刻から次の非 active 状態への遷移時刻までとする。
synchronous delete では `terminated` が区間を閉じる。direct runtime の非同期 delete は、
既存どおり `terminating` が要求受理時に区間を閉じ、`terminated` が削除完了を表す。

## 対象範囲

対象に含める。

- 親 proxy が直接管理する session
- External Session Manager の同期削除
- direct runtime の queued deletion 完了
- 未割り当て allocation と pending external route の削除
- stale alias／既に remote workload が存在しない場合の cleanup
- cleanup worker や schedule から公開 DELETE 経路を利用する削除

token usage event の形式、public API response、OpenAPI schema、過去データの backfill は変更しない。

## 設計方針

### dimension を削除前に固定する

現在の `sessioncount.Worker.RecordStatus` は記録時に route、session、allocation、team を参照し、
`pool`、`scope`、`principal_id` を解決する。削除後はこれらが消えている可能性があるため、
DELETE の最後で既存メソッドを呼ぶだけでは記録できない。

破壊的操作の前に、次の immutable な snapshot を作成する。

```go
type SessionUsageDimensions struct {
    SessionID   string
    Pool        string
    Scope       string
    PrincipalID string
    LastStatusAt time.Time
}
```

`PrincipalID` は user scope なら `UserID`、team scope なら
`TeamConfig.PrincipalID()` とする。`Pool` は route の `Pool` を優先し、空の場合のみ allocation
から補完する。snapshot 作成に失敗しても DELETE は止めず、warning を記録する。

### recorder の責務

`sessionStatusUsageRecorder` に、解決済み dimension を扱う操作を追加する。

```go
type sessionStatusUsageRecorder interface {
    RecordStatus(context.Context, repositories.SessionStatusEvent) error
    ResolveDimensions(context.Context, string) (SessionUsageDimensions, error)
    RecordResolvedStatus(
        context.Context,
        SessionUsageDimensions,
        repositories.SessionStatusEvent,
    ) error
}
```

- `ResolveDimensions` は現行の route/session/allocation/team 解決を再利用する。
- `RecordResolvedStatus` はそれらを再参照せず `SaveEvent` まで進める。
- 通常の状態遷移は既存の `RecordStatus` を維持する。
- 型は controller package ではなく `modules/sessioncount` または usecase port に置く。

`event_id` は現行どおり
`SHA-256(session_id + NUL + status + NUL + occurred_at(RFC3339Nano))` とする。同じ timestamp で
retry すれば `INSERT OR IGNORE` により冪等になる。

### synchronous delete の順序

1. 認可を完了する。
2. `ResolveDimensions` で snapshot を作成する。
3. workload／allocation／route を削除する。
4. 成功、または対象が既に absent なら、削除完了時刻を一度だけ取得する。
5. snapshot と同じ時刻で `terminated` を記録する。
6. 補助設定、credential、cache を cleanup する。
7. API response を返す。

削除失敗時は `terminated` を記録しない。workload が存続している可能性を優先し、虚偽の
終端履歴を作らない。記録自体は best effort とし、失敗で DELETE response を 5xx に変えない。

## 削除経路別の処理

| 削除経路 | dimension の取得元 | 終端イベントの時点 |
| --- | --- | --- |
| 親 proxy の通常 session | session + allocation + team | `DeleteSessionByID` 成功後 |
| 未作成 allocation | allocation と作成時 metadata | `DeletePendingSessionAllocation` が `deleted=true` を返した後 |
| pending external route | route + team | route 削除成功、または absent 確認後 |
| ESM 同期削除 | route + team | manager が 200/204/404 を返し、local cleanup を完了した後 |
| direct runtime queued delete | 永続化済み route + team | command が 200/204/404 となり、route を削除する直前 |
| stale local alias | route + team | alias route の削除成功、または absent 確認後 |
| metadata も不存在の冪等 DELETE | 取得不能 | 新規イベントなし |

未作成 allocation に identity がなく、作成時 metadata も既に消えている legacy データは、削除を
優先して記録を skip する。新規 allocation は削除完了まで identity metadata を保持する。

### direct runtime の完了順序

`reconcileQueuedDeletion` では route が終端 event の唯一の metadata になり得る。

1. command result の成功を確認する。
2. route から dimension を解決する。
3. `Status = "terminated"` と `StatusUpdatedAt = completedAt` を route に Save する。
4. 同じ `completedAt` で終端 event を記録する。
5. route を削除する。

手順 4 が失敗しても手順 5 は続ける。手順 5 が失敗し再度 reconcile されても、route に固定した
時刻を再利用するため同じ event ID となる。

## 競合と冪等性

### 重複 DELETE

- 最初の DELETE が snapshot と終端 event を作る。
- 後続 DELETE で session／route が既に存在しない場合は 200 を維持し、新規 event は作らない。
- queued delete の retry は route に固定した `StatusUpdatedAt` を使用する。
- libSQL の primary key と `INSERT OR IGNORE` を最後の防御にする。

### late status update

終端より後の runtime status は session を復活させるため保存しない。direct runtime では既存の
`DeletionRequestID != ""` fence を維持する。親 proxy 管理 session は cancel 後の callback を
無視する。少なくとも同一 session では終端 event 後に非終端 event を保存してはならない。

同一 timestamp の順位を hash の辞書順に依存させない。削除完了時刻が直前の遷移時刻と同じなら
1ns 加算する。将来は session 単位 sequence の追加を検討するが、本変更では schema migration を
増やさない。

## 集計仕様

snapshot query は変更不要である。`terminated` は各 count の対象外なので、最新 event が終端に
なった時点から count が 0 になる。

active 区間は `LEAD` で次の遷移時刻を求める。

```sql
WITH transitions AS (
  SELECT session_id, status, occurred_at,
    LEAD(occurred_at) OVER (
      PARTITION BY session_id ORDER BY occurred_at, event_id
    ) AS next_occurred_at
  FROM agentapi_session_status_events
)
SELECT session_id, occurred_at AS active_from, next_occurred_at AS active_to
FROM transitions
WHERE status IN ('active', 'stable');
```

`active_to IS NULL` は現在も active、または終端 event が欠落していることを表す。現存 session に
対応しない NULL 区間を監視対象にする。

## rollout と観測性

schema migration は不要で、旧新 binary は同じ table を共有できる。過去の正確な削除時刻は復元
できないため自動 backfill しない。段階 rollout 中は旧 replica の DELETE に終端 event がない
可能性があるため、完全 rollout 時刻を dashboard の品質境界とする。

次の構造化 log を追加する。

- `session_usage_terminal_event_recorded`: session ID、pool、scope、delete path
- `session_usage_dimensions_resolve_failed`: session ID、delete path、error
- `session_usage_terminal_event_failed`: session ID、delete path、error

metrics を追加する場合は `session_status_usage_events_total{status,result}` とし、session ID、
principal ID、pool を label に含めない。

## テスト計画

### unit test

- snapshot 後に metadata を消しても `terminated` を保存できる。
- user/team scope で正しい principal ID を記録する。
- synchronous delete の 200/204/404 で終端 event を 1 件保存する。
- manager 5xx、delete failure、`deleted=false` では保存しない。
- queued delete は `terminating`、`terminated` の順に保存する。
- queued delete の retry でも `terminated` は 1 件だけになる。
- stale alias と pending route の cleanup でも保存する。
- metadata が最初からない冪等 DELETE は成功し、イベントを作らない。
- recorder failure は DELETE response を失敗させない。
- 直前 event と同じ clock 値でも終端が最新になる。

### repository／integration test

- 同じ event ID の複数保存が 1 row になる。
- `terminated` 後の snapshot query で各 count が 0 になる。
- `LEAD` query で `active_to` が終端時刻になる。
- direct runtime では要求時に `terminating`、manager 完了後に `terminated` となる。
- queued deletion の途中で proxy を restart しても終端 event が重複しない。

## 受け入れ条件

- すべての削除成功経路で、dimension を取得できる session に `terminated` が 1 件記録される。
- 削除失敗時に `terminated` は記録されない。
- 終端 event の失敗は session 削除を失敗させない。
- 削除後の snapshot で対象 session が active または all count に残らない。
- active 期間の終了時刻を status event だけから算出できる。
- public API と既存 table schema に破壊的変更がない。

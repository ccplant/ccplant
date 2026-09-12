# セッション自動サスペンド／透過レジューム設計

## 1. 目的

アイドル状態のセッション workload を個人・チーム単位のポリシーで自動停止し、
論理セッション、会話履歴、設定、永続 volume は保持する。利用者がそのセッションを
再び開いた場合は、明示的な resume 操作を要求せず API gateway で workload を再作成する。
フロントエンドは再作成中に通常のチャット画面を誤表示せず、`resuming` 画面を表示する。

本設計でいう「アクセス」は、セッション内 runtime へのアクセスである。セッション一覧、
全体 status stream、通知、共有状態の参照など、一覧表示やバックグラウンド処理による
read-only アクセスは resume の契機にしない。これにより一覧表示やポーリングによる全 session の
同時復帰を防ぐ。

## 2. 現状と前提

現在の Kubernetes Session Manager には次の土台がある。

- `session_persistence.suspend_after` という deployment 全体の固定値
- Service annotation に期限を保存する reconciler
- workload を削除して Service、Secret、snapshot、PVC を残す `SuspendSession`
- workload を冪等に再作成する `EnsureSessionWorkload`
- `POST /sessions/:sessionId/resume` と、フロントからの明示的 resume
- 内部状態名として `suspended`、復帰処理中の応答値として `restoring`

不足しているのは tenant 設定、session ごとの policy snapshot、runtime API 境界での透過的な
復帰、統一された `resuming` 状態、および専用 UI である。

## 3. 設定モデル

Settings に次の値オブジェクトを追加する。

```json
{
  "auto_suspend": {
    "enabled": true,
    "idle_timeout_minutes": 60
  }
}
```

- `enabled`: 自動サスペンドの有効／無効。省略時は deployment の既存設定から移行した既定値を使う。
- `idle_timeout_minutes`: 最後に activity が完了してから suspend までの分数。
- API とフロントは `1`〜`10080` 分（7日）の整数を許容し、カスタム値を入力できる。
- `enabled=false` の場合も timeout は保存し、再度有効化した際に以前の選択を維持する。
- `enabled=true` で timeout が未指定または許容外なら `400 invalid_auto_suspend_settings` とする。

設定解決は session の scope と一致させる。

| session scope | 使用する設定 | fallback |
| --- | --- | --- |
| `user` | session owner の個人設定 | deployment の既定値 |
| `team` | `team_id` のチーム設定 | deployment の既定値 |

チーム session に個人設定は混ぜない。誰が開いたかによって既存 session の寿命が変わることを
防ぐためである。設定は session 作成時に解決し、`auto-suspend-enabled` と
`auto-suspend-idle-seconds` を canonical Service annotation に snapshot する。既存 session は
annotation がなければ reconciler が owner/scope から一度だけ解決して backfill する。

設定変更の適用範囲は次の通りとする。

- 新規 session: 即時適用
- 既存の active session: reconciler の次回走査時に新設定を反映し、最後の activity から期限を再計算
- suspended session: 無効化しても自動 resume はしない。次回アクセス後は新設定を使用

## 4. activity とサスペンド判定

期限の基準は「作成時刻」ではなく最後の利用完了時刻とする。canonical Service に以下を保持する。

```text
agentapi.proxy/last-activity-at
agentapi.proxy/suspend-at
agentapi.proxy/suspended-at
agentapi.proxy/auto-suspend-enabled
agentapi.proxy/auto-suspend-idle-seconds
```

activity は以下で更新する。

- session 作成完了
- user message の受理
- agent turn の終了（`stable`、`error`、`timeout`）。通常はこの時刻が次の期限の起点
- resume 完了

`running`、`starting`、`resuming` の session は suspend しない。期限到達時に busy なら期限を
短い固定間隔で延長するのではなく、turn 終了イベントが新しい期限を設定するまで保留する。
複数 replica の競合は Service の `resourceVersion` を使った更新と、workload の create/delete の
冪等性で吸収する。checkpoint が失敗した場合は workload を削除せず、指数 backoff 付きで再試行する。

永続化 backend が無い session、oneshot session、当該 Session Manager が suspend capability を
持たない session では policy を無効として扱い、設定 UI には「永続化対応の実行基盤でのみ有効」
と明記する。

## 5. 状態遷移

公開状態名は `restoring` を廃止して `resuming` に統一する。移行期間中、フロントは両方を
`resuming` として解釈する。

```text
active/stable --idle deadline--> suspending --> suspended
suspended --runtime access-----> resuming  --> starting --> active/stable
                                  |              |
                                  +----error-----+
```

- `suspending`: checkpoint と workload 削除中。短時間でも観測可能にし二重操作を防ぐ。
- `suspended`: canonical resources は存在し、runtime workload は存在しない。
- `resuming`: workload の作成要求を受理済みだが endpoint は未 ready。
- `starting`: workload は存在するが agent runtime の ready を待っている。

status change は既存の proxy-wide SSE に流す。status API、session list、SSE で同じ公開状態を返す。

## 6. API レベルの透過 resume

### 6.1 resume 境界

`RouteToSession` の認可と route 解決の後、reverse proxy の直前に
`EnsureSessionAccess` を置く。この一箇所で local Kubernetes manager と External Session Manager
の両方を扱う。対象は `/:sessionId/*` の runtime route（message history、ACP metadata、ACP SSE、
prompt、tool status など）であり、個々の controller/client に resume 呼び出しを追加しない。

処理順は次の通り。

1. session と route を解決し、既存と同じ認可を行う。
2. session が active ならそのまま proxy する。
3. suspended なら manager の `EnsureSessionWorkload` を呼ぶ。操作は session ID 単位の singleflight
   と execution-plane の冪等 create で重複を抑止する。
4. ready になるまで最大 30 秒待つ。ready になれば元の HTTP request を変更せず一度だけ proxy する。
5. 30 秒以内に ready にならなければ、元 API の response schema を偽装せず
   `503 session_resuming` と `Retry-After: 2` を返す。

```json
{
  "error": {
    "code": "session_resuming",
    "message": "Session workload is resuming",
    "session_id": "...",
    "status": "resuming"
  }
}
```

GET/HEAD は client が安全に retry できる。書き込み request は gateway が body を保持して ready 後に
一度だけ upstream へ渡し、ready 待ち timeout 時には upstream へ一度も送らない。このため client の
retry で二重 prompt は発生しない。SSE は ready 後に接続を開始する。

明示的な `POST /sessions/:id/resume` は CLI と旧 client の互換性のため残すが、フロントの通常導線では
使用しない。session list、`/sessions/status/*`、`/sessions/:id/share`、削除、annotation 更新、明示的な
suspend は non-waking route とする。

### 6.2 External Session Manager

親 proxy は route の `manager_id` を解決後、manager control API の ensure endpoint を呼ぶ。
`resuming` を親の route/status cache に反映し、manager から ready 通知を受けたら元 request を転送する。
manager が offline の場合は resume を受理したふりをせず `503 session_manager_unavailable` を返す。

## 7. フロントエンド

session page に bootstrap state を設ける。

- 最初の runtime API が `session_resuming` を返した、または status stream が `resuming` / `restoring` を
  通知した場合、チャット本体の代わりに全体の `ResumingSession` 画面を表示する。
- 表示内容は spinner、「セッションを再開しています」、通常 1 分以内である旨、session 一覧へ戻る導線。
- status SSE が `active` / `stable` になったらチャット初期化を最初から一度だけ再実行する。
- SSE が切れている場合は `GET /sessions` または既存 status API を 2 秒から最大 10 秒の backoff で確認する。
- 2 分で timeout 表示に切り替え、「再試行」と一覧へ戻る操作を提示する。
- `error` / `unhealthy` では復帰失敗画面へ移り、通常の空チャット画面は表示しない。

sidebar にある `resumeSessionFromList` と明示 resume polling は削除する。sidebar の status dot と
badge には `suspending` と `resuming` を追加する。これにより URL 直打ち、別 client、モバイルからの
アクセスも同じ API semantics になる。

設定画面には個人・チーム共通の「セッション」ページを追加する。

- 自動サスペンド toggle
- 「操作がない状態から」select（15 分〜24 時間）
- 無効時は select を disabled にするが値は保持
- チーム画面では、この設定が team scope の session 全員に適用される旨を表示

## 8. API／スキーマ変更

- Settings request/response と保存 JSON に `auto_suspend` を追加
- session status enum に `suspending`, `resuming` を追加
- OpenAPI の settings schema、status enum、`503 session_resuming` response を更新
- `SessionWorkloadEnsurer` の戻り値は boolean ではなく enum/result に変更する

```go
type EnsureWorkloadState string
const (
    WorkloadReady    EnsureWorkloadState = "ready"
    WorkloadResuming EnsureWorkloadState = "resuming"
)

type EnsureWorkloadResult struct {
    Session entities.Session
    State   EnsureWorkloadState
}
```

boolean の意味の増加を避け、local/remote の応答を同じ型で扱う。

## 9. 移行と rollout

1. Settings の保存・読取と policy annotation の backfill を先に導入する。
2. status enum と UI の後方互換対応（`restoring` も受付）を導入する。
3. API gateway の transparent ensure を feature flag 下で有効化する。
4. sidebar の明示 resume を削除する。
5. 安定後、内部の `restoring` 表記と deployment-wide timer 直接参照を削除する。

deployment の `session_persistence.suspend_after` は migration fallback として一 release 以上残す。
未設定環境の挙動を変えないため、既存値が `0` / 空なら tenant の既定も disabled、正の値なら enabled
かつ最も近い許容時間へ丸めず、その秒数を server-side fallback として保持する。

## 10. テスト計画

- domain/repository/controller: 設定 round-trip、validation、個人・チーム認可、legacy JSON
- policy resolver: user/team/fallback、設定変更、既存 session backfill
- reconciler: idle/busy/checkpoint failure、disabled、複数 replica、期限再計算
- ensure: 同時アクセス、GET/POST/SSE、ready/timeout/error、remote manager offline
- route integration: suspended session の最初の request が resume 後に同じ upstream へ一度だけ届くこと
- frontend: resuming/restoring 表示、SSE による復帰、poll fallback、timeout/retry、設定保存
- E2E: session 作成 → idle suspend → URL 直アクセス → resuming UI → 履歴を保持した chat 復帰

## 11. 観測性と受け入れ条件

metrics は `session_suspend_total`, `session_resume_total`, `session_resume_duration_seconds`,
`session_resume_failures_total` を scope/manager/result（tenant ID は含めない）で記録する。log には
session ID、manager ID、遷移元／先、所要時間、失敗理由を含める。

受け入れ条件は以下とする。

- 個人・チーム設定で有効／無効と時間を保存・再読込できる。
- idle timeout 前、および agent 実行中には suspend されない。
- suspend 後も session 一覧と履歴の論理データが残る。
- session URL へのアクセスだけで workload が一度だけ復帰し、明示 resume API 呼び出しを必要としない。
- 復帰中は `resuming` 画面が表示され、ready 後に履歴付き chat へ自動遷移する。
- session 一覧や status polling だけでは workload が復帰しない。
- local manager と External Session Manager で同じ公開 API semantics になる。

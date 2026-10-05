# セッション利用統計画面設計

## 1. 目的

利用者が CCPlant 上で、個人またはチームのセッションがどの程度モデルを利用し、どのくらいの時間
稼働していたかを画面から把握できるようにする。現在の `/usage` は Parquet のダウンロードだけを提供しているため、
日常的な確認には外部ツールが必要である。本設計では `/usage` を利用統計画面へ拡張し、エクスポートを
同じ画面の補助操作として残す。

この画面が最初に答える問いは次の四つとする。

1. 選択期間にどれだけ利用したか。
2. いつ利用が増えたか。
3. どのセッションとモデルが利用量の多くを占めるか。
4. その月にセッションが何分稼働し、そのうち何分 agent が実行中だったか。

金額はモデル単価や契約形態に依存し、現行の usage event に価格情報もないため初期リリースでは扱わない。
「モデル利用量」は token 数を指す。「セッション利用量」は session status usage event から復元した
稼働時間と実行時間を指す。金額、CPU 使用時間、wall-clock 課金時間は意味が異なるため表示しない。

## 2. 現状と前提

バックエンドは response 単位の usage event を専用 libSQL に保存し、認可済みの個人またはチーム
scope に対して次の API を提供している。

- `GET /usage`: 合計、モデル別、セッション別の token 集計
- `GET /sessions/{sessionId}/usage`: 単一セッションの token 集計
- `GET /usage/export.parquet`: 最大90日、最大100,000 event の明細 export

`GET /usage` は `from`、`to`、`team_id`、`model`、`provider`、`agent_type`、`session_id` を受け取れる。
ただし、時系列集計、前期間比較、セッション表示名、breakdown の件数制限と pagination はない。

認可規則は既存 API を維持する。scope 未指定時は本人、`team_id` 指定時はアクセス可能なチームだけを
参照できる。全組織を横断する管理者専用画面は本設計の対象外とする。

usage collection が無効な環境では router に API 自体が登録されない。この状態と、collection は有効だが
選択期間に event がない状態を画面上で区別する。

別設定の `session_count` が有効な環境では、`agentapi_session_status_events` に session、pool、scope、
principal、status、遷移時刻が append-only で保存される。現状はこれを読み出す public API がなく、
稼働時間集計用 repository method と認可済み endpoint の追加が必要である。token usage と session status は
独立して有効化できるため、画面も片方だけを表示できるものとする。

## 3. 情報設計

### 3.1 導線

既存 Navigation の `Usage export` を `Usage` に変更し、遷移先 `/usage` は維持する。これにより
bookmark を壊さない。TopBar の Personal / Team selector を scope の唯一の切り替え手段とし、画面内に
重複した team selector は置かない。

画面内は `Model usage` と `Session runtime` の二つの view を segmented control で切り替える。scope と
期間は共通で、view を切り替えても維持する。異なる単位の token と分を同じ chart 軸や順位表へ混在させない。

セッション行から `/chats/{repository}` のような推測 URL へ直接遷移させない。session ID と既存の
session route 情報から正規の会話 URL を返せるようになるまでは、session ID の copy 操作と filter 適用を
提供する。セッション表示名を解決できる API 追加後に詳細導線を有効にする。

### 3.2 画面構成

desktop では期間内の時系列を主役にし、その下でセッションとモデルを比較する。均一な KPI card の
グリッドにはせず、総 token 数をチャートの左上に組み込む。

```text
+------------------------------------------------------------------------------+
| Usage                      Personal ▼       Navigation ▼        Settings     |
+------------------------------------------------------------------------------+
| 過去30日 ▼   2026/09/06–10/05     Model: All ▼     [Download Parquet]        |
|                                                                              |
|  12.4M tokens       1,284 responses      38 sessions                         |
|  Input 8.1M  Output 2.7M  Cached 1.6M                                      |
|                                                                              |
|  tokens                                                                      |
|  900k ┤                  ╭─╮                                                 |
|  600k ┤       ╭─╮       ╭╯ ╰╮       input / output / cached                  |
|  300k ┤  ╭────╯ ╰───────╯   ╰────╮                                          |
|     0 └────────────────────────────────  date                                |
+-------------------------------------------+----------------------------------+
| Sessions                                  | Models                           |
| Session              Responses     Tokens | Model        Share       Tokens  |
| fix checkout timeout       326      4.2M  | gpt-5.2      █████ 61%    7.6M  |
| add usage screen           211      2.8M  | claude...    ███   29%    3.6M  |
| ...                                         ...                               |
| [Show more]                                                                  |
+------------------------------------------------------------------------------+
```

`Session runtime` view は同じ骨格を使い、月次の時間配分を主役にする。

```text
+------------------------------------------------------------------------------+
| Usage                      Personal ▼       Navigation ▼        Settings     |
+------------------------------------------------------------------------------+
| [Model usage | Session runtime]     2026年10月 ▼       Pool: All ▼           |
|                                                                              |
|  842h 18m runtime      126h 42m running       Peak 18 concurrent             |
|  Active/idle 715h 36m  ·  Running 126h 42m  ·  Suspended is not counted     |
|                                                                              |
|  concurrent sessions                                                         |
|   20 ┤             ╭────╮                                                    |
|   10 ┤   ╭─────────╯    ╰──────╮       allocated / running                  |
|    0 └────────────────────────────────  October                              |
+------------------------------------------------------------------------------+
| Sessions                                                                     |
| Session                 Runtime       Running      Share       Current state  |
| fix checkout timeout     96h 12m       18h 04m      11.4%       suspended    |
| add usage screen         81h 33m       26h 41m       9.7%       active       |
| ...                                                                          |
| [Show more]                                                                  |
+------------------------------------------------------------------------------+
```

mobile では control を二段に折り返し、summary、chart、Sessions、Models の順に一列で表示する。表は
横スクロールさせず、token view は各行を「名前 / total tokens」と「responses / token 内訳」、runtime
view は「名前 / runtime」と「running / current state」の二段表示へ変える。

### 3.3 filter

- 期間 preset: `7日`、`30日`、`90日`。初期値は30日。
- custom range: 日付単位。`to` は選択した終了日の翌日 00:00 UTC を exclusive boundary として送る。
- model: 選択期間に存在するモデル。初期値は All。
- runtime view の期間: calendar month。初期値は現在月で、月 selector から過去月を選ぶ。
- pool: runtime view で利用できる logical pool。初期値は All。
- scope: 既存 TopBar selector に従う。
- URL query: `range` または `from` / `to`、`model` を保持し、再読み込みと共有を可能にする。

期間と model を変えたときは既存結果を薄く残して loading indicator を重ね、画面全体を空にしない。
scope を変えた場合は別 tenant の値を一瞬表示しないよう、直ちに旧結果を消して再取得する。

### 3.4 指標の定義

| 表示 | 定義 |
| --- | --- |
| Total tokens | `input + output + cached_input + cache_creation` |
| Responses | usage event 数 |
| Sessions | event が存在する distinct `session_id` 数 |
| Input | `input_tokens`。cached input を含むかは provider collector の入力値に従う |
| Output | `output_tokens` |
| Cached | `cached_input_tokens` |
| Cache write | `cache_creation_tokens`。0でなければ内訳 tooltip に表示 |
| Reasoning | `reasoning_tokens`。Total には加算せず内訳 tooltip に表示 |

Reasoning token は provider によって output token の内数になり得るため、Total への二重加算を避ける。
`session_id` が空の古い event は `Unknown session` にまとめるが Sessions 件数からは除外する。

### 3.5 稼働時間の定義

status event は状態の開始を表す。各 event の `occurred_at` から同じ session の次 event までを、その状態の
区間として扱う。問い合わせ期間を `[from, to)` とし、区間をその境界で切り詰めてから秒数を合計する。

| 表示 | 対象 status | 意味 |
| --- | --- | --- |
| Runtime | `creating`, `starting`, `active`, `stable`, `running`, `resuming`, `restoring`, `suspending` | workload が起動中、利用可能、処理中、または状態移行中だった時間 |
| Running | `running` | agent が turn を処理していた時間 |
| Active / idle | Runtime から Running を引いた時間 | session は存在するが agent が処理中ではなかった時間 |
| Suspended | `suspended` | Runtime に含めない。内訳としてのみ表示可能 |
| Unavailable | `stopped`, `error`, `timeout`, `unhealthy` | Runtime に含めない。内訳と品質確認に使用する |
| Terminated | `terminated` | 区間を終了し、以後は加算しない |
| Peak concurrent | Runtime 対象 status の同時区間数の最大値 | 最大同時稼働 session 数 |

status event だけでは CPU 使用率や process の生存を直接観測できないため、Runtime は厳密な compute 課金時間
ではなく、公開 session status 上で稼働していた時間とする。特に `stopped`、`error`、`timeout`、`unhealthy`
を無期限に加算すると大きく過大評価し得るため除外する。実基盤の課金時間が必要になった場合は workload の
start / stop event を別途収集する。

現在も続く最後の区間は、過去月では月末、現在月では API request の `as_of` まで加算する。これにより
現在月の値は時間とともに増える。response に `as_of` と `is_partial` を返し、画面に「10月5日 11:20 時点」
と表示する。

月境界と日次 bucket は選択した表示 timezone で決め、API には UTC の `from` / `to` と IANA timezone
（例: `Asia/Tokyo`）を送る。初期値は browser timezone とする。同じ瞬間を二重計上しないため区間は常に
半開区間 `[start, end)` とし、内部集計は秒、表示は最終段階で分へ丸める。session ごとに秒を合計してから
分へ丸め、行合計と summary の差が生じないよう summary も同じ未丸め値から計算する。

最初の status event より前は状態を推測しない。期間開始以前の最後の event がある場合だけ、その状態を
期間開始へ carry forward する。status event の記録失敗や履歴導入前の session は過少集計になり得るため、
API は `coverage_started_at` を返し、選択月より遅い場合は「記録開始以前の時間は含みません」と表示する。

## 4. Visual direction

既存画面の gray / blue palette と light / dark theme を維持し、利用状況を「計測器」のように読める
構成にする。装飾的な gradient や全 card への shadow は使わない。

- Base: `gray-50` / dark `gray-950`
- Surface: `white` / dark `gray-900`
- Primary series: `blue-600` / dark `blue-400`
- Output series: `violet-500` / dark `violet-400`
- Cached series: `emerald-500` / dark `emerald-400`
- Dividers and secondary text: 既存の `gray-200`, `gray-500` 系
- Type: 既存 application font を維持し、数値には `tabular-nums` を指定する

色だけで系列を区別せず、凡例、tooltip、線種または area pattern を併用する。総量の数値とグラフを
一つの分析面に置くことを、この画面固有の視覚的特徴とする。

runtime view では同じ色体系を状態へ割り当てる。allocated / idle は `blue`、running は `violet`、
suspended は `gray` とし、token view と runtime view の間で同じ色が異なる意味に見えないよう view 名と
凡例を常時表示する。時間軸を両 view の共通言語にすることが、この画面の一貫性となる。

## 5. API 設計

### 5.1 dashboard endpoint

初期表示で aggregate、trend、上位 breakdown を整合した一つの snapshot として取得するため、専用 API を
追加する。既存 `GET /usage` は互換性のため変更しない。

```http
GET /usage/dashboard?from=2026-09-06T00:00:00Z
  &to=2026-10-06T00:00:00Z
  &team_id=example/team
  &model=gpt-5
  &bucket=day
  &breakdown_limit=10
```

`bucket` は `day` のみから開始し、7日以下では将来 `hour` を追加できる形にする。bucket boundary と label は
UTC で返す。`breakdown_limit` は既定10、最大50とする。

```json
{
  "range": {
    "from": "2026-09-06T00:00:00Z",
    "to": "2026-10-06T00:00:00Z",
    "bucket": "day"
  },
  "summary": {
    "events": 1284,
    "sessions": 38,
    "input_tokens": 8100000,
    "output_tokens": 2700000,
    "cached_input_tokens": 1600000,
    "cache_creation_tokens": 0,
    "reasoning_tokens": 410000
  },
  "trend": [
    {
      "start": "2026-09-06T00:00:00Z",
      "events": 42,
      "sessions": 8,
      "input_tokens": 210000,
      "output_tokens": 72000,
      "cached_input_tokens": 49000,
      "cache_creation_tokens": 0,
      "reasoning_tokens": 11000
    }
  ],
  "by_session": [
    {
      "key": "session-id",
      "display_name": "fix checkout timeout",
      "events": 326,
      "input_tokens": 2800000,
      "output_tokens": 900000,
      "cached_input_tokens": 500000,
      "cache_creation_tokens": 0,
      "reasoning_tokens": 120000
    }
  ],
  "by_model": [],
  "available_models": ["gpt-5", "claude-sonnet-4-5"]
}
```

`display_name` は session metadata が現存する場合だけ backend で解決する。削除済み session でも usage は
残るため nullable とし、画面は短縮 session ID を fallback 表示する。session metadata を usage DB に複製しない。

repository には次の query を追加する。

- `AggregateDashboard(query, bucket, limit)`: summary、distinct session、trend、breakdown
- trend は recursive date series または application 側の zero fill により、event のない日も返す
- breakdown の並び順は Total tokens 降順、同値なら key 昇順で安定させる
- SQL の column 名は allowlist から選び、query parameter を直接埋め込まない

全 breakdown を返す現行 `GET /usage` はデータ増加に対して応答が肥大化するため、新画面からは呼ばない。

### 5.2 error semantics

| 状態 | HTTP / code | 画面 |
| --- | --- | --- |
| collection 無効で route なし | `404` | 設定が必要な disabled state |
| 未認証 | `401` | 既存 auth handler に委譲 |
| team 権限なし | `403` | scope を Personal へ戻す導線を表示 |
| 不正な期間・90日超 | `400 invalid_usage_range` | filter 直下に理由を表示 |
| storage failure | `500 usage_query_failed` | retry を表示し、export も無効化 |

backend は新 endpoint でも最大90日を強制する。`from` と `to` は必須とし、暗黙の期間を frontend と
backend で二重管理しない。

### 5.3 session runtime endpoint

session status event は token usage と別 DB・別 feature flag であるため、dashboard response へ無理に
結合せず専用 endpoint を追加する。

```http
GET /session-usage/dashboard?from=2026-09-30T15:00:00Z
  &to=2026-10-31T15:00:00Z
  &timezone=Asia%2FTokyo
  &team_id=example/team
  &pool=linux
  &bucket=day
  &breakdown_limit=10
```

```json
{
  "range": {
    "from": "2026-09-30T15:00:00Z",
    "to": "2026-10-31T15:00:00Z",
    "timezone": "Asia/Tokyo",
    "as_of": "2026-10-05T11:20:00Z",
    "is_partial": true,
    "coverage_started_at": "2026-08-12T03:10:00Z"
  },
  "summary": {
    "runtime_seconds": 3032280,
    "running_seconds": 456120,
    "suspended_seconds": 88200,
    "sessions": 38,
    "peak_concurrent": 18
  },
  "trend": [
    {
      "start": "2026-09-30T15:00:00Z",
      "runtime_seconds": 88200,
      "running_seconds": 12600,
      "peak_concurrent": 12
    }
  ],
  "by_session": [
    {
      "session_id": "session-id",
      "display_name": "fix checkout timeout",
      "runtime_seconds": 346320,
      "running_seconds": 65040,
      "suspended_seconds": 7200,
      "current_status": "suspended"
    }
  ],
  "available_pools": ["linux", "gpu"]
}
```

認可済み principal は controller で auth context と `team_id` から解決し、client が任意の
`principal_id` を指定できないようにする。user scope は本人の stable principal、team scope はアクセス可能な
team principal だけを query する。repository には `AggregateRuntime(query, bucket, limit, asOf)` を追加する。

SQL は対象 session ごとに `from` より前の最後の event を一件 carry forward し、期間内 event と合わせて
`LEAD(occurred_at, 1, effective_to)` で区間化する。`effective_to` は `min(to, as_of)` とする。status 集合を
Go と SQL に重複定義せず、domain package の分類関数と query 用 allowlist に集約する。peak concurrency は
各 runtime 区間の開始を `+1`、終了を `-1` とした sweep line で求め、同時刻では終了を先に適用する。

`session_count` が無効なら route を登録せず `404`、event がなければ `200` と zero summary を返す。
最大 query range は token dashboard と揃えて90日とする。月表示は1か月ずつ取得するため十分である。

## 6. フロントエンド設計

ページを責務で分割する。

```text
app/usage/page.tsx
  UsageFilters
  UsageOverview
    UsageTrendChart
    TokenBreakdown
  UsageBreakdown
    SessionUsageList
    ModelUsageList
  SessionRuntimeOverview
    SessionConcurrencyChart
    SessionRuntimeList
  UsageExportButton
```

- fetch は SWR を使用し、key に scope と filter 全体を含める。
- API response の snake_case を保つ `UsageDashboardResponse` type を追加する。
- chart は既存 dependency の Recharts を使用し、独自 chart library は追加しない。
- chart の tooltip と表の formatter は共通の `formatTokenCount` を使う。
- 1,000 未満は整数、1,000 以上は locale に応じた `1.2K` / `1.2M` の短縮表示とし、tooltip では正確な
  comma 区切り値を表示する。
- Parquet download URL は現在と同じ filter を引き継ぐ。download は通常の anchor とし、大きな file を
  JavaScript memory に載せない。
- runtime view は現在月だけ60秒ごとに再検証し、過去月は再検証しない。tab が非表示の間は更新を止める。
- duration は locale 対応 formatter で `842h 18m` のように表示し、1分未満は `<1m` とする。tooltip と
  accessible table では秒を丸めた完全な時・分を表示する。

## 7. 状態設計

### loading

summary の文字幅、chart、上位行に対応する skeleton を表示する。spinner だけの全面表示は使わない。

### empty

「この期間の利用データはありません」と表示し、期間を90日に広げる操作を置く。collection 無効とは
区別し、0 token の chart や空の table header を並べない。

### disabled

「選択した利用統計はこの環境で有効になっていません」と表示する。token collection または runtime
collection の片方が無効でも、利用可能な view は表示する。一般利用者には管理者への問い合わせを案内し、
設定値や Secret 名は露出しない。管理権限を判定できる場合だけ設定資料への link を表示する。

### partial metadata

削除済み session は短縮 ID（先頭8文字）と `Deleted session` を表示する。usage 自体は欠損扱いにしない。

## 8. Accessibility と responsive

- chart の直後に同じ日次値を読める visually-hidden table を置く。
- series の表示切り替えは button と `aria-pressed` を使い、legend click だけに依存しない。
- tooltip だけに情報を閉じず、summary と breakdown から主要値を読めるようにする。
- keyboard focus は既存 focus ring に合わせる。
- 320px 幅から操作可能にし、44px 程度の touch target を確保する。
- `prefers-reduced-motion` では chart の初回 animation を無効にする。
- loading / error 更新は `aria-live="polite"` で通知する。

## 9. 実装段階

### Phase 1: 最小の画面化

- 既存 `GET /usage` を使い、summary、モデル上位、セッション上位を表示
- 7 / 30 / 90日、scope、model filter
- empty / disabled / error state
- 既存 Parquet download を同じ filter bar に移動

時系列 chart はまだ出さない。現行 API の `by_session` が無制限であるため、Phase 1 は小規模環境向けの
暫定段階とし、長期運用前に Phase 2 へ進める。

### Phase 2: dashboard API と trend

- `GET /usage/dashboard` と repository aggregation
- 日次 trend chart、distinct session 数、breakdown limit
- session display name の best-effort 解決
- URL query 同期

### Phase 2b: session runtime

- `GET /session-usage/dashboard` と status interval aggregation
- 月次 Runtime / Running、日次推移、peak concurrency、session 別内訳
- timezone、現在月の `as_of`、coverage warning
- `Model usage` / `Session runtime` view 切り替え

### Phase 3: 比較と drill-down

- 直前の同期間との増減（API への比較範囲追加後）
- session 行からの正規 conversation URL
- model / session 別の二次 filter

価格表示、予算 alert、全組織横断 view、CPU / memory 使用時間は別設計とする。

## 10. テスト方針

### backend

- personal / team scope の認可と越境拒否
- range boundary が `[from, to)` であること
- UTC 日次 bucket と event がない日の zero fill
- distinct session で空 ID を数えないこと
- token category と Total の定義
- breakdown limit、安定 sort、90日制限
- 削除済み session の metadata fallback
- status interval の carry forward と `[from, to)` clipping
- 現在月の最後の区間を `as_of` で終了すること
- Runtime / Running / Suspended の status 分類
- peak concurrency で同時刻の終了を開始より先に処理すること
- timezone の月境界と DST を含む日次 bucket
- event 欠損時の `coverage_started_at` と zero summary
- principal 解決と user / team 間の runtime data 越境拒否

### frontend

- scope / range / model から正しい query を作ること
- scope 切り替え時に旧 tenant の結果を表示しないこと
- summary、chart、breakdown の formatter
- loading、empty、disabled、403、500 state
- export link が表示中 filter を引き継ぐこと
- keyboard による filter と series 操作
- mobile layout と dark theme の visual regression
- token / runtime の片方だけが有効な場合の view fallback
- 現在月だけを再検証し、非表示 tab で停止すること
- duration formatter と partial / coverage 表示

## 11. 受け入れ条件

- 利用者が Personal またはアクセス可能な Team の過去7 / 30 / 90日の token 利用量を `/usage` で確認できる。
- Total、Input、Output、Cached、Responses、Sessions の定義が UI と API で一致する。
- 日ごとの推移、利用量上位の session、model を一画面で把握できる。
- 選択月の Runtime、Running、peak concurrent と session 別の時間を確認できる。
- 月初以前から続く状態と現在も継続中の状態が、月境界と `as_of` で正しく切り詰められる。
- team の usage が権限のない利用者へ返らず、scope 切り替え時にも一時表示されない。
- collection 無効、event なし、API error を異なる状態として案内できる。
- 表示中の scope、期間、model と同じ条件で Parquet を download できる。
- 90日・100,000 event という既存 export 制約を維持する。

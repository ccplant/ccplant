# Session Pool Binding Quota Design

## Goal

Session Pool の Binding に同時実行数の上限を持たせ、ユーザーまたはチームごとに
cluster-wide Session Manager の利用量を制御する。

この設計では新しい Placement Policy や汎用 Quota リソースは追加しない。既存の
Logical Pool、Binding、Preference、Allocation を利用する。

## Scope

quota の設定項目は `max_concurrent` のみとする。

```go
type Binding struct {
	ID            string      `json:"id"`
	Pool          string      `json:"pool"`
	SubjectType   SubjectType `json:"subject_type"`
	SubjectID     string      `json:"subject_id"`
	Role          BindingRole `json:"role"`
	Enabled       bool        `json:"enabled"`
	Priority      int         `json:"priority,omitempty"`
	MaxConcurrent int         `json:"max_concurrent,omitempty"`
}
```

- `max_concurrent == 0`: 上限なし。既存 Binding もこの値になるため後方互換になる。
- `max_concurrent > 0`: この Binding に帰属する active Allocation の最大数。
- 負数は API で拒否する。
- quota は `use` と `manage` のどちらの Binding にも同じように適用する。

日次利用時間、CPU、メモリ、コストなどの quota は対象外とする。必要になった時点で
別フィールドとして追加し、最初から汎用的な quota expression は導入しない。

## Session owner

Binding の詳細度を決める owner は session scope から一意に決定する。

| Session scope | Exact subject | Subject ID |
| --- | --- | --- |
| user | `user` | authenticated user ID |
| team | `team` | request の `team_id` |

team scope では作成者個人の user Binding を評価せず、指定された team Binding を評価する。
user scope ではユーザーが所属する team の Binding を評価しない。これにより quota の帰属先が
作成者のチーム所属順に依存しない。

## Binding specificity

quota は Pool ごとに、次の順番で Binding を解決する。

1. session owner と完全一致する `user` または `team` Binding
2. `all` Binding
3. Binding なし

疑似コード:

```text
effectiveBinding(pool, session):
    owner = session.scope == team
        ? (team, session.team_id)
        : (user, session.user_id)

    if enabled binding(pool, owner.type, owner.id) exists:
        return exact binding

    if enabled binding(pool, all, "") exists:
        return all binding

    return none
```

同じ Pool と subject の組み合わせには既存実装どおり Binding を1つだけ作成できる。
したがって同じ詳細度で quota Binding が複数競合することはない。

exact Binding と `all` Binding の quota は累積しない。exact Binding が存在する場合は
exact Binding だけを適用する。exact Binding の quota が上限に達しても `all` Binding へ
fallback しない。fallback すると、狭い範囲に設定した quota を迂回できるためである。

既存 Resolver の `userID + teams` という入力は owner に置き換える。

```go
type Subject struct {
	Type SubjectType
	ID   string
}

func (r *Resolver) Resolve(ctx context.Context, owner Subject, tags map[string]string) (*ResolvedPool, error)
```

`ResolvedPool` は選択した Pool と effective Binding を一緒に返す。これにより Pool 選択後に
別のロジックで Binding を探し直して結果が変わることを防ぐ。user scope の Preference は
user Preference だけを、team scope の Preference は対象 team Preference だけを参照する。

例:

```json
[
  {
    "pool": "shared-cluster",
    "subject_type": "all",
    "subject_id": "",
    "max_concurrent": 100
  },
  {
    "pool": "shared-cluster",
    "subject_type": "team",
    "subject_id": "org/team-a",
    "max_concurrent": 10
  },
  {
    "pool": "shared-cluster",
    "subject_type": "user",
    "subject_id": "alice",
    "max_concurrent": 2
  }
]
```

- Team A の team session は Team A の Binding を使い、Team A 全体で10まで。
- Alice の user session は Alice の Binding を使い、Alice 個人で2まで。
- exact Binding がない owner は `all` Binding を使い、全 owner で共有する100まで。

Binding の詳細度は同一 Pool 内で解決する。別 Pool の Binding には影響しない。利用する
Pool 自体は既存の `allocator.pool`、Preference、default Pool の順序で決定するが、候補 Pool
の認可には owner の exact Binding または `all` Binding だけを使う。

## Allocation attribution

quota の使用数を追跡するため、Allocation に適用した Binding ID を保存する。

```go
type Allocation struct {
	// Existing fields omitted.
	QuotaBindingID string `json:"quota_binding_id,omitempty"`
}
```

`quota_binding_id` が空の既存 Allocation は quota の使用数に含めない。これにより移行時に
既存セッションが突然 quota を消費することを避ける。

active として数える Allocation status は次の4つとする。

- `pending`
- `leased`
- `claimed`
- `running`

`completed`、`failed`、削除済み Allocation は数えない。セッション削除、起動失敗、完了時に
quota reservation を必ず解放する。解放処理は同じ session ID に対して複数回呼ばれても
成功する idempotent な処理にする。

## Atomic reservation

`ListAllocations` で現在数を数えてから `Enqueue` する実装は使用しない。同時リクエストが
同じ値を読み、`max_concurrent` を超えて enqueue できるためである。

Binding ごとに quota usage record を持ち、KV Store の version compare-and-swap を使う。

```go
type QuotaUsage struct {
	BindingID string            `json:"binding_id"`
	Sessions  map[string]bool   `json:"sessions"`
	UpdatedAt time.Time         `json:"updated_at"`
}
```

セッション作成処理は次の順序にする。

1. Pool を決定する。
2. Pool と session owner から effective Binding を決定する。
3. Binding がなければ Pool を利用不可として拒否する。
4. `max_concurrent == 0` なら予約せず通常どおり enqueue する。
5. quota usage record を CAS で更新し、session ID を予約する。
6. Allocation に `quota_binding_id` を設定して enqueue する。
7. enqueue に失敗した場合は予約を解放する。

CAS conflict は最新 record を読み直して有限回 retry する。retry 後も競合する場合は
一時的な store error として扱う。

プロセス停止が手順6と7の間に起きると予約だけが残る可能性がある。そのため、usage record
の session ID に対応する active Allocation が存在しない場合に予約を除去する reconciliation
を起動時および定期的に実行する。

## Quota exceeded response

quota 超過時は allocation を作成せず HTTP `429 Too Many Requests` を返す。

```json
{
  "error": "session pool quota exceeded",
  "pool": "shared-cluster",
  "binding_id": "binding-...",
  "max_concurrent": 2,
  "active": 2
}
```

quota 超過を理由に、より広い `all` Binding、別 Pool、local Session Manager へ自動 fallback
しない。呼び出し元がセッションを減らすか、明示的に別の許可済み Pool を選択する。

## Interaction with existing selection

この quota は cluster-wide Session Pool 経由で作成される Allocation に適用する。

- Binding と Preference により対象ユーザー／チームを cluster Pool に配置できる。
- 対象 Binding がなければ、既存挙動どおり Pool は選択されず local Session Manager に進む。
- Pool が選択された後は、effective Binding の quota を検査してから enqueue する。
- `PoolSupplier.MaxRunners` は Manager 側のインフラ容量であり、Binding の
  `MaxConcurrent` はユーザー／チーム側の利用上限である。両方を維持する。

直接 `manager_id` を指定する legacy External Session Manager 経路と local Session Manager
には、この quota を適用しない。これらにも quota が必要になった場合は別途設計する。

## API changes

Binding の create、patch、response と OpenAPI schema に `max_concurrent` を追加する。

- create: 省略時は0。
- patch: 指定時だけ更新する。
- validation: 0以上。
- list/get: 常に現在値を返してもよいが、既存レスポンスとの簡潔さを保つ場合は0を省略する。

Preference API は変更しない。

## Required tests

Resolver / binding selection:

- user scope で user Binding が `all` Binding より優先される。
- team scope で team Binding が `all` Binding より優先される。
- team scope で作成者の user Binding を使用しない。
- user scope で所属 team の Binding を使用しない。
- exact Binding がない場合だけ `all` Binding を使用する。
- exact Binding の quota 超過時に `all` Bindingへ fallback しない。

Quota enforcement:

- `max_concurrent == 0` は無制限になる。
- active Allocation が上限未満なら予約して enqueue できる。
- active Allocation が上限と同じなら `429` になる。
- 同時作成でも上限を超えない。
- enqueue 失敗時に予約を解放する。
- completed、failed、deleted session の予約を解放する。
- 同じ session ID の解放を複数回実行できる。
- reconciliation が orphan reservation を除去する。
- `quota_binding_id` のない既存 Allocation は使用数に含めない。

API:

- create / patch で負の `max_concurrent` を拒否する。
- Binding response と OpenAPI schema に `max_concurrent` が含まれる。

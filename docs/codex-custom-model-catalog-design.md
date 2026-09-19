# Codex カスタムモデルカタログ設計（ACP チャットのモデル切り替え）

## 目的

ACP チャットの info パネル（Switch Model）から、セッションプロファイルの
`params.model_options` に並べたモデル（例: Ollama の `glm-5.3`）へ **実際に切り替えられる**
ようにする。

現状はプロファイル候補を UI に表示しても、`codex-acp` が
`session/set_config_option` を `-32602 Invalid params` で拒否する
（ACP モデル切替 UI を追加した [PR #353](https://github.com/ccplant/ccplant/pull/353) で
UI とエラーメッセージは改善済み。プロファイルの `params.model_options` はそこで追加した）。

## 調査結果（実測）

Codex CLI `codex-cli 0.154.0` / `@agentclientprotocol/codex-acp 1.11.0` で確認した。

### 1. `model_catalog_json` がカタログを差し替える

`~/.codex/config.toml` の `model_catalog_json` に JSON ファイルへのパスを書くと、
Codex のモデルカタログが **そのファイルの内容に置き換わる**（マージではない）。

```toml
model_catalog_json = "/home/agentapi/.codex/ccplant-model-catalog.json"
```

- `codex debug models --bundled` で同梱カタログ（JSON）を取得できる
- `codex debug models -c 'model_catalog_json="/tmp/cat.json"'` で内容を確認できる
- カタログ内のモデルだけが `model/list`（app-server）に出る

### 2. codex-acp の切り替えが成功する

カスタムカタログに `glm-5.2` / `glm-5.3` を入れて検証:

```
session/new → model options: ["glm-5.2", "glm-5.3"]
set model=glm-5.3 → OK
```

カタログ未登録時は同じ値が `-32602 Invalid params` になる。つまり
`model/list` → ACP `configOptions` → `session/set_config_option` の検証は
**すべて同じカタログを参照**しており、カタログに載せれば切り替え可能になる。

### 3. カタログの必須フィールド

`model_catalog_json` は厳格なスキーマ（serde）で、欠けたフィールドは読み込み時にエラーになる。
最小エントリは次の 12 フィールド + 指示文（858 バイト）で通ることを確認した。

| フィールド | 例 |
| --- | --- |
| `slug` | `glm-5.3` |
| `display_name` | `GLM 5.3` |
| `description` | `Ollama GLM 5.3` |
| `supported_reasoning_levels` | `[{"effort":"medium","description":"..."}]` |
| `shell_type` | `unified_exec` |
| `visibility` | `list` |
| `supported_in_api` | `true` |
| `priority` | `100` |
| `support_verbosity` | `false` |
| `truncation_policy` | 同梱モデルからコピー |
| `experimental_supported_tools` | `[]` |
| `base_instructions`（または `model_messages.instructions_template`） | 任意の指示文 |

同梱モデルを複製して `slug` / `display_name` / `description` だけ差し替える方法でも通る
（`model_messages` のプロンプト群をそのまま引き継げるため、推論・ツール設定が揃う）。

### 4. 置き換えであることの注意

`model_catalog_json` を指定すると **同梱モデルは消える**。指定しない場合の挙動
（同梱カタログ + リモート更新キャッシュ）に戻すには key 自体を書かない。

## 設計

### データフロー

```
session profile params.model_options
  └─ LaunchRequest.ModelOptions（実装済み: ACP モデル切替 UI の PR #353）
      └─ RunServerRequest.ModelOptions
          └─ applyModelConnections()            … 制御プレーン
              └─ settings.Env["CODEX_MODEL_OPTIONS"] = JSON 配列
                  └─ provisioner: CompileSettings()   … セッションコンテナ内
                      ├─ ~/.codex/ccplant-model-catalog.json を生成
                      └─ ~/.codex/config.toml に model_catalog_json を追記
                          └─ codex app-server: model/list
                              └─ codex-acp: configOptions / set_config_option
                                  └─ ACP チャット info パネルの Switch Model
```

### カタログ生成（コンテナ内で実行する理由）

必須フィールドは Codex のバージョンに依存する。制御プレーンで固定の JSON を
持つと codex 更新で壊れるため、**セッションコンテナ内で、そのコンテナに
インストール済みの codex 自身から生成する**。

`CompileSettings` の Codex 設定生成時（`generateCodexConfigTOML` の近傍）に:

1. `env["CODEX_MODEL_OPTIONS"]`（JSON 配列）を読む。空なら何もしない（従来動作）。
2. `codex debug models --bundled` を実行し同梱カタログを取得（オフライン・高速）。
3. 参照エントリ（既定: 同梱カタログの `visibility == "list"` の先頭、または現在の
   `CODEX_MODEL` と一致するエントリ）を複製し、`model_options` と現在モデルの各 slug について
   `slug` / `display_name` / `description` を差し替えて追加する。
   - `display_name` は slug をそのまま使う（Phase 1）
   - 同梱モデルはそのまま残す（既存ユーザーの選択肢を消さない）
   - 現在モデルも必ず含める（指示文・コンテキスト設定の参照先になるため）
4. `<CODEX_HOME>/ccplant-model-catalog.json` に書き出す。
5. 生成に成功したときだけ `config.toml` に `model_catalog_json = "<path>"` を追記する
   （`removeTopLevelTOMLKey` と同様、既存値を上書きして重複させない）。

生成に失敗した場合（`codex` バイナリが無い、同梱カタログが取得できない等）は key を書かず、
警告ログのみで続行する。パース検証は行わない: Codex は未知の config キーを無視するため
（`codex debug models -c 'zzz_bogus_key=1'` がエラーにならないことを確認済み）、
`model_catalog_json` 未対応のバージョンでも key を書くだけで起動は壊れない。

なお `CompileSettings` は互換コネクション（`openai_compatible` 等）が provider TOML を所有する
場合に env を null 化するが、カタログ生成用の env は `codexModelCatalogEnv` で別途組み立てて
維持する。現在モデルは接続の `model` を優先する（Ollama などは接続側にモデルが入るため）。

### 制御プレーンの変更

`KubernetesSessionManager.applyModelConnections()`（`backend/internal/infrastructure/services/model_connections.go`）で、
`codex-acp` のとき `req.ModelOptions` を env へ materialize する:

```go
if len(req.ModelOptions) > 0 {
    if encoded, err := json.Marshal(req.ModelOptions); err == nil {
        settings.Env["CODEX_MODEL_OPTIONS"] = string(encoded)
    }
}
```

- 既存の `CODEX_MODEL_*` 系と同じ env チャネルに乗せる（新しい YAML フィールドを増やさない）
- 再起動時は settings を再解決するため、`model_options` の変更は再起動/新規セッションで反映される
  （`ValidateRestart` は `model_options` を制約しない）
- `pi-ollama` / `claude-acp` は対象外（Claude は SDK 側のモデル解決、pi は別途）

### フロントエンド

カタログが適用されるとプロファイル候補は **エージェントが提示する候補** として返るため、
PR #353 で追加した「プロファイルのモデル候補（エージェント未対応の場合は拒否されます）」
グループは自然に空になる。追加対応:

- プロファイル編集画面の注意書きを「codex-acp ではモデル候補が Codex のカタログに登録され、
  セッション開始後に Switch Model で選択できます」に更新する
- カタログ生成に失敗した場合に備え、選択時 `-32602` のメッセージは現状のまま維持する

### セキュリティ

- `model_options` はプロファイル保存時に `modelprovider.ValidateModel` で検証済み（PR #353）
- モデル slug は provider へのリクエストのモデル名になるだけで、接続先・資格情報は変更しない
- 生成ファイルはセッションコンテナ内の `CODEX_HOME` に閉じる

## テスト計画

- 単体（Go）: カタログ生成関数
  - `model_options` が空なら key を書かない
  - 同梱カタログのモデルが維持され、指定モデルが追加される
  - 現在モデルが必ず含まれる
  - 重複 slug / 空文字 / 不正 slug を無視する
  - 同梱カタログの取得に失敗した場合、`model_catalog_json` を書かない
- 単体（Go）: `applyModelConnections` が `CODEX_MODEL_OPTIONS` を設定し、
  他エージェント種別では設定しない
- 統合（dev）: `codex-ollama` プロファイルに `model_options = ["glm-5.2", "glm-5.3"]` を設定し、
  chat info パネルの Switch Model で `glm-5.3` に切り替えられることを確認
  （`GET /session` の `configOptions` に現れることも確認）

## 代替案と判断

| 案 | 判断 |
| --- | --- |
| codex-acp をパッチして任意モデルを許可 | サードパーティ実装の fork/パッチは保守負荷が高く不採用 |
| モデル変更時にエージェントを再起動して会話を再開 | ccplant 側で `ValidateRestart` がモデル変更を禁止しており、ACP セッションの再開処理も大掛かり。不採用 |
| **`model_catalog_json` にカスタムモデルを登録** | Codex 純正の機構で、ACP の検証も自然に通る。採用 |

## 未決事項

- 同梱モデルをカタログに残すか（既定: 残す。プロファイルで「このリストだけにする」指定が
  できるようにする余地はある）
- 表示名・コンテキスト長・推論レベルなどをプロファイルから指定できるようにするか
  （Phase 2 で `model_options` をオブジェクト形式に拡張する想定）
- codex のバージョン更新で必須フィールドが増えた場合の検知（生成は
  `codex debug models --bundled` の出力を複製するため通常は追随できるが、
  codex-acp 側でカタログが読めなかった場合はログ監視が必要）

# Codex 認証設定への OpenAI 互換 API 追加

ステータス: 設計案（実装はこの PR の対象外）

## 目的と対応範囲

個人・チームの「設定 → Codex 認証」で接続方式、Base URL、モデル、API キーを登録し、環境変数や TOML を手動編集せず Codex ACP を起動できるようにする。初期版はスコープごとに互換 API 接続を一つ保存する。

対象は **Responses API 互換**の接続先。Chat Completions のみ対応するサービスには直接接続できない。ストリーミングと Codex が使うツール呼び出しにも対応が必要であり、「OpenAI 互換」という名称だけで動作保証はしない。プロトコル変換プロキシ、複数接続の管理、独自ヘッダー・Azure 固有設定、自動モデル一覧取得は初期版に含めない。

公式仕様: [Configuration Reference](https://learn.chatgpt.com/docs/config-file/config-reference) は `wire_api` の対応値を `responses` のみとしている。[Advanced Configuration](https://learn.chatgpt.com/docs/config-file/config-advanced) にカスタムプロバイダの `base_url`・`env_key`・`model_provider` が記載されている。実装時には配布する Codex / codex-acp のバージョンでも検証する。

## 現状と再利用する処理

- `frontend/src/app/settings/sections/CodexAuthSection.tsx` は個人・チーム共通の認証画面。`CodexCredentialsSettings.tsx` にデバイス認証と auth.json アップロードがある。
- `backend/pkg/sessionsettings/compile.go` は既に `OPENAI_BASE_URL` から `agentapi_openai_compatible` プロバイダを生成する。モデルは `CODEX_MODEL`、次いで `OPENAI_MODEL`、キーは `OPENAI_API_KEY` を参照し、`wire_api = "responses"` を出力する。
- `KubernetesSessionManager.resolveAutoAgentType` は配置予定の auth.json の有無で自動選択しているため、互換 API 設定だけでは Codex を選べない。
- Settings API は環境変数の値を返さず、保存では空文字を既存値の保持として扱う。URL・モデルの再表示やキー削除を単なる環境変数フォームで実現するのは不適切。
- Settings repository の環境変数暗号化は構成依存（未設定・noop では平文）。新しいキーの保存も既存の暗号化基盤を利用し、常時暗号化されるという誤った説明はしない。

## 画面

既存の個人・チームの `codex-auth` ページを利用する。ページ冒頭に現在の接続方式、設定元、設定済み／未設定を表示し、auth.json の存在だけで接続状態を表現しない。

```text
Codex 認証
接続方式  [既存の認証（auth.json） | OpenAI 互換 API]

OpenAI 互換 API（Responses API）
Base URL      [https://llm.example.com/v1]
モデル ID     [接続先で利用できるモデル ID]
認証          [API キー | 認証なし]
API キー      [新しいキーを入力]   保存済み
詳細設定      コンテキスト長／自動圧縮開始トークン数
              reasoning summaries の対応（未指定／対応／非対応）
[保存して使用]

次に開始するセッションから適用されます。
```

デバイス認証とアップロードは「既存の認証」配下に残す。方式変更と接続情報の保存は一回の操作で確定する。入力中には保存せず、保存成功後に有効な方式を更新する。認証方式だけ先に変わる状態を作らない。

API キーは password 入力とし、保存済み値を取得・再表示しない。変更しなければ保持し、「キーを削除」を別操作で明示する。認証なしはユーザーが選んだ場合だけ有効。方式切替時も保存済み auth.json と API 設定は保持し、削除操作を分ける。デバイス認証完了やアップロードだけで互換 API の選択を変更しない。

「AI プロバイダ」ページの auth.json 必須に見える説明と、セッション作成・プロファイルの自動選択の説明も更新する。保存完了と実接続成功を区別し、初期版に接続確認ボタンは設けない。

## データと API

既存の `GET/PUT /settings/{name}` に `codex_connection` を追加する。既存の Claude 用 `auth_mode` は変更しない。更新権限は既存 Settings API の個人・チーム認可に従う。

PUT の例（新規保存）:

```json
{
  "codex_connection": {
    "mode": "openai_compatible",
    "base_url": "https://llm.example.com/v1",
    "model": "provider-model-id",
    "authentication": "api_key",
    "api_key": "<write-only>"
  }
}
```

| 項目 | 契約 |
| --- | --- |
| `mode` | `auth_json` / `openai_compatible`。オブジェクト未登録は従来動作 |
| `base_url`, `model` | 互換 API 有効時は必須。GET でも返す |
| `authentication` | `api_key` / `none`。空キーによる暗黙の認証なしは禁止 |
| `api_key` | PUT のみ。省略で保持、空文字はエラー、非空で置換 |
| `clear_api_key` | PUT のみ。true で削除。キーとの同時指定はエラー |
| `has_api_key` | GET のみ。実値はレスポンス・ログに含めない |
| `context_window` | 任意の正整数 |
| `auto_compact_token_limit` | 任意の正整数。指定時は context_window 必須、その値未満 |
| `supports_reasoning_summaries` | 任意の boolean。省略は未指定 |

`codex_connection` 自体の省略は変更なし。オブジェクト指定時は mode 必須、その他の省略フィールドは保持、詳細設定は null で解除する。方式変更は保存後の全体を検証する。API キー認証を有効なままキーだけ削除する更新は拒否し、認証なしまたは auth.json 方式への変更と同時に行う。

設定と秘密情報は同じ repository 更新で保存する。キーは通常の公開 DTO から分離し、既存の暗号化サービスと同じメタデータ形式を使う専用 secret フィールドへ格納する。`env_vars` に二重保存しない。repository の往復、設定同期、インポート／エクスポートも確認し、一般の設定出力にはキーを含めない。

Base URL は絶対 HTTP(S) URL として解析し、userinfo・query・fragment を拒否する。`/responses` や `/chat/completions` を末尾に付けた入力にはベース URL を求めるエラーを返す。`/v1` は自動追加せずカスタムパスを保持する。HTTP はローカル・社内接続向けに許容する。モデル ID の制御文字・空白のみの値、数値の範囲外は拒否する。

## 設定解決と起動

接続は **URL・モデル・キー・認証方式を一組として解決**する。通常の環境変数マージで個人の URL とチームのキーを合成しない。

1. 既存の `credentialOwnersForRequest` に基づいて利用可能な認証元を列挙する。`session_user`、`team`、`triggered_user`、`github_sender` の選択と権限を維持する。単に個人優先とする別のルールを追加しない。
2. 候補順に、明示された `codex_connection`、または既存の認証ファイル集合を採用する。前の候補の有効な認証ファイル集合を採用した場合は、後の候補の互換 API キーを混ぜない。
3. 明示設定が不完全・復号不能ならエラーにする。別の所有者や auth.json へ暗黙に切り替えない。`credential_source=none` では管理された接続キーも注入しない。
4. 共通 resolver の結果をエージェント自動選択と設定生成の双方へ渡す。互換 API が有効なら `auto` は `codex-acp`、auth.json 方式なら従来判定。明示された別エージェントは優先し、Codex 用キーは注入しない。
5. 選択されたセッションにだけキーを注入し、Codex provider を生成する。プロキシ本体や共有ホームの設定を変更しない。Kubernetes・外部 Session Manager の両起動経路で同じ解決結果を使う。

新設定では汎用の `OPENAI_API_KEY` を上書きせず、専用の `CCPLANT_CODEX_API_KEY` を provider の `env_key` に指定する。compile 処理に構造化した接続設定を渡し、既存 TOML 生成ロジックを拡張する。

```toml
model = "provider-model-id"
model_provider = "agentapi_openai_compatible"

[model_providers.agentapi_openai_compatible]
name = "OpenAI compatible"
base_url = "https://llm.example.com/v1"
wire_api = "responses"
env_key = "CCPLANT_CODEX_API_KEY"
requires_openai_auth = false
```

認証なしの場合は `env_key` を出力しない。キーの実値を TOML やコマンド引数に埋め込まない。互換 API のセッションには管理下の Codex auth.json を配布・同期しない。保存済み auth.json はサーバーに残し、方式を戻した次のセッションで再利用する。

構造化設定があれば環境変数による旧プロバイダ生成より優先する。明示 `auth_json` の場合も、旧 `OPENAI_BASE_URL` による互換 API への切替を抑止する。新設定が存在しない場合だけ従来の環境変数方式を維持し、自動移行しない。従来利用者の画面には「環境変数による設定あり」を示し、キーを表示せず明示的な再登録を案内する。

TOML の同名 provider は置換し、重複テーブルを生成しない。フック・sandbox 等は維持する。新設定で詳細値が未指定なら生成側でモデルの能力値を推測せず、Codex／接続先の既定に任せる。旧方式の 128000/64000 の補完は後方互換として残す。

セッション開始時の解決結果を固定し、設定更新で稼働中セッションの接続先を変更しない。再開・復元では保存された方式と接続先を維持し、復元不能ならエラーにする。別プロバイダへ自動フォールバックしない。セッション側 localhost はブラウザやプロキシサーバーの localhost とは異なることを入力補足に示す。

## 実装単位と受け入れ条件

1. Settings entity / controller / repository / frontend 型に構造化接続と write-only キーを追加。設定更新の検証と秘密情報の保存を実装。
2. 認証元 resolver を共通化し、KubernetesSessionManager の通常起動・外部起動、自動選択、統一 SessionSettings、compile、管理ファイル同期へ接続。
3. `CodexAuthSection` に接続フォームを追加。`ImmediateSaveNotice` は操作単位の保存を正しく説明する表示に調整し、関連説明文を更新。
4. 以下のテストと配布ランタイムでの実接続確認を完了してリリース。

| 検証 | 期待結果 |
| --- | --- |
| 個人／チームの保存と再表示 | URL・モデル・方式を再表示し、キー実値を返さない |
| キー保持・変更・削除、不正な更新 | 明示した契約どおり動作し、失敗時は旧設定全体が残る |
| repository 暗号化と復号失敗 | 非 noop 時の保存値に実キーを含めず、復号失敗は明示エラー |
| 認証元選択・フォールバック・none | 許可された同一所有者の接続を採用し、異なる所有者のキーを混ぜない |
| auth.json なし＋互換 API＋auto | Codex ACP を選択し、ログインを要求しない |
| auth.json へ戻す／別エージェント明示 | 旧環境変数による意図しない切替や Codex キー注入がない |
| TOML 合成・再生成 | パース可能、provider 重複なし、既存フック保持、秘密値なし |
| 旧設定のみ | 従来の環境変数設定・auth.json 起動が維持される |
| 起動・再開・外部 Session Manager | 選択した接続と方式を維持し、稼働中に勝手に切り替わらない |
| Responses 接続の実機確認 | ストリーミング応答とツール実行・結果返却が成功する |
| 401、404、到達不能、非対応モデル | 秘密情報を伏せて原因を表示し、別接続へ切り替えない |

この設計 PR ではコード変更・ランタイム検証を行わない。実装 PR では設定 API、resolver、compile の単体テストとフォーム操作テストを追加し、対応する Codex / codex-acp のバージョンと実機確認した接続先を記録する。

# Codex / Claude Code 認証設定への互換 API 追加

ステータス: 実装済み・レビュー待ち（Fly 開発環境で Ollama Cloud を実機検証済み。[検証結果と制限](ollama-fly-dev-verification.md)）

## 目的と対応範囲

個人・チームの認証設定で接続方式、Base URL、モデル、API キーを登録し、環境変数や設定ファイルを手動編集せず Codex と Claude Code を互換 API 経由で起動できるようにする。初期版はスコープごと・エージェントごとに接続を一つ保存する。両方を同時に登録でき、それぞれ独立して切り替える。

| エージェント | 追加する接続方式 | プロトコル | 対象起動方式 |
| --- | --- | --- | --- |
| Codex | OpenAI 互換 API | Responses API | `codex-acp` |
| Claude Code | Anthropic 互換 API | Anthropic Messages API | `claude-acp` と既存 `claude-legacy` |

ストリーミングと各エージェントが使うツール呼び出しへの対応が必要であり、互換 API の名称だけで動作保証はしない。Chat Completions のみの接続先、プロトコル変換プロキシの実装、一つのエージェントの複数接続管理、独自ヘッダー・Azure 固有設定、アプリ側の自動モデル一覧取得は初期版に含めない。

公式仕様: [Configuration Reference](https://learn.chatgpt.com/docs/config-file/config-reference) は `wire_api` の対応値を `responses` のみとしている。[Advanced Configuration](https://learn.chatgpt.com/docs/config-file/config-advanced) にカスタムプロバイダの `base_url`・`env_key`・`model_provider` が記載されている。実装時には配布する Codex / codex-acp のバージョンでも検証する。

Claude Code は公式の [Gateway 接続設定](https://code.claude.com/docs/en/llm-gateway-connect) と [互換性ガイド](https://code.claude.com/docs/en/llm-gateway-protocol) に基づく。Messages の `/v1/messages`、ストリーミング、`tool_use` / `tool_result` を検証する。`/v1/messages/count_tokens` は任意対応として、その有無の両方を試す。ゲートウェイが上流へ転送する `anthropic-version` / `anthropic-beta` と機能互換性も確認する。非 Claude モデルへのルーティングは [Anthropic の公式サポート対象外](https://code.claude.com/docs/en/llm-gateway) なので、本機能の接続設定と個々のモデルの動作保証は区別する。

## 現状と再利用する処理

- `frontend/src/app/settings/sections/CodexAuthSection.tsx` は個人・チーム共通の認証画面。`CodexCredentialsSettings.tsx` にデバイス認証と auth.json アップロードがある。
- `backend/pkg/sessionsettings/compile.go` は既に `OPENAI_BASE_URL` から `agentapi_openai_compatible` プロバイダを生成する。モデルは `CODEX_MODEL`、次いで `OPENAI_MODEL`、キーは `OPENAI_API_KEY` を参照し、`wire_api = "responses"` を出力する。
- `KubernetesSessionManager.resolveAutoAgentType` は配置予定の auth.json の有無で自動選択しているため、互換 API 設定だけでは Codex を選べない。
- `AIProvidersSection.tsx` に Claude OAuth と Bedrock 設定がある。`ClaudeOAuthSettings.tsx` の方式選択は OAuth トークンの有無に依存しているため、接続方式セレクターを外に出し、トークン未登録の個人・チームでも互換 API を選べるようにする。
- `backend/pkg/settingspatch/materialize.go` は `auth_mode=oauth` で `ANTHROPIC_MODEL` を削除し、方式判定後に OAuth トークンを注入する。環境変数を追加するだけではモデルが消えたり認証が混在したりするため、明示された接続方式に応じて生成処理を分岐する。
- `buildSessionSettings` は materialize 後にもプロファイル・開始リクエストの環境変数をマージする。接続解決はその最終結果まで扱い、後続の URL だけの上書きによるキーの誤送信を防ぐ。
- Settings API は環境変数の値を返さず、保存では空文字を既存値の保持として扱う。URL・モデルの再表示やキー削除を単なる環境変数フォームで実現するのは不適切。
- Settings repository の環境変数暗号化は構成依存（未設定・noop では平文）。新しいキーの保存も既存の暗号化基盤を利用し、常時暗号化されるという誤った説明はしない。

## 画面

既存の個人・チームの `codex-auth` ページを利用する。Claude 側は既存 `ai-providers` ページに「Claude Code 認証」セクションを設け、OAuth・Bedrock・Anthropic 互換 API を一つの方式セレクターで選ぶ。Codex 認証へのリンクを同ページに置き、二つの認証設定を行き来できるようにする。

各セクションの冒頭に現在の接続方式、設定元、設定済み／未設定を表示し、auth.json や OAuth トークンの存在だけで接続状態を表現しない。互換 API フォームの URL・モデル・キー入力と保存状態は共通コンポーネントとし、選択肢・URL 補足・詳細設定はエージェントごとに変える。

```text
Codex 認証
接続方式  [既存の認証（auth.json） | OpenAI 互換 API]

OpenAI 互換 API（Responses API）
Base URL      [https://llm.example.com/v1]
デフォルトモデル ID [接続先で利用できるモデル ID]
認証          [API キー | 認証なし]
API キー      [新しいキーを入力]   保存済み
詳細設定      コンテキスト長／自動圧縮開始トークン数
              reasoning summaries の対応（未指定／対応／非対応）
[保存して使用]

次に開始するセッションから適用されます。
```

```text
Claude Code 認証
接続方式  [OAuth | Amazon Bedrock | Anthropic 互換 API]

Anthropic 互換 API（Messages API）
Base URL      [https://llm.example.com/anthropic]
デフォルトモデル ID [接続先で利用できるモデル ID]
認証          [API キー（x-api-key） | Bearer トークン]
キー/トークン [新しい値を入力]   保存済み
詳細設定      Sonnet / Opus / Haiku 用モデル ID（任意）
[保存して使用]
```

Claude 側の初期版は認証値必須とし、「認証なし」は提供しない。URL だけを変更すると保存済み OAuth が使用され得るため、空キーでの接続やダミーキーの自動生成は行わない。API キーと Bearer の違いはヘッダーの送り方であり、OAuth トークン入力とは別項目にする。

デバイス認証とアップロードは「既存の認証」配下に残す。方式変更と接続情報の保存は一回の操作で確定する。入力中には保存せず、保存成功後に有効な方式を更新する。認証方式だけ先に変わる状態を作らない。

API キーは password 入力とし、保存済み値を取得・再表示しない。変更しなければ保持し、「キーを削除」を別操作で明示する。Codex の認証なしはユーザーが選んだ場合だけ有効。方式切替時も保存済み auth.json と API 設定は保持し、削除操作を分ける。デバイス認証完了やアップロードだけで互換 API の選択を変更しない。

「AI プロバイダ」ページの auth.json 必須に見える説明と、セッション作成・プロファイルの自動選択の説明も更新する。保存完了と実接続成功を区別し、初期版に接続確認ボタンは設けない。

## データと API

既存の `GET/PUT /settings/{name}` に `codex_connection` と `claude_connection` を追加する。更新権限は既存 Settings API の個人・チーム認可に従う。以下の最初の例と表は Codex のスキーマとする。

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

### Claude Code のスキーマと既存設定との関係

```json
{
  "claude_connection": {
    "mode": "anthropic_compatible",
    "base_url": "https://llm.example.com/anthropic",
    "model": "gateway-claude-model-id",
    "authentication": "api_key",
    "api_key": "<write-only>"
  }
}
```

| 項目 | Claude 側の契約 |
| --- | --- |
| `mode` | `oauth` / `bedrock` / `anthropic_compatible` |
| `base_url`, `model` | 互換 API 有効時は必須。GET でも返す |
| `authentication` | `api_key` / `bearer_token`。有効化時は選択必須 |
| `api_key`, `clear_api_key`, `has_api_key` | Codex と同じ保持・置換・削除・write-only 契約。Bearer の秘密値もこの欄に格納 |
| `model_aliases` | 任意の `sonnet` / `opus` / `haiku` と接続先モデル ID のマップ。未知キーは拒否 |

Claude オブジェクトの省略は変更なし、mode 以外のフィールド省略は保持する。`model_aliases` 指定時はマップ全体を置換し、null は解除。Codex 専用のコンテキスト長や reasoning summaries フィールドを受け付けない。秘密値を削除する場合は OAuth / Bedrock への切替と同時に行うか、既に互換 API が無効であることを要求する。保存・検証はどちらの接続も同一更新内で完了し、一方だけ成功する更新にはしない。

`claude_connection` 未登録時は既存 `auth_mode` と OAuth / Bedrock / 環境変数処理を維持する。登録後は `claude_connection.mode` を実行時の唯一の選択元にする。OAuth トークンと Bedrock の詳細は既存フィールドに保存し続け、互換 API の秘密値とは分離する。

後方互換用の `auth_mode` は互換 API 有効時に `anthropic_compatible` を返すよう enum を拡張する。旧クライアントが `auth_mode=oauth|bedrock` を更新した場合、登録済み `claude_connection.mode` も同じ更新で切り替える。新旧両方の mode を矛盾して指定した更新は拒否する。OAuth / Bedrock の保存済み資格情報の更新だけでは選択方式を変えない。新 UI の互換 API 選択時は既存 OAuth コンポーネントから暗黙に mode を送信しない。

Claude の Base URL は API ルートを指定する（例: `https://host/anthropic` → `/anthropic/v1/messages`）。`/v1/messages` や `/v1/messages/count_tokens` の入力は拒否し、末尾 `/v1` はルート指定を案内するエラーとする。Codex と同じ `/v1` 付きサンプルを流用しない。カスタムプレフィックスは保持し、末尾スラッシュの扱いも実際の SDK の URL 結合で確認する。

## デフォルトモデルとプロファイル上書き

接続設定の `model` はデフォルトモデルとする。セッションプロファイルに Codex / Claude Code 別のモデル入力欄を追加し、既存のプロファイル環境変数 `CODEX_MODEL` / `ANTHROPIC_MODEL` として保存する。空欄は接続設定のデフォルトを継承する。優先順位は開始要求の明示モデル環境変数 > セッションプロファイルのモデル > 接続設定のデフォルト。Codex の既存 `OPENAI_MODEL` も互換入力として扱い、同一レイヤーでは `CODEX_MODEL` を優先する。上書きしても接続先・キー・認証方式は変えず、利用不可のモデルはエラーにする。Claude の未指定別名は上書き後の実効モデルから補完し、明示別名は維持する。

## 設定解決と起動

接続は **URL・デフォルトモデル・キー・認証方式を一組として解決**した後、モデルだけをプロファイル等で上書きする。通常の環境変数マージで個人の URL とチームのキーを合成しない。

1. 既存の `credentialOwnersForRequest` に基づいて利用可能な認証元を列挙する。チームスコープで認証元未指定の場合、新しい構造化接続は当該チームの設定を使う（旧認証ファイルの配置規則は維持する）。`session_user`、`team`、`triggered_user`、`github_sender` の選択と権限を維持する。単に個人優先とする別のルールを追加しない。
2. エージェントごとに候補順で、明示された接続設定、または既存認証を採用する。Codex は `codex_connection`、Claude は `claude_connection` を扱い、Claude の既存 OAuth / Bedrock は従来の解決経路を再利用する。前の候補の有効な認証ファイル集合を採用した場合は、後の候補の互換 API キーを混ぜない。既存認証のマージ規則を互換 API の URL・キーへ流用しない。
3. 明示設定が不完全・復号不能ならエラーにする。別の所有者や auth.json へ暗黙に切り替えない。`credential_source=none` では管理された接続キーも注入しない。
4. 共通 resolver の結果をエージェント自動選択と設定生成の双方へ渡す。開始要求・プロファイル・既存 `default_agent_type` で明示されたエージェントを優先する。最終的に `auto` なら、有効な Codex 認証（互換 API または auth.json）があれば `codex-acp`、なければ `claude-acp` とする。両方の互換 API を登録した場合も Codex 優先を維持し、Claude を常用する場合は既存の既定エージェント設定で選ぶことを画面に説明する。保存順では決めない。
5. 選択されたエージェントの接続キーだけをセッションに注入する。Claude セッションへ Codex の管理キーを、Codex セッションへ Claude の管理キーを配布しない。プロキシ本体や共有ホームの設定を変更しない。Kubernetes・外部 Session Manager の両起動経路で同じ解決結果を使う。

構造化接続を利用する場合、最終環境変数マージ後に URL・認証・モデルを管理対象として確定する。モデル上書きを除き、プロファイルや開始要求からの管理対象変数の競合指定は明示エラーにし、URL だけの上書きを許可しない。通常の非接続用環境変数の優先順位は維持する。構造化接続がない旧セッションは既存の優先順位を維持する。`credential_source=none` は新しい管理接続全体を採用しないものとし、既存の明示的な環境変数設定の意味は変更しない。

### Codex の実行設定

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

### Claude Code の実行設定

Claude では専用の env_key を指定する Codex と異なり、Claude プロセスが読む標準環境変数へ起動時に変換する。保存形式は環境変数と分離し、以下を選択済み Claude セッションの環境に生成する。

| 設定 | 生成する環境変数 |
| --- | --- |
| Base URL | `ANTHROPIC_BASE_URL` |
| 主モデル | `ANTHROPIC_MODEL` |
| API キー認証 | `ANTHROPIC_API_KEY` のみ（`x-api-key`） |
| Bearer 認証 | `ANTHROPIC_AUTH_TOKEN` のみ（`Authorization: Bearer`） |
| Sonnet / Opus / Haiku の指定 | `ANTHROPIC_DEFAULT_SONNET_MODEL` / `ANTHROPIC_DEFAULT_OPUS_MODEL` / `ANTHROPIC_DEFAULT_HAIKU_MODEL` |

モデル別名は [公式モデル設定](https://code.claude.com/docs/en/model-config) に合わせる。初期版では上記三別名の未指定分を主モデルへ割り当て、補助タスクだけ接続先にない既定モデルへ向かう問題を防ぐ。この補完方針はフォームの説明と設定プレビューにも表示する。配布バージョンが追加の別名・補助モデルを使う場合も検証し、必要なマッピングを追加してから対応バージョンとする。任意モデルのコンテキスト長・thinking 対応は推測しない。

互換 API 方式では OAuth 注入と Bedrock 用 model 上書きをスキップし、非選択の認証変数、`CLAUDE_CODE_OAUTH_TOKEN`、`CLAUDE_CODE_USE_BEDROCK` / `CLAUDE_CODE_USE_VERTEX` / `CLAUDE_CODE_USE_FOUNDRY` の有効化、既存の `apiKeyHelper` と認証ヘッダーによる干渉を除去する。OAuth の `.credentials.json` は配布・同期しない。AWS 環境変数を一括削除してツール利用を壊さず、Claude のプロバイダ選択と認証に必要な範囲を処理する。保存済み OAuth / Bedrock 認証はサーバーに保持する。

OAuth / Bedrock に戻すときも互換 API の Base URL・秘密値・モデル別名が残らないよう、セッションの生成物を方式から再構築する。認証情報の空文字上書きだけに依存せず、最終プロセスの環境から非選択キーを削除できる unset 情報を SessionSettings / provisioner に渡す。コンテナ・ESM 親プロセスからの継承、生成 settings.json の env、復元ファイルからの再混入も対象にする。管理ポリシーなど上位の強制設定と競合する場合は起動エラーとして示す。

API キーの実値をリポジトリの `.claude/settings.json`、引数、プレビューへ書かない。実装時は `claude-acp` の SDK / 子プロセスと `claude-legacy` の両経路で環境変数が届くことを確認する。`x-api-key` 利用時の初回確認も含め、保存済み OAuth に頼らず非対話起動が完了することを受け入れ条件にする。モデル・接続の固定と再開の規則は Codex と共通とする。

## 実装単位と受け入れ条件

1. Settings entity / controller / repository / frontend 型に両エージェントの構造化接続と write-only キーを追加。Claude の auth_mode enum と後方互換変換、`backend/spec/openapi.json`、設定同期・セッションプロファイルの型も更新する。
2. 認証元 resolver を共通化し、KubernetesSessionManager の通常起動・外部起動、自動選択、統一 SessionSettings、compile、管理ファイル同期へ接続。Claude は settingspatch の auth 分岐と最終環境変数の削除伝播を実装する。
3. `CodexAuthSection` と `AIProvidersSection` に共通フォームを追加。`ClaudeOAuthSettings` から方式選択を分離する。`ImmediateSaveNotice` と SettingsScopeContext の保存処理は操作単位の保存に合わせ、既存の保存バーが古い値を再送しないよう成功後の基準値を更新する。
4. 以下のテストと配布ランタイムでの実接続確認を完了してリリース。

| 検証 | 期待結果 |
| --- | --- |
| 個人／チームの保存と再表示 | URL・モデル・方式を再表示し、キー実値を返さない |
| キー保持・変更・削除、不正な更新 | 明示した契約どおり動作し、失敗時は旧設定全体が残る |
| repository 暗号化と復号失敗 | 非 noop 時の保存値に実キーを含めず、復号失敗は明示エラー |
| 認証元選択・フォールバック・none | 許可された同一所有者の接続を採用し、異なる所有者のキーを混ぜない |
| auth.json なし＋Codex 互換 API＋auto | Codex ACP を選択し、ログインを要求しない |
| Codex 認証なし＋Claude 互換 API＋auto | Claude ACP を選択し、保存した接続を使う |
| auth.json へ戻す／別エージェント明示 | 旧環境変数による意図しない切替や Codex キー注入がない |
| TOML 合成・再生成 | パース可能、provider 重複なし、既存フック保持、秘密値なし |
| 旧設定のみ | 従来の環境変数設定・auth.json 起動が維持される |
| 起動・再開・外部 Session Manager | 選択した接続と方式を維持し、稼働中に勝手に切り替わらない |
| Responses 接続の実機確認 | ストリーミング応答とツール実行・結果返却が成功する |
| Claude の API キー／Bearer 認証 | それぞれ正しいヘッダーだけを送り、OAuth なしで ACP / legacy が非対話起動する |
| Messages 接続の実機確認 | SSE、tool_use / tool_result、トークン計数の対応あり／なしを確認する |
| Claude OAuth・Bedrock・互換 API の相互切替 | model が誤って削除されず、非選択の認証・URL・provider フラグが最終プロセスに残らない |
| 主モデルと別名・補助タスク | 未指定別名の主モデルへの補完と個別指定が機能し、未対応モデルへの意図しない要求がない |
| Codex と Claude の両接続登録 | auto は Codex、明示 Claude は Claude となり、他方の管理キーを配布しない |
| デフォルトモデル・プロファイル・開始要求 | 両エージェントで指定優先順位を守り、空欄で継承し、URL・キーは変わらない |
| プロファイル・開始要求・親環境の競合 | 接続の部分上書きを拒否し、古い認証が再注入されない |
| Claude 新旧 API フィールド | auth_mode との同期と矛盾検出が機能し、既存 OAuth / Bedrock 利用者を維持する |
| 401、404、到達不能、非対応モデル | 秘密情報を伏せて原因を表示し、別接続へ切り替えない |

本 PR に設定 API、接続解決、起動設定の生成、復元時の接続検証と両フォームの操作テストを追加した。Fly 開発環境にデプロイし、Codex / Claude ACP と Ollama Cloud の実推論・モデル上書き・ツール実行を確認した。検証範囲と制限は [実機検証記録](ollama-fly-dev-verification.md) を参照。

# セッション設定全体の再読み込み・エージェント再起動

Status: proposed（設計のみ。API 名・フィールドは追加提案）

## 1. 方針

**起動時と同じ設定生成処理で最新の `SessionSettings` 一式を作り直し、実行側に再送して
エージェントを再起動する。** 認証情報の切り替えも、この設定再読み込みの一部として扱う。

同じセッション ID、会話履歴、作業ディレクトリを保持する。通常の操作は
「設定を再読み込みして再起動」の一つとする。「停止 → 設定変更 → 再開」も同じ処理を使い、
停止中に既存の個人・チーム設定やセッションプロファイル、認証情報を編集できるようにする。
再開時にそれらの最新設定を解決して送る。

認証専用の設定モデル、credential ID の新設、認証だけの patch／preview API は追加しない。
起動と再起動で別々のマージ規則を持たず、同じ resolver と `SessionSettings` schema を使う。
再送するのは過去の settings のコピーではなく、最新の設定元から生成した一式である。

## 2. 読み込む設定と保持する状態

| 区分 | 再読み込み時の扱い |
| --- | --- |
| 個人・チーム設定、選択中の session profile | 最新版から、起動時と同じ優先順位で全体を再生成 |
| 認証情報、接続先、モデル、agent 設定、環境変数 | 最新版を適用。会話復元の互換性は停止前に検証 |
| MCP、skills／plugins、managed files、GitHub 設定等 | 起動時の設定生成・適用対象を同じように再読み込み |
| sandbox、Docker／registry 等の workload 設定 | 全体生成の対象。プロセスだけでは反映できない変更は manager が workload を再作成 |
| session ID、owner、scope、team、作業ディレクトリ、既存会話 ID | 保持。設定の再読み込みによって別セッションへ変えない |
| repository URL、branch 等の初期配置指示 | 既存 workdir を再 clone／checkout／reset しない。変更要求は停止前に拒否 |
| initial message、webhook 起動イベント、初回セットアップ | 再送 payload に含まれても再実行しない |
| 内部 runtime/control token、世代番号、manager の識別情報 | 制御側が生成・管理。ユーザー編集から上書きさせない |

「設定全体を生成すること」と「新規セッション作成の副作用を繰り返すこと」は分離する。
設定を再生成する処理と、再開モードで設定を適用する処理を共通部品として切り出す。

既存の `persistModelConnectionIdentity` は会話復元時に接続先・モデル等の一致を要求する。
この制約に違反する変更は停止前に理由を返す。設定生成対象を認証だけに限定する理由にはしない。
agent 種別を変えて別形式の会話を読み込むことや、復元失敗時に空の会話で成功扱いすることはしない。
互換性のない変更を受けたら設定全体の適用を拒否し、互換な項目だけを黙って部分適用しない。

## 3. 既存実装の利用

- `SessionSettings` が起動設定全体の共通形式として既に存在する。
- 外部 manager の `ResumeSession` は settings body を受け取り、`PrepareSessionResume` で保存できる。
- Kubernetes manager は `settings.yaml` を再開用 Secret に保存する。
- 親の `remoteResumeSettings` は allocation の `ProvisionSettings` を読み込む。
  現在は主に自動停止ポリシー等を更新するため、明示的な再読み込みでは共通 resolver の出力へ置き換える。
- provisioner は設定適用と agent 子プロセス起動を行う。ただし現在の `runProvision` は初回起動向けで、
  agent の終了をエラーとして扱うため、そのまま二度呼ぶ実装にはしない。
- 既存の suspend／checkpoint／resume を workload 再作成と手動停止に再利用する。

参照:
[settings schema](../backend/pkg/sessionsettings/types.go)、
[session controller](../backend/internal/interfaces/controllers/session_controller.go)、
[Kubernetes manager](../backend/internal/infrastructure/services/kubernetes_session_manager.go)、
[manager handlers](../backend/internal/modules/sessionmanager/handlers.go)、
[provisioner](../backend/pkg/provisioner/provision.go)、
[connection identity](../backend/pkg/provisioner/model_connections.go)。

## 4. 設定生成と保存

親 proxy が起動時の設定生成を共通化し、`ResolveSessionSettings(startInput, mode)` として呼ぶ。
`mode` は `create`／`restart`。設定の解決順序は同一とし、restart では session の識別情報と
既存 workdir を保持する。

再生成に必要な**起動入力**をセッションに保存する。解決済み settings から元の入力を逆算しない。

- 起動時に指定した設定の参照元と profile ID
- 明示指定した起動オプション・session 固有の overrides
- owner、scope、team、認可済みの認証参照元に関する起動コンテキスト

profile は作成時に実際に選ばれた ID を保存し、その最新版を読む。既定 profile の変更で既存 session の
参照先を勝手に変更しない。別 profile を使う場合はユーザーが明示指定する。
作成時の明示 override は引き続き優先する。最新の profile 値を使いたい項目は override を削除する。
個人・チーム共通の設定画面での編集は、通常どおり他の利用者／セッションにも影響する設定変更である。

再起動要求には必要に応じて profile 選択と起動時と同じ公開オプションを渡せるようにする。
内部 `SessionSettings` をブラウザに返して秘密ごと送り返させる必要はない。
親が認可して解決した **完全な `SessionSettings`** を manager／provisioner に送る。
設定の取得失敗や参照の削除・権限喪失はエラーとし、別の設定元に暗黙 fallback しない。

既存の allocation／セッション保存領域に、起動入力、settings の revision、適用済み revision、
再起動 phase、手動停止フラグを追加する。`ProvisionSettings` と再開 Secret に同じ revision の
完全な settings を保存し、後日の resume や Pod 再起動でも古い設定へ戻らないようにする。
秘密を含む保存は既存の保護された保存経路を使い、公開 status やログには全文を出さない。
既存 session の起動入力を復元できない場合は再読み込み不可と表示し、明示的な起動入力の指定で補完する。

## 5. API と UI

公開 API の追加は `POST /sessions/:id/restart` を中心にする。

```json
{
  "reload_settings": true,
  "busy_policy": "wait"
}
```

`reload_settings` は既定で true。false は保存済み settings で再起動する場合に使用する。
必要な場合のみ、起動 API と共通の公開設定オプションを追加で指定する。
同じ要求の再送を識別する request ID と期待 settings revision を受け取り、重複実行や古い画面からの
更新を防ぐ。受理後は `202` を返し、既存の session status／SSE で phase と結果を追跡する。

手動停止は既存 suspend 経路に「明示再開まで停止を維持する」指定を追加する。
手動停止からの resume に `reload_settings: true` を指定した場合も restart と同じ処理を使う。
通常の自動 suspend／透過 resume は保存済み settings を使用し、ページを開いただけで設定全体が変わらない。

UI は「設定を再読み込みして再起動」「停止」「設定を再読み込みして再開」を提供する。
設定・認証の編集は既存画面を利用し、session 固有の profile／override は起動画面の入力部品を再利用する。
設定元と明示 override が分かる表示を行う。実行中は完了待ちを既定とし、中断して再起動する選択肢を設ける。
停止中・再起動中は prompt 送信と透過 resume を止め、タブを閉じてもサーバー側で進捗を保持する。

## 6. 再起動の流れ

```text
最新設定を生成・検証 → 入力停止 → turn 終了／中断 → 状態保存・旧 agent 停止
    → settings 一式を保存・再送 → 設定再適用 → agent 起動 → 同じ会話を復元
```

1. session の操作権限と設定元の利用権限を検証し、設定全体を生成する。参照元は保存された起動入力で
   決定し、操作した閲覧者の個人設定に置き換えない。互換性と必要な再起動方式を停止前に判定する。
2. セッション単位の lifecycle lock と永続 phase を取得し、再起動・削除・自動 resume を直列化する。
   新規入力を止め、進行中の turn は待つか明示的に cancel する。待機期限切れで勝手に強制停止しない。
3. 会話と workspace の状態を保存する。旧 agent、MCP 等の子プロセス、認証同期や付随 goroutine を
   停止して終了を待つ。agent 停止は checkpoint の確定に必要な flush を行ってから完了させる。
4. 遅延した旧認証同期の書き込みを失効した token／世代で拒否する。同期停止後に最新認証を再取得して
   settings を最終確定する。この時点の入力バージョンに変更があれば設定全体を再生成・再検証する。
5. 完全な settings を親の保存領域と manager の再開設定へ保存し、同じ revision の反映を確認する。
   再送は revision と request ID で冪等に扱い、部分保存状態から起動しない。
6. 新しい設定を適用して agent を起動する。再開モードでは初回 prompt、webhook、cycle の自動実行や
   repository 初期化を繰り返さない。startup script は初回専用と、再起動時に実行可能な処理を分離する。
7. 新世代での会話復元成功を確認して適用済み revision を更新し、入力を再開する。ready は外部 provider の
   最初の推論成功までを保証しない。認証失敗と会話復元失敗は個別に表示する。

### エージェントプロセスの再起動

provisioner 自身とその制御接続を残し、agent 実行用の context／process group を別に管理する。
`StopAgent → ApplySettings → StartAgent` を初回起動と共有し、意図的な停止を異常終了として扱わない。
agent に付随する同期・MCP 等は世代ごとに管理し、再起動のたびに多重起動しない。

workload 構成が変わる場合は manager が既存 suspend／prepare／resume 経路で workload を再作成する。
この場合も送る settings と公開 API は同じ。永続化や再作成に対応しない manager では、必要な方式を
停止前に判定して未対応を返す。全設定を受け取ってプロセスだけ再起動し、workload 設定を未適用のまま
成功と返すことはしない。

### 完全な置換の意味

生成済み設定は古い出力への単純な追記ではなく、新しい出力で置き換える。削除された env、MCP、
plugin、認証ファイルも実行環境に残さない。管理対象のファイル・設定キーを記録して cleanup し、
ユーザーの作業ファイルや会話データを削除しない。既存の `UnsetEnv`／`RemoveFiles` と cleanup を
拡張する。snapshot／PVC の復元は新設定適用より前に行い、旧認証を最後に復元してしまわないようにする。

## 7. 障害・認証同期

- 停止前の検証失敗や checkpoint 失敗では設定を適用せず、利用可能な旧 agent を維持する。
- 停止後の保存・起動失敗では停止を維持し、設定画面で修正して再読み込みを再試行できる。
  旧認証／旧設定へ自動で戻さない。
- proxy や manager の再起動後は保存された phase と revision を確認して再調整する。
  旧 agent の停止が不明なら新 agent を二重起動しない。
- 手動停止・失敗停止の間は通常アクセスで再開しない。既存 status／SSE に理由を含める。
- 認証の同期先は起動時の解決結果に一致させる。現状の `ManagedFilesController.Save` は
  `session.UserID()` に保存するため、チーム等の設定元から取得した場合の同期先を修正する。
  認証参照元の規則は起動と再起動で共通化し、再起動専用の認証モデルを作らない。
- 同じ保存先を使う他 session の認証更新と競合した場合、保存バージョンの条件付き更新で
  古い OAuth token の上書きを防ぐ。これは通常起動も含む同期処理の修正として扱う。

## 8. 実装順序と受け入れ条件

1. 起動入力の保存と settings resolver の共通化。通常起動との生成結果一致を確認する。
2. provisioner の agent lifecycle と設定適用を再実行可能にする。削除された設定も反映する。
3. manager／親の restart、手動停止、revision／phase の保存を追加し、必要時は workload 再作成へ渡す。
4. UI／CLI を接続する。旧 manager は capability により停止前に未対応を返す。

検証する内容:

- 認証だけでなく env、MCP、plugins、managed files 等の変更・削除が再起動後に反映される。
- 同じ起動入力から新規起動と再読み込みで同じ設定を生成する（固定 identity／再開モードの差分を除く）。
- profile 更新と session override の優先順位、参照削除、認可、起動入力の移行を確認する。
- 会話、session ID、未コミット変更を保持し、初回 prompt や repository 初期化を繰り返さない。
- プロセス再起動と workload 再作成の双方で設定が反映され、後日の resume でも古い設定に戻らない。
- 起動途中の障害、設定保存の部分失敗、二重要求、proxy 再起動から復旧できる。
- 旧プロセスや同期 goroutine が残らず、古い認証の遅延書き戻しを拒否できる。
- 手動停止中のページ再読込で起動せず、明示再開では最新設定を読み込める。
- 接続先／agent 種別などの非互換変更と、manager が対応しない workload 変更を停止前に拒否する。

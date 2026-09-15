# セッション途中の認証情報切り替え・設定変更・再開

Status: proposed（設計のみ。API 名・フィールドは追加提案）

## 1. 方針

同じセッション ID、会話履歴、作業ディレクトリを保持したまま、エージェントを停止し、
session settings を変更して再開できるようにする。操作は以下の二通りを提供する。

- **変更して再起動**: 新しい認証情報を選択 → 検証 → 停止 → 設定適用 → 再開。
- **停止して編集**: 停止 → 設定を編集・保存 → 任意のタイミングで再開。

両者は同じサーバー側の永続操作として実装する。ブラウザから suspend、設定更新、resume を
順番に呼ぶだけにはしない。ブラウザ切断や proxy 再起動でも処理を追跡・再開できる必要がある。
再開は会話を読み込んで入力待ちになることを指し、実行途中のツールや直前の prompt の再送はしない。

初期実装は、同じ agent・接続先・モデル・認証方式での認証情報の更新／参照元切り替えに対応する。
接続先、agent 種別、モデル、認証方式の変更は既存の会話復元制約があるため別段階とする。
GitHub、MCP、registry、ESM 自体の接続トークンの切り替えは初期対象外。

## 2. 現状の実装と不足点

| 現状 | 設計への影響 |
| --- | --- |
| Kubernetes の `SuspendSession` は canonical resource を残して workload を停止する | 停止・再開の基盤として再利用する |
| `PrepareSessionResume` は再開用 Secret の `settings.yaml` を更新する | revision と停止確認を追加して適用先に使う |
| 外部 manager の resume は settings body を受け取る | 親で確定した settings を送る。manager が独自再解決しない |
| 親の `remoteResumeSettings` は allocation の `ProvisionSettings` を読み、自動停止ポリシー等を更新する | Secret だけの編集では後の resume で古い認証へ戻る。親の保存内容も更新する |
| runtime route へのアクセスで suspended session が自動再開する | 手動停止・編集用の durable hold が必要 |
| `credentialOwnersForRequest` は `CredentialSource`、`SettingsTeamID` 等から参照元を解決する | 初回解決結果を明示的に保存し、閲覧者によって認証元を変えない |
| provisioner は managed files を復元し、10 秒間隔で変更を同期する | 古い runtime の書き戻しを遮断し、書き戻し先を選択した認証元と一致させる |
| `ManagedFilesController.Save` は `session.UserID()` 宛てに保存する | チーム認証等の切り替えではそのまま使えない |
| `persistModelConnectionIdentity` は復元時に接続・モデルの一致を要求する | 初期版はこの検証を維持する |

主要な参照箇所:

- [session controller](../backend/internal/interfaces/controllers/session_controller.go)
- [Kubernetes manager](../backend/internal/infrastructure/services/kubernetes_session_manager.go)
- [manager handlers](../backend/internal/modules/sessionmanager/handlers.go)
- [settings schema](../backend/pkg/sessionsettings/types.go)
- [provisioner](../backend/pkg/provisioner/provision.go)
- [connection identity](../backend/pkg/provisioner/model_connections.go)
- [managed files controller](../backend/internal/interfaces/controllers/managed_files_controller.go)

## 3. データモデルと責務

親 proxy に session ごとの設定レコードと操作レコードを持たせる。local manager の場合も同じ
repository interface を使う。Kubernetes Secret と runtime 内の設定ファイルは実行用の複製とする。

### SessionConfiguration

- `revision`: 編集可能な設定の単調増加番号。更新は期待 revision による CAS（条件付き更新）。
- `active_revision`: 最後に会話復元まで成功した revision。
- `desired_revision`: 次回起動に適用する revision。
- `resume_hold`: `none | manual_edit | reconfigure | recovery`。一覧アクセス、透過 resume、
  auto-suspend reconciler、通常の明示 resume は hold を解除できない。
- `credential_binding`: agent 用認証の明示的参照（種別、user/team scope、owner ID、保存先の
  credential ID、取得時の version）。認証値は公開レスポンスに含めない。
- `overrides`: 許可された session 固有の変更。個人／チームの既定設定を書き換えない。
- `compiled_settings_ref`: 実行用 settings の暗号化保存先。操作中は immutable revision として保持。
- `runtime_generation`: 起動ごとの番号。設定 revision とは別で、同じ設定による再起動でも増加する。

既存の credential repository に ID/version がない箇所には opaque version と参照解決を追加する。
単なる owner の変更として実装せず、session owner、認証の参照元、同期の書き戻し先を分離する。
解決時に通常設定全体を再マージせず、既存の session snapshot に指定された認証の変更だけを反映する。

### SessionOperation

`id`, `session_id`, `actor`, `idempotency_key`, `request_hash`, `base_revision`,
`target_revision`, `phase`, `runtime_generation`, `checkpoint_ref`, `error_code`, timestamps を保存する。
秘密を含む compiled settings は別の保護された保存領域を参照する。

セッション単位に一つの lifecycle operation だけを実行する。複数 proxy replica では永続 lease と
CAS を利用し、古い worker の実行は generation で拒否する。HTTP 接続終了で操作を cancel しない。

## 4. API 案

すべて認可後に親 proxy が処理する。ここでの settings は公開編集用 DTO であり、内部
`SessionSettings` 全体や raw YAML を直接受け入れない。

| API | 意味 |
| --- | --- |
| `GET /sessions/:id/settings` | 秘密を除いた現在値、revision、hold、capabilities、選択可能な認証参照を返す。起動しない |
| `POST /sessions/:id/reconfiguration-preview` | 同じ validation を実行し、非秘密の差分、互換性、再起動要否を返す。設定は保存しない |
| `POST /sessions/:id/pause` | checkpoint と停止を行い、`manual_edit` hold を保持する |
| `PATCH /sessions/:id/settings` | 停止確認済みの manual_edit／recovery hold 中に限り desired settings を保存する |
| `POST /sessions/:id/restart` | patch を伴う一括操作、または保存済み revision による再起動／再開 |
| `GET /sessions/:id/operations/:operationId` | 進捗・結果・復旧方法を返す。起動しない |

PATCH / pause / restart は `If-Match` に設定 revision、操作 API は `Idempotency-Key` を必須とする。
同一 key・同一 body は同じ操作を返し、同一 key・異なる body は `409`。
pause/restart は永続受理後に `202` と operation ID / Location を返す。PATCH は `200` と新 revision。
preview の結果は認可を予約するものではなく、保存時・起動直前にも再検証する。

restart request 例（binding の ID は表示用ではなく server が認可可能な参照）:

```json
{
  "settings_patch": {
    "credential_binding": {
      "scope": "team",
      "owner_id": "myorg/platform",
      "credential_id": "cred-example"
    }
  },
  "busy_policy": "wait"
}
```

保存済み設定で再開するときは `settings_patch` を省略する。変更なしの restart は同じ認証 binding
から最新版を再取得できるが、別 owner への fallback はしない。`busy_policy` は `wait`（既定）と
`interrupt`。後者は現在の turn を中断する明示操作で、ツールの副作用を取り消すものではない。

主なエラー: `409 operation_in_progress / session_not_paused / credential_version_changed`、
`412 settings_revision_mismatch`、`422 incompatible_session_settings / credential_unavailable`、
`501 reconfigure_not_supported`、`503 session_manager_unavailable`。
hold 中の runtime 呼び出しには `423 session_paused` と operation ID を返し、prompt を送信しない。
閲覧用共有リンクには設定編集・認証一覧・操作状況の機密メタデータを公開しない。

## 5. 停止・設定反映・再開プロトコル

```text
active → validating → waiting_idle → quiescing → checkpointing → stopping
                                                                  ↓
                                                        paused（manual_edit）
                                                                  ↓
                                            applying → resuming → ready
                                                └──── failure → paused（recovery）
```

一括 restart では paused から自動的に applying へ進む。pause ではそこで操作を完了する。
既に自動 suspend 中なら hold を取得し、保存済み状態の完全性を確認して applying に進める。
公開 session status は既存の `suspended/resuming` を活用し、細かい phase は operation に分離する。

1. **検証・予約**: session の編集権限、認証利用権限、復元対応、変更互換性を確認する。
   operation と hold を原子的に記録する。認証値はサーバーで取得し、履歴に残さない。
2. **入力停止**: prompt と新規 tool 実行の入口を閉じる。wait は進行中 turn の終了を待ち、
   interrupt は既存の cancel 経路で中断する。待機には期限を設け、期限切れで強制 kill はしない。
3. **静止化・保存**: agent の書き込みを止め、認証同期を停止して処理中の書き戻しを drain する。
   会話・未コミット変更を含む workspace を checkpoint し、復元可能な snapshot を確認する。
   pause 中に動き続ける子プロセスも manager 管理の process group / workload 単位で停止する。
4. **停止確認**: workload を停止し、旧プロセスの終了を確認する。旧世代の認証同期・status 更新を
   拒否する。停止が不明なまま別 runtime を作らない。
5. **設定適用**: 選択した credential version を再確認し、変化していたら再検証する。
   desired revision を確定して allocation `ProvisionSettings` と再開 Secret に複製する。
   manager が同じ revision の保存完了を応答するまで起動しない。
6. **再作成**: runtime generation と内部 runtime/control token を更新して起動する。
   workspace／会話を復元後、新認証ファイルを atomic write し、`UnsetEnv` / `RemoveFiles` /
   接続設定の cleanup を使って旧認証と衝突する値を除去する。初回 message と cycle の自動実行は抑止する。
7. **完了**: 起動した generation・revision と ACP 会話復元成功を確認し、active revision を更新、
   hold を解除する。SSE で通知し、UI は同じ会話へ再接続する。

ready はプロセス・会話の利用準備完了であり、外部 provider の課金枠や最初の推論成功まで保証しない。
会話復元に失敗した場合、新規会話への暗黙 fallback は禁止する。

## 6. 認証同期と認可

- session の編集権限と credential の利用権限を別に検証する。共有閲覧権限だけでは切り替え不可。
- 個人認証は本人の明示選択に限定し、他人の user ID を指定して利用することはできない。
  初期版の team session は当該 team の認証に限定し、メンバーの個人認証を持ち込む導線は設けない。
- `credential_binding` を使用する同期 API に generation、binding version、credential version の
  条件付き更新を追加する。保存先は親の binding から決定し、runtime が owner を指定できないようにする。
- OAuth refresh による更新は使用中の binding にのみ書き戻す。別 session が更新済みなら競合を返し、
  古い値で上書きしない。同期対象外の API key や接続設定は書き戻さない。
- generation を持たない旧同期経路にも失効可能な control token による遮断を追加する。
  runtime SSE の generation 検証だけでは managed files の旧書き込みを止められない。
- checkpoint の認証ファイル除外を確認・テストする。過去 snapshot や PVC に残る値も復元後 cleanup
  の対象とする。Secret、旧 compiled revision、バックアップの保持期限を設け、操作ログには値を記録しない。

## 7. 障害時と競合

| 障害／競合 | 処理 |
| --- | --- |
| 検証失敗、busy timeout、停止前の checkpoint 失敗 | 設定は変更しない。旧 runtime が利用可能と確認できれば入力制限と hold を解除する |
| 停止タイムアウト、manager 切断、旧 runtime 生存が不明 | recovery hold のまま観測・再試行。二重起動しない |
| 停止後の設定保存失敗 | 停止維持。同じ operation で未完了段階から再試行 |
| 新認証／会話復元／起動失敗 | recovery hold で失敗を表示。新 runtime を停止確認後に修正・再試行を許可 |
| proxy／worker 再起動 | 永続 operation と manager の観測 revision/generation から再調整する |
| Secret は更新済み、allocation の複製は未更新 | canonical desired revision から複製を修復し、一致するまで起動禁止 |
| 別タブの編集、自動 resume、通常 resume | CAS／hold により競合を返す。SSE の古い世代も無視する |
| セッション削除 | lifecycle lock の下で operation を終了させ、停止を確認して削除。worker の再作成を禁止する |

旧認証への自動 rollback はしない。ユーザーが切り替えた認証を勝手に使い直さないためである。
「前の設定で再開」は旧参照の現在の利用権限と有効性を再検証する新しい restart operation として提供する。
停止前に失敗した場合と、停止後に失敗した場合を UI と API で明確に区別する。

## 8. UI

session メニューに「設定・認証情報」と「停止して編集」を追加する。設定画面には現在の認証元、
変更候補、保存済み／適用済みの差分を表示する。秘密値は表示・編集せず、登録済み credential を選ぶ。
新規登録が必要なら既存の認証管理画面を利用する。

通常は「変更して再起動」、手動停止中は「設定を保存」「この設定で再開」を表示する。
実行中なら既定で完了を待ち、「現在の処理を中断して切り替える」を明示選択できるようにする。
処理中は operation phase を表示して入力を止める。タブを閉じても次回表示時に operation を取得する。
manual_edit／recovery hold 中はチャットの bootstrap API を呼んで勝手に起動しない。

## 9. 対応範囲と実装順序

1. 親の configuration / operation 保存、認可、revision、hold、世代制御を追加する。
2. 認証 resolver と同期書き戻し先の修正、旧 token の失効を実装する。
3. Kubernetes manager に revision 付き prepare／停止確認／再開契約を実装する。
4. 外部 manager に capability と generation/revision 応答を追加する。旧 manager は `501` を返す。
5. API、UI、CLI に pause／settings／restart／operation status を追加する。
6. 接続先や認証方式の変更は別途互換性テスト後に追加する。既存 identity 検証を単純に無効化しない。

初期対応は永続 workspace と会話 checkpoint が利用できる Kubernetes local / external manager。
native manager や ephemeral session は capability 不足として停止前に拒否する。native 対応には
process group の停止・再生成、永続 workdir、同等の checkpoint と設定適用契約が必要。

既存 session は保存済み settings と作成時メタデータから revision 1 を生成する。credential の参照元を
一意に復元できない場合は `unknown` とし、ユーザーに参照元の選択を求める。推測で個人・チーム認証を選ばない。

## 10. 検証・受け入れ条件

- 同一 provider で個人／team の認証更新を行い、session ID、会話、未コミットファイルが保持される。
- 「停止→編集→保存→再開」と一括 restart が同じ最終状態になる。
- pause 後のページ再読込、履歴取得、status polling、自動 resume で起動しない。
- busy wait／interrupt、checkpoint 失敗、manager 切断、proxy 再起動、各保存段階の失敗を注入して復旧できる。
- 旧世代の token と遅延同期が、新認証や別 owner の credential を上書きできない。
- runtime 環境変数、認証ファイル、restore snapshot、restart Secret、allocation に古い有効値が混在しない。
- 同時操作、同じ Idempotency-Key の再送、削除競合でも runtime が二重起動しない。
- 新規 provider 呼び出しの認証失敗を実際に検証し、ready と provider 認証成功を混同しない。
- 復元時に初回 prompt や cycle を再実行せず、会話復元失敗を空の新規会話として成功扱いしない。
- 認可、参照元不明の移行、旧 manager、native 非対応、接続先変更拒否を controller／統合テストで検証する。
- UI E2E で操作中のタブ切断・再接続、失敗後の編集・再試行、手動停止からの再開を確認する。

metrics は操作数、phase 所要時間、失敗理由、stale generation 拒否数を記録する。
監査記録には actor、session、認証参照 ID、設定 revision、結果を含め、認証値や settings 全文を含めない。

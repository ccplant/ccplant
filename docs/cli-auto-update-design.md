# ccplant CLI 自動アップデート設計

## 目的

GitHub Release の standalone `ccplant` バイナリを利用しているユーザーが、手動で
tarball を取得し直さなくても安定版へ更新できるようにする。通常の CLI 実行を妨げず、
コンテナ、Kubernetes セッション、Tauri sidecar、パッケージマネージャー管理下の
バイナリを誤って書き換えないことを優先する。

初版は release workflow が現在生成している次のプラットフォームを対象とする。

- Linux amd64 (`ccplant_Linux_x86_64.tar.gz`)
- Linux arm64 (`ccplant_Linux_arm64.tar.gz`)
- macOS arm64 (`ccplant_Darwin_arm64.tar.gz`)

Windows、macOS amd64、開発ビルド、prerelease は、対応する release asset がないため
対象外とする。

## 現状と制約

- `internal/buildinfo.Version` は release build では `vX.Y.Z`、通常ビルドでは `dev` が
  linker flag で設定される。
- release workflow は上記 tarball と `checksums.txt` を GitHub Release に公開する。
- Native External Session Manager の upgrade は既に release asset をダウンロードし、
  SHA-256 を検証して実行ファイルを atomic replace する。ただし daemon の drain、再起動を
  含む管理操作なので、ユーザー CLI の自動更新とは入口を分ける必要がある。
- Kubernetes セッション内の CLI は initContainer が `/opt/ccplant/bin` へコピーし、
  main container には read-only mount する。更新元は session manager の release image
  であり、Pod 内での自己更新はしてはいけない。
- Docker image、Tauri sidecar、Homebrew/Nix 等の将来の配布経路も、それぞれの配布主体が
  ライフサイクルを管理する。`os.Executable()` が書き込み可能という理由だけで自動更新対象に
  すると、その管理を壊す。
- CLI は hook や自動処理から頻繁に呼ばれる。更新確認のネットワーク障害、GitHub の停止、
  書き込み権限不足を本来のコマンド失敗へ波及させてはいけない。

## 方針

### 配布種別をビルド時に埋め込む

`internal/buildinfo` に `Distribution` を追加し、次の値を linker flag で埋め込む。

| 値 | 対象 | 自動更新 |
| --- | --- | --- |
| `github-release` | release workflow の standalone tarball | 対象 |
| `container` | Docker image 内のバイナリ | 対象外 |
| `tauri-sidecar` | Native app 同梱バイナリ | 対象外 |
| `dev` | linker flag のないローカルビルド、CI build | 対象外 |

未知の値は安全側に倒して対象外にする。これによりパス、UID、環境変数などの不安定な
ヒューリスティックへ依存しない。`ccplant version --json` では version、distribution、
platform を確認できるようにする。

### 明示操作と自動操作

トップレベルに `update` コマンドを追加する。

```text
ccplant update check          # 最新版と現在版を表示。変更しない
ccplant update apply          # 直ちに確認・更新する
ccplant update enable         # 通常実行時の自動更新を有効化
ccplant update disable        # 自動更新を無効化
ccplant update status --json  # 設定、最終確認、保留中エラーを表示
```

`github-release` 配布では自動更新を既定で有効にする。それ以外では `apply` も配布主体を
案内して終了し、`--force` での上書きは初版では提供しない。明示的な `check` / `apply` は
確認間隔を無視する。

通常実行時は root command の実行前に確認期限だけをローカルで判定する。期限が来ていれば
更新を試み、成功した場合も起動済みプロセスはそのまま現在版でコマンドを完了する。
atomic replace 済みの新版は**次回起動から**使う。この方式なら argv の再実行や、途中での
プロセス置換を必要としない。

自動更新の失敗は exit code に影響させない。TTY の stderr に限り、同じ失敗について一度だけ
短い警告を出す。JSON 出力、pipe、hook からの実行では出力しない。`update apply` の失敗だけは
診断を stderr に出し、非ゼロ終了する。

## 更新フロー

```text
CLI 起動
  └─ distribution / config / interval を確認
      ├─ 対象外または期限前: 通常コマンドへ
      └─ 期限到来: process lock を取得
          ├─ 別プロセスが確認中: 通常コマンドへ
          └─ signed manifest を取得・検証
              ├─ current >= latest: checked_at を保存
              └─ newer release
                  ├─ platform asset を一時ファイルへ取得
                  ├─ size・SHA-256・archive 内容を検証
                  ├─ 同じ directory に mode 0755 で一時配置
                  └─ rename で実行ファイルを atomic replace
```

### バージョン比較と release 選択

- stable channel のみを初版で提供し、`vMAJOR.MINOR.PATCH` の release だけを選ぶ。
- 現在版が semver でない、prerelease である、または最新版より新しい場合は自動更新しない。
- downgrade はしない。明示的なバージョン指定や channel 切り替えは後続機能とする。
- GitHub の `releases/latest` API ではなく、固定 URL の小さな update manifest を使う。
  API rate limit と release JSON の仕様への依存を避けるためである。

### release manifest と検証

release workflow は asset の upload 完了後に、次の情報を持つ canonical JSON
`ccplant-update-manifest.json` と、その Ed25519 signature
`ccplant-update-manifest.json.sig` を生成する。

```json
{
  "schema_version": 1,
  "version": "v1.2.3",
  "published_at": "2026-10-13T00:00:00Z",
  "assets": {
    "linux/amd64": {"name": "ccplant_Linux_x86_64.tar.gz", "sha256": "...", "size": 123},
    "linux/arm64": {"name": "ccplant_Linux_arm64.tar.gz", "sha256": "...", "size": 123},
    "darwin/arm64": {"name": "ccplant_Darwin_arm64.tar.gz", "sha256": "...", "size": 123}
  }
}
```

CLI には検証用公開鍵と許可する GitHub repository を埋め込む。manifest の署名、version、
asset 名、size 上限、SHA-256 をすべて検証し、redirect 後も HTTPS と許可 host を要求する。
tar は regular file の basename が厳密に `ccplant` であるエントリを 1 個だけ許可し、
展開サイズを制限する。manifest と asset のどちらか一方でも検証できなければ置換しない。

既存の `checksums.txt` は手動検証用として維持するが、同じ release の未署名ファイルだけを
信頼の起点にすると asset と checksum を同時に差し替えられるため、自動更新では署名済み
manifest を必須にする。署名秘密鍵は GitHub Actions environment secret として release job
だけに公開し、通常 CI や pull request から参照できないようにする。鍵のローテーション時は
1 release の間だけ旧新 2 公開鍵をクライアントに持たせる。

manifest は各 version の Release asset に置くほか、release 完了時に GitHub Pages 等の
固定 stable URL へ同一バイト列を publish する。固定 URL の publish は全 binary asset の
upload と release 公開が成功した後に行い、途中の release をクライアントへ通知しない。

## ローカル状態と設定

状態は OS の user config directory 配下（例: Linux の
`$XDG_CONFIG_HOME/ccplant/update.json`、macOS の User Config Dir）に保存する。

```json
{
  "schema_version": 1,
  "enabled": true,
  "interval_hours": 24,
  "last_checked_at": "2026-10-13T00:00:00Z",
  "last_installed_version": "v1.2.3",
  "last_error_at": null,
  "last_error_code": ""
}
```

- 初回は最大 60 分の deterministic jitter を machine/user ごとに加え、release 直後の集中を
  避ける。以後の確認間隔は 24 時間とする。
- 自動確認の HTTP timeout は全体 3 秒、manifest は 1 MiB、archive は 256 MiB を上限とする。
- state と lock file は mode 0600、親 directory は 0700 で作る。state は一時ファイルから
  atomic rename し、壊れていれば退避せず既定値として再生成する。
- process lock は待たずに取得し、取れなければ今回の確認を skip する。
- `CCPLANT_AUTO_UPDATE=0` は一時的に無効化し、`CCPLANT_AUTO_UPDATE=1` は保存設定より優先する。
  CI、air-gapped 環境、緊急停止に使えるよう `0` を文書化する。
- `CCPLANT_UPDATE_MANIFEST_URL` の上書きはテスト build (`dev`) だけで許可し、production
  binary の供給元を環境変数で変更できないようにする。

設定に秘密情報は含めない。GitHub token は利用せず、公開 release だけを対象とする。

## 実行ファイルの置換

1. `os.Executable()` と `EvalSymlinks` で実体を解決する。
2. 実体と同じ directory に `O_EXCL` で一時ファイルを作る。
3. 検証済み binary を書き、`Sync`、`Chmod(0755)` を行う。
4. ダウンロードした binary の `--version` を別 process で実行し、manifest の version と一致する
   ことを短い timeout で確認する。
5. 同一 filesystem 内の rename で置換し、可能な OS では directory も `Sync` する。

symlink 自体は変更せずリンク先を置換する。実体 directory に書き込めない場合、自動更新は
skip して `permission_denied` を状態へ記録する。`sudo` の起動、権限昇格、別 directory への
fallback はしない。macOS の署名・notarization を release artifact に導入した場合は、置換前に
その検証も追加する。

### 並行実行と障害復旧

- download から rename まで user 単位の lock を保持する。
- 一時ファイル名に PID と random suffix を使い、起動時に 24 時間を超えた同形式の一時
  ファイルだけを削除する。
- download 中断、checksum 不一致、展開失敗では現在の binary に触れない。
- rename 後に state 保存が失敗しても binary は有効なので rollback しない。次回、埋め込み
  version と manifest を比較して収束させる。

## コマンド適用範囲

自動確認は root の `PersistentPreRunE` 相当へ接続するが、次では実行しない。

- `ccplant update ...`（コマンド自身が同期的に処理する）
- `ccplant server`、`worker`、`session-manager`、`native` など長時間稼働プロセス
- executor/provisioner/hook 用コマンド
- shell completion と `help` / `version`
- stdin/stdout が protocol transport になる ACP 等のコマンド

初版では人が端末から使う短命な管理コマンドだけを allowlist にする。新しい subcommand は
既定で対象外とし、コマンド定義時に明示的に opt-in させる。これにより protocol 出力汚染や
daemon 起動遅延を防ぐ。長時間稼働プロセスの更新は既存の image rollout、ESM upgrade、Native
app updater が担当する。

## コード構成

download・検証・展開・置換は `backend/internal/updater` へ分離し、Cobra や daemon に依存させない。

```text
internal/updater/
  manifest.go     canonical manifest の decode・署名・platform 選択
  client.go       timeout、redirect policy、size limit 付き download
  installer.go    archive 検証、version probe、atomic replace
  state.go        config、interval、lock、警告抑制
cmd/update.go     update check/apply/enable/disable/status
```

Native ESM の `installNativeRelease` は同じ downloader/installer を利用するよう段階的に
置き換える。ただし ESM 側の active session 確認と再起動は現在の management route に残す。
最初に共通化して既存 upgrade の挙動を変えるのではなく、updater の unit test が揃ってから
移行する。

## 観測性とプライバシー

サーバーへの telemetry は追加しない。公開 manifest/asset への通常の HTTP access log 以外に、
ユーザー ID、hostname、実行コマンドを送らない。ローカルの `update status` で次を確認できる
ようにする。

- 現在版、配布種別、対象 platform
- enabled と次回確認時刻
- 最終確認時刻、確認した最新版
- 最終成功版
- 秘密や URL query を含まない分類済みの最終エラー

## テスト

unit test では外部ネットワークを使わず、`httptest.Server`、一時 directory、注入した clock と
public key を使う。

- semver の upgrade/no-op/downgrade/prerelease 判定
- distribution と command allowlist による対象判定
- 正常な signature、改ざん manifest、未知鍵、version/asset 不一致
- redirect の host/scheme 制限、timeout、size 上限、truncated response
- path traversal、symlink/hardlink、重複 binary、巨大 entry を含む tar の拒否
- checksum 不一致、version probe 不一致で既存 binary が不変
- atomic replace 成功と permission denied 時の非破壊性
- 同時実行時に downloader/installer が 1 回だけ動くこと
- state 破損、clock skew、jitter、環境変数 override
- 自動更新失敗が本来の command の stdout/stderr/exit code を変えないこと
- container、session CLI、Tauri sidecar、dev build では通信しないこと

release workflow では全 target の asset が manifest と一致すること、署名を同梱公開鍵で検証
できることを release 公開前に検査する。Linux amd64/arm64 と macOS arm64 の smoke test で、
旧 release binary から新 release への `update apply`、次回起動の version、元コマンドの継続を
確認する。

## ロールアウト

1. `Distribution`、`version --json`、共通 updater、`update check/apply/status`、署名付き release
   manifest を追加する。この段階では自動更新を既定無効にする。
2. 1 release の間、明示更新と Native ESM upgrade の回帰、署名鍵運用、各 platform の
   atomic replace を検証する。
3. `github-release` だけ自動更新を既定有効にする。環境変数と `disable` を同時に案内する。
4. 必要なら update channel、macOS amd64/Windows、OS keychain による設定保護、package manager
   向けの「最新版通知のみ」を後続で追加する。

## 受け入れ条件

- GitHub Release の旧 standalone CLI は、期限到来後の通常コマンドを成功させたまま binary を
  最新 stable へ更新し、次回起動で新版になる。
- `update apply` は対応 3 platform で署名・hash・binary version を検証して更新できる。
- 改ざん、通信断、権限不足、同時実行、途中終了のどの場合も既存 binary を破損しない。
- 自動更新の失敗は通常コマンドの stdout、exit code、protocol transport を変えない。
- container、Kubernetes セッション、Native app sidecar、dev build は自動更新のための通信も
  自己置換もしない。
- ユーザーは永続設定または `CCPLANT_AUTO_UPDATE=0` で更新確認を停止でき、状態を JSON で確認
  できる。
- release 公開は全 asset と署名済み manifest が揃うまで stable manifest を切り替えない。

## 対象外

- control plane、session manager、Kubernetes session Pod の自動 rollout
- Tauri application 自体の updater
- Homebrew、Nix、apt 等の package repository の更新
- prerelease/nightly channel と任意 version への downgrade
- 実行中 daemon の hot swap、権限昇格、root-owned binary の代理更新

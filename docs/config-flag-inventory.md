# UI / backend configuration flag inventory

調査日: 2026-08-09

対象は umbrella chart、UI chart、backend chart、両アプリケーションの実装、`compose.yaml`、`.env.example` である。
ここでの「使用中」は、単に values に存在することではなく、Helm template で manifest に反映され、その出力をアプリまたは Kubernetes が消費することを確認した、という意味である。

## 結論

- umbrella chart 5件、UI chart 6件、backend chart 33件の boolean values は、すべて template から参照されている。完全に参照ゼロの boolean values はない。
- UI chart の54 leaf values、backend chart の281 leaf valuesにも、親オブジェクト単位の `toYaml` / `with` を含めると template 参照ゼロのものはない。
- ただし、互換用の別名、同じ機能を二重に制御する値、chart外からしか設定できない環境変数がある。これらは下表で `整理候補` とした。
- `env` / `envFrom` は任意の環境変数を注入できるため、個別フラグを削除しても利用者が再注入できる。廃止時にはアプリ側の受理も同時に止める必要がある。

判定ラベル:

- `維持`: 現在の機能を直接制御し、代替との重複がない。
- `整理候補`: 使用中だが、統合・非推奨化・命名整理の余地がある。
- `基盤`: Kubernetes/依存chartの標準的なスイッチ。

## Umbrella chart

| value | default | 役割 | 判定 |
|---|---:|---|---|
| `backend.enabled` | `true` | backend subchart自体を作成する | 基盤 |
| `frontend.enabled` | `true` | UI subchart自体を作成する | 基盤 |
| `backend.ingress.enabled` | `false` | backend ingressを作成する | 基盤 |
| `frontend.ingress.enabled` | `false` | UI ingressを作成する | 基盤 |
| `global.ingress.tls.enabled` | `false` | 両chartのURL組み立てでHTTPSを選ぶ共通値 | 維持 |

## UI chart

| value | default | manifest / 環境変数 | 役割 | 判定 |
|---|---:|---|---|---|
| `serviceAccount.create` | `true` | ServiceAccount | ServiceAccountを作成する | 基盤 |
| `serviceAccount.automount` | `true` | ServiceAccount | API credentialの自動mount | 基盤 |
| `ingress.enabled` | `false` | Ingress | UI ingressを作成する | 基盤 |
| `autoscaling.enabled` | `false` | HPA / Deployment replicas | HPAを作り、固定replica指定を外す | 基盤 |
| `cookieEncryptionSecret.enabled` | `true` | `COOKIE_ENCRYPTION_SECRET`, `COOKIE_SECRET` | cookie暗号化secretを注入する | 整理候補: 同じsecretを新旧2名で注入 |
| `oauthOnlyMode.enabled` | `false` | `AUTH_MODE=oauth_only` | OAuth専用ログインに強制する | 整理候補: `config.authMode=oauth_only` と重複 |

UI chartが明示的に生成する環境変数は次の通り。

| 環境変数 | values / 用途 | 状態 |
|---|---|---|
| `AGENTAPI_PROXY_URL` | `config.proxyUrl` → `oauthOnlyMode.proxyUrl` → release内backend Serviceの順で決定 | 使用中。`oauthOnlyMode.proxyUrl` は互換用の重複 |
| `ALLOWED_ORIGINS` | `config.publicUrl` またはhostname/TLSから導出。proxy routeのCORS | 使用中 |
| `NEXT_PUBLIC_BASE_URL` | 同上。OAuth callback/public URL | 使用中 |
| `AUTH_MODE` | `oauthOnlyMode.enabled` または `config.authMode` | 使用中、設定経路が二重 |
| `LOGIN_TITLE`, `LOGIN_DESCRIPTION`, `LOGIN_SUB_DESCRIPTION`, `FAVICON_URL` | `config.*`。`/api/config` の表示設定 | 使用中 |
| `PWA_APP_NAME`, `PWA_SHORT_NAME`, `PWA_DESCRIPTION`, `PWA_ICON_URL` | `pwa.*`。manifest生成 | 使用中 |
| `COOKIE_ENCRYPTION_SECRET` | `cookieEncryptionSecret.*`。認証cookie | 使用中 |
| `COOKIE_SECRET` | 同じsecret。別cookie実装のfallback | 整理候補: `COOKIE_ENCRYPTION_SECRET` へ統一可能か要確認 |

### UIアプリが受理するがchartに専用valueがないフラグ

| 環境変数 | 役割 | 判定 |
|---|---|---|
| `DISABLE_AUTH=true` | middlewareの認証を全面的に迂回 | 整理候補: 強い開発用フラグ。productionで禁止または削除を検討 |
| `NEXT_PUBLIC_OAUTH_ONLY_MODE=true` | `/api/config` の旧OAuth-only指定 | 整理候補: `.env.example` でdeprecated。`AUTH_MODE`へ統一 |
| `DEBUG_LOGS=false` | development時のproxy/cookie debug logを止める | 維持または通常のlog levelへ統合 |
| `AGENTAPI_DEBUG` | Next設定経由のdebug指定 | 整理候補: 実利用箇所が限定的 |
| `NEXT_PUBLIC_LOGIN_TITLE`, `NEXT_PUBLIC_LOGIN_DESCRIPTION`, `NEXT_PUBLIC_LOGIN_SUB_DESCRIPTION` | login表示の旧build-time fallback | 整理候補: runtime変数へ統一 |
| `NEXT_PUBLIC_PWA_APP_NAME`, `NEXT_PUBLIC_PWA_SHORT_NAME`, `NEXT_PUBLIC_PWA_DESCRIPTION` | PWA表示のbuild-time fallback | 整理候補: runtime変数へ統一 |
| `VAPID_PUBLIC_KEY`, `NEXT_PUBLIC_VAPID_PUBLIC_KEY` | push通知公開鍵のruntime/build-time経路 | 整理候補: runtime経路へ統一 |

`AGENTAPI_API_KEY`, `AGENTAPI_TIMEOUT`, `AGENTAPI_PROXY_MAX_SESSIONS`, `AGENTAPI_PROXY_SESSION_TIMEOUT`, `NEXT_PUBLIC_API_BASE_URL`, `NEXT_PUBLIC_API_KEY` もクライアントライブラリが受理するが、feature flagではなく接続設定であり、chartには専用valueがない。

## Backend chart

### Kubernetes / 配置基盤

| value | default | 役割 | 判定 |
|---|---:|---|---|
| `serviceAccount.create` | `true` | backend ServiceAccount作成 | 基盤 |
| `serviceAccount.automount` | `true` | API credential自動mount | 基盤 |
| `ingress.enabled` | `false` | backend ingress作成 | 基盤 |
| `controlPlaneService.enabled` | `true` | session podが戻る固定Serviceを有効化 | 整理候補: `create` と意味が近い |
| `controlPlaneService.create` | `true` | 固定Serviceリソースを実際に作成 | 整理候補: `enabled` との2段階制御を明文化または統合 |
| `controlPlaneService.retainOnDelete` | `true` | Helm delete時にServiceをkeep | 維持 |
| `kubernetesSession.enabled` | `true` | session用RBAC/secretを作り、Kubernetes session manager構成を有効化 | 維持 |
| `kubernetesSession.rbac.create` | `true` | session用RBACをchartで作成 | 基盤 |
| `kubernetesSession.pvc.enabled` | `false` | session podへPVCを付与 | 維持 |
| `kubernetesSession.otelCollector.enabled` | `false` | session podへOTel collector sidecarを付与 | 維持 |

### 認証・GitHub・role environment

| value | default | 環境変数 / 役割 | 判定 |
|---|---:|---|---|
| `github.enterprise.enabled` | `false` | `GITHUB_URL`, `GITHUB_API` をEnterprise向けにする | 整理候補: URLの有無だけで判定可能 |
| `github.app.repositoryRestriction` | `false` | `REPOSITORY_RESTRICTION`。GitHub Appを対象repoへ制限 | 維持 |
| `config.auth.static.enabled` | `false` | `AGENTAPI_AUTH_STATIC_ENABLED`。static API key認証 | 維持 |
| `config.auth.github.enabled` | `false` | `AGENTAPI_AUTH_GITHUB_ENABLED`。GitHub token/OAuth認証 | 維持 |
| `roleEnvFiles.enabled` | `false` | `AGENTAPI_ROLE_ENV_FILES_ENABLED`。role別env file読込 | 維持 |
| `roleEnvFiles.loadDefault` | `true` | `AGENTAPI_ROLE_ENV_FILES_LOAD_DEFAULT`。`default.env`も読む | 維持 |

### SCIA / asset / persistence

| value | default | 役割 | 判定 |
|---|---:|---|---|
| `scia.enabled` | `false` | SCIA OAuth/proxy連携と関連リソースを有効化 | 維持 |
| `scia.sessionSidecar.enabled` | `true` | session podへSCIA sidecarを付与 | 整理候補: 親`scia.enabled`との組合せが必要 |
| `scia.oauth.google.secret.create` | `false` | Google OAuth Secretをchartで作成 | 基盤 |
| `scia.oauth.todoist.enabled` | `false` | Todoist providerをSCIAに追加 | 維持 |
| `scia.oauth.todoist.omitRedirectUrl` | `false` | Todoist設定からredirect URLを省略 | 維持（provider互換） |
| `scia.oauth.todoist.secret.create` | `true` | Todoist OAuth Secretをchartで作成 | 基盤 |
| `asset.enabled` | `false` | asset API/ingress/storage構成を有効化 | 維持 |
| `asset.persistence.enabled` | `false` | nginx/local asset storage用PVCを作成 | 維持 |
| `sessionPersistence.persistence.enabled` | `true` | session state用PVCを作成 | 維持。ただしbackendがS3のとき無効化可能 |

### KV / workers / session control / Redis

| value | default | 環境変数 / 役割 | 判定 |
|---|---:|---|---|
| `config.usage.enabled` | `false` | `AGENTAPI_USAGE_ENABLED`。usage DB記録 | 維持 |
| `config.kvStore.migration.enabled` | `false` | migration initContainer実行 | 維持 |
| `config.kvStore.migration.dryRun` | `false` | migrationを検証だけにする | 維持 |
| `config.kvStore.migration.overwrite` | `false` | migration先の既存値を上書き | 維持 |
| `scheduleWorker.enabled` | `false` | `AGENTAPI_SCHEDULE_WORKER_ENABLED`。schedule実行worker | 維持 |
| `slackbotCleanupWorker.enabled` | `false` | `AGENTAPI_SLACKBOT_CLEANUP_WORKER_ENABLED`。古いSlack session削除 | 維持 |
| `slackbotCleanupWorker.dryRun` | `false` | 削除せずlogだけ出す | 維持 |
| `stockInventoryWorker.enabled` | `false` | `AGENTAPI_STOCK_INVENTORY_WORKER_ENABLED`。pre-warmed session補充 | 維持 |
| `stockInventoryWorker.dockerEnabled` | `false` | 単一pool互換設定でDinDを付与 | 整理候補: `pools[].dockerEnabled` 使用時は旧設定 |
| `redis.enabled` | `false` | bundled Redis subchart | 基盤 |
| `redis.auth.enabled` | `false` | bundled Redis認証 | 基盤 |
| `redis.master.persistence.enabled` | `false` | bundled Redis永続化 | 基盤 |
| `sessionControl.enabled` | `false` | `SESSION_CONTROL_LONG_POLL_ENABLED`。Redis経由command pull | 維持 |
| `sessionControl.directRuntimeEnabled` | `false` | `AGENTAPI_DIRECT_SESSION_RUNTIME_ENABLED`。session runtimeへ直接route | 維持。`sessionControl.enabled`依存をvalidation候補にする |
| `externalRedis.tlsEnabled` | `false` | `AGENTAPI_REDIS_TLS_ENABLED` | 維持 |
| `libsqlTrial.enabled` | `false` | trial用libSQL pod/PVC/Serviceを作成 | 整理候補: production chartからexamples/dev chartへ移動候補 |

### Backendが受理するがchartに専用valueがないbooleanフラグ

| 環境変数 | 役割 | 判定 |
|---|---|---|
| `AGENTAPI_OAUTH_VERBOSE_LOGGING`, `AGENTAPI_VERBOSE_LOGGING` | OAuth詳細log。どちらかtrueで有効 | 整理候補: 2名をlog levelへ統合 |
| `AGENTAPI_ACP_RAW_LOG` | ACP JSON-RPC raw log | 維持（diagnostic） |
| `AGENTAPI_REQUIRE_SESSION_STATE_BACKUP=1` | session終了時backup失敗をfatal扱い | 維持。ただしboolean表現を`true`へ統一候補 |
| `AGENTAPI_MEMORY_SAVE_ON_SHUTDOWN` | `true`のとき終了時memory保存 | 維持 |
| `AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED` | chart valueあり。provisionerも直接参照 | 使用中 |
| `SESSION_CONTROL_LONG_POLL_ENABLED` | chart valueあり。server/provisioner双方が直接参照 | 使用中 |
| `AGENTAPI_DIRECT_SESSION_RUNTIME_ENABLED` | chart valueあり。direct runtime route | 使用中 |

`AGENTAPI_NATIVE_SESSION_ROOT`、`PROVISIONER_*`、`SESSION_CONTROL_TOKEN`、`AGENTAPI_SESSION_ID`、`AGENTAPI_AGENT_TYPE`、`GITHUB_*` はsession/provisionerの実行コンテキストでありfeature flagではない。削除対象を選ぶ際は、backend Deploymentだけでなく生成されるsession podの環境変数も確認する必要がある。

## 優先して「いる / いらない」を決めたい整理候補

1. UIの `oauthOnlyMode.enabled` / `oauthOnlyMode.proxyUrl` を廃止し、`config.authMode` / `config.proxyUrl` に統一するか。
2. UIの `COOKIE_SECRET` と `COOKIE_ENCRYPTION_SECRET` を一本化するか。先に2つのcookie実装を統合する必要がある。
3. UIのdeprecated `NEXT_PUBLIC_OAUTH_ONLY_MODE`、`NEXT_PUBLIC_LOGIN_*`、`NEXT_PUBLIC_PWA_*`、build-time VAPID fallbackを削除するか。
4. UIの `DISABLE_AUTH` を開発専用として明示的に制限するか、削除するか。
5. backendの `controlPlaneService.enabled` と `controlPlaneService.create` を1フラグにするか。外部Serviceを使うケースだけを別名で表現すると分かりやすい。
6. `github.enterprise.enabled` を廃止し、`baseUrl` / `apiUrl` の設定有無で判定するか。
7. legacy KV (`config.kvStore.backend/databaseUrl/authToken`) をprimary/secondaryモデルへ完全移行して削除するか。
8. `stockInventoryWorker.targetCount/dockerEnabled` をlegacy single poolとして廃止し、`pools`へ統一するか。
9. `libsqlTrial.enabled` を本体chartから切り離すか。
10. verbose/raw-log系を共通のlog levelへ統合するか。

## 削除前の確認方法

- valueを削るときは `values.yaml`、`values.schema.json`、template、README、umbrella経由のglobal fallbackを同時に更新する。
- 環境変数を削るときは Helm Deploymentだけでなく、Viperの自動mapping、明示的な`os.Getenv`、session pod template、frontendのserver/client build境界を確認する。
- 互換フラグは最低1リリース非推奨警告を出し、旧新両方が設定された場合の優先順位を固定してから削除する。

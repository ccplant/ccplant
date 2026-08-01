# 分離 Helm Chart から ccplant Chart への移行設計

## 目的

`agentapi-proxy` と `agentapi-ui` を別々の Helm Release として管理している環境を、
単一の `ccplant` Release へ安全に移行する。次を設計上の要件とする。

- 既存の Secret、PVC、Service endpoint、認証設定を保持する
- 通常構成ではローリング更新相当の停止時間に抑える
- 実行前に values と Kubernetes resource の差分を機械的に検査できる
- 途中失敗から旧 Release 管理へ戻せる
- dry-run、再実行、監査ログを標準で提供する

本書の「分離 Release」は既定で次を指すが、実際の名前は CLI option で変更できる。

| component | Release | Chart |
|---|---|---|
| backend | `agentapi-proxy` | `agentapi-proxy` |
| frontend | `agentapi-ui` | `agentapi-ui` |
| 移行先 | `ccplant` | `ccplant` |

## 現状の制約

`ccplant` は component Chart を `backend`、`frontend` という alias で参照している。
values は原則として次のように移せる。

```yaml
# agentapi-proxy-values.yaml
replicaCount: 2
redis:
  enabled: true
```

```yaml
# ccplant-values.yaml
backend:
  replicaCount: 2
  redis:
    enabled: true
```

ただし values の入れ子化だけでは in-place migration はできない。サブチャートから見た
`.Release.Name` が `ccplant` になるため、既定では次が変わる。

- resource name: `agentapi-proxy` → `ccplant-agentapi-proxy`
- selector label: `app.kubernetes.io/instance: agentapi-proxy` → `ccplant`
- Helm ownership annotation:
  `meta.helm.sh/release-name: agentapi-proxy` → `ccplant`

Deployment の selector は immutable なので、稼働中のリソースに新しい manifest をそのまま
適用する方式は失敗する。また、ownership 移管後に旧 Release を `helm uninstall` すると、
旧 Release の保存済み manifest に含まれるリソースが削除される。したがって、chart の
互換機能と専用の移行ツールをセットで提供する。

## 提案する chart 互換機能

両 component Chart に `instanceOverride` を追加する。

```yaml
backend:
  fullnameOverride: agentapi-proxy
  instanceOverride: agentapi-proxy
frontend:
  fullnameOverride: agentapi-ui
  instanceOverride: agentapi-ui
```

`instanceOverride` は selector と Pod template の
`app.kubernetes.io/instance` の両方にだけ適用し、未指定時は従来どおり
`.Release.Name` を使う。`fullnameOverride` は既存機能を利用する。

ccplant Chart には操作を簡略化する preset を追加する。

```yaml
migration:
  preserveLegacyIdentity: false
  legacyBackendRelease: agentapi-proxy
  legacyFrontendRelease: agentapi-ui
```

`preserveLegacyIdentity=true` の場合、明示指定されていない
`backend.fullnameOverride` / `backend.instanceOverride` と frontend 側の値だけを補完する。
ただし Helm の values だけでサブチャート values を動的に書き換えるのは避け、実装時は
親 chart の JSON schema と migration CLI が上記 4 値を生成する方式を第一候補とする。
これにより通常 install の chart 挙動を複雑にしない。

互換 mode は恒久利用可能とする。identity を後で `ccplant` に変更することには
Deployment の再作成と Service の切り替えが必要で、Release 統合とは別作業として扱う。

## サポートツール

`agentapi-proxy helm migrate` サブコマンドを追加し、次の lifecycle を提供する。

```text
agentapi-proxy helm migrate plan
agentapi-proxy helm migrate adopt
agentapi-proxy helm migrate verify
agentapi-proxy helm migrate finalize
agentapi-proxy helm migrate rollback
```

共通 option:

```text
--namespace <namespace>
--backend-release agentapi-proxy
--frontend-release agentapi-ui
--target-release ccplant
--chart oci://ghcr.io/ccplant/charts/ccplant
--version <exact-version>
--values-out ccplant-values.yaml
--state-secret ccplant-migration-state
--yes
--output text|json
```

### `plan`

read-only で次を行う。

1. 旧 Release の最新 deployed revision と user-supplied values を Helm storage Secret から読む
2. backend values を `backend.*`、frontend values を `frontend.*` に格納する
3. image tag など、ccplant defaults で上書きされる値も旧 Release の実効値を優先する
4. `fullnameOverride` と `instanceOverride` を生成する
5. Secret reference の存在と key を既存の `doctor` ロジックで検査する
6. 旧 manifest と移行先 manifest の resource identity、selector、PVC、Service port を比較する
7. cluster-scoped resource の衝突、hook、`lookup`、CRD、pending Helm operation を検出する
8. 移行可能性と blocking reason を text または JSON で出力する

機密値は values 出力へ複製しない。旧 values に平文の機密値がある場合は warning とし、
可能なら既存 Secret reference へ変換する候補だけを示す。

`plan` の成功条件は、移行対象の全 resource が次のいずれかに分類されることとする。

- `adopt`: identity と spec が互換で ownership のみ変更
- `update`: immutable field を維持したまま更新可能
- `create`: ccplant で新規作成
- `retain`: Helm 管理外の Secret/PVC としてそのまま参照
- `remove-after-cutover`: 旧 chart のみにあり、finalize 後に削除可能

分類不能、immutable field 差分、同名の第三者所有 resource は block する。

### `adopt`

`plan` が生成した plan ID と完全一致する cluster state に対してだけ実行する。

1. 旧 Helm storage Secret、対象 resource の UID/resourceVersion/manifest、生成 values を
   migration state Secret に gzip 圧縮して保存する
2. 対象 resource に migration ID と元 Release の annotation を追加する
3. `meta.helm.sh/release-name`、`meta.helm.sh/release-namespace`、
   `app.kubernetes.io/managed-by` を target Release 用に patch する
4. target Release を compatibility values で作成または upgrade する
5. rollout と application health check を実行する

resource ごとの patch は UID precondition 相当の検査を行う。途中失敗時は自動で
ownership を戻し、旧 Release を同じ revision/values で reconcile する。

`helm upgrade --install --atomic` は採用済みリソースを失敗時に削除する危険があるため
直接使わない。ツールが明示的な transaction log と compensating action を管理する。

### `verify`

次を検査し、一定時間連続して成功した場合だけ finalize を許可する。

- target Release が deployed
- Deployment rollout 完了、Service endpoint 有効
- backend health endpoint と frontend HTTP endpoint
- Secret reference と PVC binding
- 対象 resource の Helm ownership がすべて target Release
- 旧 Release 作成の session workload が backend から参照可能
- optional worker/Redis/SCIA/asset workload の期待数

`--probe-url`、`--wait`、`--success-window` で環境固有 probe を追加できるようにする。

### `finalize`

旧 Release を `helm uninstall` してはいけない。verify 済み plan に対して以下を行う。

1. 旧 Release の Helm storage Secret を state Secret と外部 backup file に保存済みか確認する
2. 旧 Release の `sh.helm.release.v1.*` Secret のみを削除する
3. migration annotation を audit 用の完了 annotation に置換する
4. state Secret に完了時刻、chart digest、resource UID、probe 結果を記録する

PVC、application Secret、workload は削除しない。state Secret の既定保持期間は 30 日とする。

### `rollback`

finalize 前を標準の rollback window とする。

1. ccplant で新規作成された resource だけを削除する
2. adoption 対象の ownership annotation を元 Release へ戻す
3. 保存した旧 Helm storage Secret を復元する
4. 両旧 Release を保存 revision/values で upgrade し reconcile する
5. rollout と health check を実行する

finalize 後も state Secret が残る間は同じ操作を可能にするが、ccplant 移行後に spec を
変更している場合は `--force` なしでは停止する。

## 推奨移行パス

### Phase 0: chart と CLI の準備

- `instanceOverride` を component Chart と schema に追加
- selector/name compatibility の render test を追加
- `helm migrate plan/adopt/verify/finalize/rollback` を実装
- 同一 chart version/digest を plan から finalize まで固定

### Phase 1: staging rehearsal

production の Helm values と Secret/PVC の metadata を複製した namespace で plan から
rollback まで実施する。実データの Secret 内容は複製しない。

### Phase 2: production preflight

```bash
agentapi-proxy doctor -n production \
  --release agentapi-proxy \
  --release agentapi-ui

agentapi-proxy helm migrate plan \
  --namespace production \
  --version 0.3.1 \
  --values-out ccplant-values.yaml
```

plan JSON、生成 values、旧 Release revision、chart digest を変更管理記録へ添付する。

### Phase 3: cutover

変更凍結期間に `adopt` を実行する。API/UI の外部 hostname と Service name は compatibility
values により維持するため、DNS や Ingress の同時切り替えは不要とする。

### Phase 4: observation と finalize

最低 1 回の通常運用周期（推奨 24 時間）は旧 Helm storage を残す。問題がなければ
`verify`、`finalize` の順で実行する。

## 非対応・別手順となるケース

- namespace をまたぐ移行
- backend/frontend が異なる namespace にある構成
- 同一 component を複数 Release 配置して 1 Release に統合する構成
- chart 外で selector/name を変更した resource
- Helm 以外の controller が ownership annotation を継続的に上書きする構成
- migration と同時の PVC StorageClass、Service type、hostname の変更

これらは blue/green deploy とデータ移送を使う。Release 統合と application upgrade、
resource rename、storage migration を同時に行わない。

## テスト戦略

- unit: values nesting、Secret redaction、resource classification、state machine、再実行
- golden: 分離 manifest と compatibility mode の名前・selector・Service/PVC 一致
- integration (kind): plan → adopt → verify → rollback
- integration (kind): plan → adopt → verify → finalize、旧 storage Secret だけが消えること
- fault injection: 各 resource patch 後、target install 中、rollout timeout 時の復旧
- upgrade matrix: サポート対象の直近 2 minor の分離 Chart から最新 ccplant へ

## リリース条件

最初は `experimental` とし、次を満たした後に stable とする。

- kind integration を CI で継続実行
- staging と production 各 2 環境以上で成功
- rollback drill 成功
- migration state の機密情報レビュー完了
- 対応元 chart version と support window を公開

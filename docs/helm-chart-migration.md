# 分離 Helm Chart から ccplant Chart への移行設計

## 方針

`agentapi-proxy` と `agentapi-ui` の旧Releaseを残したまま、別名の`ccplant` Releaseを
同じnamespaceへ先にinstallする。新Releaseが既存のruntime resourceを参照できることを
検証してから外部トラフィックを切り替え、旧Releaseを段階的にdrain・削除する。

保護対象は次に限定する。

- proxy が実行時に作成した session、設定、認証情報などの resource
- 外部から参照している Secret
- PVC など再作成するとデータを失う resource
- 既存 session が固定名で参照する Service、ServiceAccount、Secret

移行先は同じ namespace とする。namespace 自体は削除しない。

## 何が残り、何が消えるか

### 並行稼働後に削除する旧Helm resource

- backend/frontend Deployment、Service、Ingress
- Helm 管理のServiceAccount、Role、RoleBinding、ConfigMap
- Helm manifest に含まれる生成Secret
- optionalなSCIA、asset serverなどのworkload

### そのまま保持するruntime resource

proxyがKubernetes APIで作成する以下のresourceはHelm release manifestに含まれないため、
通常の`helm uninstall`では削除されない。

- `agentapi-session-*` Service、Pod/Deployment、PVC、settings Secret
- task、task group、memory、schedule、webhook、SlackBot
- settings、credentials、API token、personal API key
- session profile、sandbox policy、team config、user/team mapping
- `agentapi-agent-files-*`、`agentapi-user-files-*`
- notification subscription、SCIAのdynamic user Secret

session専用Secretはsession ServiceをOwnerReferenceに持つ。移行中にsession Serviceを
削除しないことが、Secretを保持する条件になる。

## install-firstを可能にするresource名

新旧ReleaseのHelm resourceが衝突しないよう、ccplant側は既定の別名を使う。

| 用途 | 旧Release | 新ccplant Release |
|---|---|---|
| backend Service | `agentapi-proxy` | `ccplant-backend` |
| frontend Service | `agentapi-ui` | `ccplant-frontend` |
| backend Deployment | `agentapi-proxy` | `ccplant-backend` |
| frontend Deployment | `agentapi-ui` | `ccplant-frontend` |

`backend.fullnameOverride: agentapi-proxy`はinstall時に衝突するため設定しない。frontendは
既定で`http://ccplant-backend:8080`を参照できる。

Ingressは同一host/pathの重複を避けるため、新Releaseでは最初は無効にする。shadow検証は
port-forwardまたは一時的な内部hostnameを使い、cutover時に既存Ingressのbackendを
`ccplant-frontend`/`ccplant-backend`へ切り替える。

## 既存sessionとの互換性

既存sessionはRelease名ではなく、固定ラベルで再検出される。

```yaml
app.kubernetes.io/managed-by: agentapi-proxy
app.kubernetes.io/name: agentapi-session
agentapi.proxy/session-id: <session-id>
```

一方、session Pod内のprovisionerは既定で次の固定Service名を参照する。

```text
http://agentapi-proxy.<namespace>.svc.cluster.local:8080
```

新しいbackendではsession provisionerの既定callback先をRelease名に依存しない
`agentapi-proxy-control` Serviceとする。Service selectorを旧backendから新backendへ切り替える
ことで、対応versionで作成されたsessionは再作成せずにcontrol planeを切り替えられる。
変更前のversionで作成され、`agentapi-proxy`を直接参照するsessionだけはlegacy sessionとして
drainまたは互換Serviceで処理する。

session用ServiceAccount/Role/RoleBindingは現在固定名`agentapi-proxy-session`であり、並行
installすると衝突する。ccplant Chartに次のoptionを追加し、shadow installでは既存RBACを
共有する。

```yaml
backend:
  controlPlaneService:
    create: false
  kubernetesSession:
    rbac:
      create: false
    serviceAccountName: agentapi-proxy-session
```

control Serviceは`helm.sh/resource-policy: keep`で保持される。旧backendをuninstallする前に
selectorを新backendへ切り替える。session RBACは一時的に既存Releaseのものを共有し、旧backend
削除前にccplant管理へ引き継ぐかRelease外の共通RBACとして管理する。

## Secret互換性

Secretを次の4種類に分類する。

| 種類 | 例 | 処理 |
|---|---|---|
| session専用 | `agentapi-session-*-settings`、webhook payload | 保持 |
| application data | task、schedule、credentials、API token | 保持 |
| 外部参照 | OAuth、GitHub PEM、Slack、cookie暗号鍵 | 同じname/keyを再利用 |
| Helm生成 | GitHub session/config、SCIA credential | 同じ入力から再生成 |

暗号化済みデータには、同じ暗号方式と鍵が必須である。

- `AGENTAPI_ENCRYPTION_KEY`または`config.encryption.key`
- KMS key ID、region、IAM権限
- Git sync用KMS設定

移行ツールはSecret値を表示・複製せず、参照先のname/key、暗号方式、key fingerprintを
比較する。install後は実際のsettings/credential復号をprobeする。

## PVCの例外

runtime生成のsession PVCは残る。一方、Helm manifestに直接含まれるasset PVCなどは
`helm uninstall`で削除され得る。Redis StatefulSetのvolumeClaimTemplate由来PVCも含め、
実際の保持動作だけに依存せずpreflightで全PVCを分類する。

- データ不要: 再作成を許可
- snapshot/backup可能: backup完了後に移行
- 保持必須: 旧Releaseを削除する前にretain可能な構成へupgradeする
- 分類不能: 移行をblockする

保持必須PVC向けにcomponent Chartへ`helm.sh/resource-policy: keep`を設定できる
`persistence.retainOnDelete`相当のoptionを追加する。古いChartが対応しない場合は、まず
同じ分離Release名のまま互換Chartへupgradeしてからuninstallする。

## サポートツール

`agentapi-proxy helm migrate`に次のsubcommandを追加する。

```text
agentapi-proxy helm migrate plan
agentapi-proxy helm migrate backup
agentapi-proxy helm migrate cutover
agentapi-proxy helm migrate verify
agentapi-proxy helm migrate rollback
agentapi-proxy helm migrate finalize
```

共通option:

```text
--namespace <namespace>
--backend-release agentapi-proxy
--frontend-release agentapi-ui
--target-release ccplant
--chart oci://ghcr.io/ccplant/charts/ccplant
--version <exact-version>
--values-out ccplant-values.yaml
--state-secret ccplant-migration-state
--output text|json
--yes
```

### `plan`

read-onlyで以下を行う。

1. 旧Release revision、values、manifestを読む
2. backend valuesを`backend.*`、frontend valuesを`frontend.*`へ変換する
3. 新旧resource名、Ingress、固定RBACの衝突を検査してshadow valuesを生成する
4. cluster resourceを`recreate`、`retain`、`backup`、`block`に分類する
5. runtime Secret/PVCに旧Helm ownershipが誤って付いていないか検査する
6. Secret reference、暗号設定、Service DNS、ServiceAccount名を検査する
7. install先が既存RBACを安全に共有できることを検査する
8. control Serviceのselectorとsession ID、data Secret UID、PVC UIDのbaselineを保存する

target chartはversionだけでなくOCI digestまで固定する。

### `backup`

- 旧Helm storage Secretと生成valuesを保存
- application Secretはmetadata、key名、content hashだけをstate Secretへ保存
- Secret本体と保持必須PVCは暗号化された外部backup/snapshotを要求
- session ID、resource UID、OwnerReferenceを記録
- backup未完了のresourceがあればcutoverを許可しない

### `cutover`

```text
1. helm install ccplant ... -f ccplant-shadow-values.yaml
2. 新backendをread-only/shadow modeで検証
3. 旧backendのbackground workerと新規session作成を停止
4. 新backendのworkerを有効化し、leader electionを確認
5. control Service selectorとIngress/外部routingを新Serviceへ切り替える
6. 新backendで新規session作成を有効化
7. 旧frontendをuninstall
8. control Service非対応のlegacy sessionがあれば旧backendをdrain状態で維持
```

schedule、SlackBot、stock inventoryなどのworkerを新旧で同時にactiveにしない。leader
election対象でない処理もある前提で、toolが旧側停止→新側開始の順序を制御する。

旧backendの削除条件は、旧backendで作成されたsessionが0件、または全sessionのcallback先が
新Serviceへ更新済みであること。条件を満たさない間はuninstallを禁止する。

### `verify`

- target Releaseがdeployed
- backend/frontend rolloutとService endpoint
- 移行前後のsession ID集合とruntime resource UIDが一致
- 既存sessionへのroute、message送信、Pod再起動後の再provision
- settings、credentialsの復号
- API token認証
- task/memory/schedule/webhook等の件数と代表read
- external Secretのname/key参照
- PVCのBound状態と代表データread
- 旧backend経由と新backend経由のsession ID集合が一致
- 新規sessionのcallback先が`ccplant-backend`
- background workerが新旧で二重実行されていない

一定時間連続して成功するまでfinalizeを禁止する。

### `rollback`

1. 外部routingを旧Serviceへ戻す
2. 新規session作成と新workerを停止する
3. 旧workerと新規session作成を再開する
4. cutover後に新規作成されたsessionのcallback互換性を確認する
5. 問題がなければ`helm uninstall ccplant`

runtime resourceはrollback中も削除しない。ccplant稼働後に新規作成されたdata resourceが
ある場合は自動削除せず、旧backendとのschema互換性を検査する。

### `finalize`

移行stateに完了時刻、chart digest、probe結果を記録する。backupとstate Secretは既定で
30日保持する。

## 移行手順

1. stagingでproduction相当のresource構成を再現してrollbackまで試験
2. productionで`doctor`と`helm migrate plan`を実行
3. Secret/PVC backupを取得
4. `ccplant`をshadow installして既存resourceのreadを検証
5. routingとworker ownershipをcutover
6. 旧frontendを削除し、旧backendをlegacy callback用にdrain
7. legacy sessionがなくなったら共通RBACを引き継ぎ、旧backendを削除
8. 問題がなければ`finalize`

通常の停止はIngress/routing反映中の既存接続再確立だけに限定する。Podを先にReadyにするため、
新規HTTP requestはほぼ無停止で切り替えられる。SSE/WebSocketはclient再接続を前提とする。

## テスト戦略

- golden: values変換、shadow resource名、共有RBAC、Ingress無効化
- kind: 新旧backendの並行稼働と既存sessionの列挙・操作
- kind: routing切り替え中の連続HTTP probeとSSE再接続
- kind: settings/credentials/API token/task/schedule等の再読込
- kind: session Pod再起動後のprovision成功
- kind: workerの旧→新handoverで二重実行されないこと
- kind: legacy session drain後に旧backendを削除できること
- PVC: retain、snapshot/restore、blockの各経路
- fault injection: 各uninstall後、install失敗、rollout timeout時のrollback
- version matrix: サポート対象の直近2 minorから最新ccplantへ移行

## 非対応

- namespaceをまたぐ移行
- namespace削除を伴う移行
- application data schemaのmigrationを同時に行う移行
- storage class変更やPVC resizeを同時に行う移行
- 複数の同一component Releaseを1つへ統合する移行

これらは別途データ移送を伴うblue/green手順を使用する。

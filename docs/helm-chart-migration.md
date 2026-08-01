# 分離 Helm Chart から ccplant Chart への移行設計

## 方針

`agentapi-proxy` と `agentapi-ui` の Deployment など、Helm が生成した再作成可能な
resource は維持しない。短い停止時間を許容し、旧 Release を削除してから単一の
`ccplant` Release を新規 install する。

保護対象は次に限定する。

- proxy が実行時に作成した session、設定、認証情報などの resource
- 外部から参照している Secret
- PVC など再作成するとデータを失う resource
- 既存 session が固定名で参照する Service、ServiceAccount、Secret

移行先は同じ namespace とする。namespace 自体は削除しない。

## 何が残り、何が消えるか

### 再作成する Helm resource

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

また、session Podは`agentapi-proxy-session` ServiceAccountを使用する。したがって移行先
valuesではbackendのresource名を維持し、frontendの接続先を明示する。

```yaml
backend:
  fullnameOverride: agentapi-proxy

frontend:
  config:
    proxyUrl: http://agentapi-proxy:8080
```

Deployment自体は再作成され、selectorの継続性は要求しない。必要なのはService DNS名と
session用ServiceAccount/RBACの再作成である。

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
3. `backend.fullnameOverride`と`frontend.config.proxyUrl`を設定する
4. cluster resourceを`recreate`、`retain`、`backup`、`block`に分類する
5. runtime Secret/PVCに旧Helm ownershipが誤って付いていないか検査する
6. Secret reference、暗号設定、Service DNS、ServiceAccount名を検査する
7. install先manifestに必要なRBACがあることを検査する
8. session ID、data Secret UID、PVC UIDのbaselineを保存する

target chartはversionだけでなくOCI digestまで固定する。

### `backup`

- 旧Helm storage Secretと生成valuesを保存
- application Secretはmetadata、key名、content hashだけをstate Secretへ保存
- Secret本体と保持必須PVCは暗号化された外部backup/snapshotを要求
- session ID、resource UID、OwnerReferenceを記録
- backup未完了のresourceがあればcutoverを許可しない

### `cutover`

```text
1. 新規作成を停止し、session/resource inventoryを再取得
2. 必要に応じてbackend/frontendをscale down
3. helm uninstall agentapi-ui
4. helm uninstall agentapi-proxy
5. runtime resourceと保持対象の存在・UIDを再確認
6. helm install ccplant ... -f ccplant-values.yaml
7. rolloutを待ち、health checkを開始
```

namespaceは削除しない。uninstall直後にsession Service、settings Secret、data Secret、PVCの
いずれかが消えた場合はinstallへ進まずrollbackする。

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

一定時間連続して成功するまでfinalizeを禁止する。

### `rollback`

1. `helm uninstall ccplant`
2. 旧backend/frontend Releaseを保存したversion/valuesで再install
3. 必要なPVC/Secretをbackupから復元
4. session DNS、RBAC、復号、APIを検証

runtime resourceはrollback中も削除しない。ccplant稼働後に新規作成されたdata resourceが
ある場合は自動削除せず、旧backendとのschema互換性を検査する。

### `finalize`

移行stateに完了時刻、chart digest、probe結果を記録する。backupとstate Secretは既定で
30日保持する。

## 移行手順

1. stagingでproduction相当のresource構成を再現してrollbackまで試験
2. productionで`doctor`と`helm migrate plan`を実行
3. Secret/PVC backupを取得
4. maintenance windowで`cutover`
5. `verify`を実行し、通常運用周期を観察
6. 問題がなければ`finalize`

停止時間は旧Releaseの削除からccplantのbackend/frontendがReadyになるまで。session Podは
原則稼働を続けるが、その間のAPI/SSE接続は切断されるためclient再接続を前提とする。

## テスト戦略

- golden: values変換、固定backend Service名、frontend proxy URL
- kind: runtime resource作成後に旧Releaseをuninstallし、UIDが維持されること
- kind: ccplant install後に既存sessionを列挙・操作できること
- kind: settings/credentials/API token/task/schedule等の再読込
- kind: session Pod再起動後のprovision成功
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

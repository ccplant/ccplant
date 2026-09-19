# Session Managerをセットアップする

`ccplant session-manager install` を使うと、ccplant APIとは別のKubernetesクラスタにSession Managerを登録・デプロイできます。APIとエージェントの実行基盤を分離したい場合や、実行先を追加したい場合に利用します。

コマンドは次の処理をまとめて行います。

1. 親ccplant APIにSession Managerを登録する
2. 接続用の資格情報をKubernetes Secretへ保存する
3. Session Manager Helm Chartをインストールする
4. デプロイ後の資格情報を親APIに対して検証する

## 必要なもの

- Session Managerを配置するKubernetesクラスタと、そのクラスタへ接続済みの`kubectl`
- Helm 3
- `ccplant` CLI
- 親ccplant APIのURL
- Session Managerを登録できる親APIのAPIキー

`ccplant` CLIは[GitHub Releases](https://github.com/ccplant/ccplant/releases)から利用環境に合うアーカイブをダウンロードし、展開したバイナリを`PATH`の通った場所へ配置してください。

```bash
ccplant --version
kubectl cluster-info
helm version
```

::: warning 実行先を確認する
インストール先は現在のkubeconfigコンテキストです。実行前に`kubectl config current-context`で対象クラスタを確認してください。
:::

## インストールする

親APIのAPIキーを環境変数へ設定します。キーはSession Managerの登録にだけ使われ、Kubernetes SecretやHelm valuesには保存されません。

```bash
export AGENTAPI_KEY="<parent-api-key>"
```

次のコマンドでSession Managerを登録し、デプロイします。`--upstream`には親ccplant APIの公開URLを指定します。`/api/v1`を省略したURLも利用できます。

```bash
ccplant session-manager install \
  --upstream https://ccplant.example.com \
  --namespace ccplant-session \
  --release session-manager \
  --pool default \
  --version X.Y.Z
```

`X.Y.Z`は使用するccplantリリースのChartバージョンへ置き換えてください。コマンドはNamespaceを必要に応じて作成し、OCI Chart `oci://ghcr.io/ccplant/charts/session-manager`をインストールして、リソースがReadyになるまで最大10分待ちます。

正常に完了すると、次の形式でインストール先とマネージャーIDが表示されます。

```text
Session manager session-manager installed in namespace ccplant-session (manager <manager-id>)
```

### APIキーをファイルから渡す

環境変数の代わりに、APIキーを保存したファイルを指定できます。

```bash
ccplant session-manager install \
  --upstream https://ccplant.example.com \
  --api-key-file ./parent-api-key \
  --namespace ccplant-session \
  --version X.Y.Z
```

ファイルにはAPIキーだけを保存し、Gitへコミットしないでください。

### 発行済みの登録トークンを使う

APIキーをインストール先へ持ち込めない場合は、別の環境で発行した1回限りの登録トークンを利用できます。

```bash
ccplant session-manager install \
  --upstream https://ccplant.example.com \
  --registration-token-file ./registration-token \
  --namespace ccplant-session \
  --version X.Y.Z
```

`--registration-token`で直接渡すこともできますが、シェル履歴への記録を避けるため`--registration-token-file`を推奨します。登録トークンは初回インストール専用です。

## チームのSession Managerとして登録する

デフォルトでは、APIキーの所有ユーザーに属するSession Managerとして登録されます。チーム所有にする場合は、スコープとチームIDを指定します。

```bash
ccplant session-manager install \
  --upstream https://ccplant.example.com \
  --scope team \
  --team-id <team-id> \
  --name team-builders \
  --namespace ccplant-session \
  --release team-builders \
  --pool builders \
  --version X.Y.Z
```

APIキーには対象チームへSession Managerを登録できる権限が必要です。

## インストールを確認する

DeploymentとPodがReadyであることを確認します。

```bash
kubectl --namespace ccplant-session get deployment,pods
kubectl --namespace ccplant-session logs \
  deployment/session-manager \
  --all-containers \
  --tail=100
```

接続資格情報は、デフォルトでは`<release>-parent` Secretに保存されます。値を表示せず、必要なキーが存在することだけを確認できます。

```bash
kubectl --namespace ccplant-session get secret session-manager-parent
```

親ccplantの画面でSession Managerが接続済みになっていることを確認し、そのManagerまたはPoolを使うセッションを作成して、実行先クラスタにSession Podが作成されることを確認してください。

## 更新・再実行する

同じNamespaceとReleaseでコマンドを再実行すると、既存のマネージャーIDと接続トークンを維持したままHelm Releaseを更新します。

```bash
ccplant session-manager install \
  --upstream https://ccplant.example.com \
  --namespace ccplant-session \
  --release session-manager \
  --pool default \
  --version NEW_VERSION
```

接続先URLまたはPoolを変更した場合や、保存済み資格情報を親APIが拒否した場合は、APIキーを使って再登録します。既存インストールの更新時は`--registration-token`を指定しないでください。

資格情報を保持するSecretには`helm.sh/resource-policy: keep`が設定されます。Helm Releaseを削除しても自動では削除されないため、再インストール時に同じ接続情報を再利用できます。

## 主なオプション

| オプション | デフォルト | 説明 |
| --- | --- | --- |
| `--namespace`, `-n` | `ccplant-session` | インストール先Namespace |
| `--release` | `session-manager` | Helm Release名 |
| `--name` | Release名 | 親APIに表示する名前 |
| `--pool` | `default` | このManagerが提供する論理Pool |
| `--instance-id` | `<namespace>/<release>` | 再登録でも変えない一意なインスタンスID |
| `--version` | 未指定 | Session Manager Chartのバージョン |
| `--chart` | 公式OCI Chart | 利用するHelm Chart |
| `--timeout` | `10m` | Helm処理のタイムアウト |
| `--wait` | `true` | リソースがReadyになるまで待つ |
| `--create-namespace` | `true` | Namespaceがなければ作成する |

すべてのオプションは次のコマンドで確認できます。

```bash
ccplant session-manager install --help
```

## トラブルシューティング

### Kubernetes設定を読み込めない

`load Kubernetes config`で失敗する場合は、`KUBECONFIG`と現在のコンテキストを確認します。

```bash
kubectl config current-context
kubectl auth can-i create deployments --namespace ccplant-session
kubectl auth can-i create secrets --namespace ccplant-session
```

### APIキーまたは登録トークンが必要と表示される

`AGENTAPI_KEY`が現在のシェルに設定されているか確認するか、`--api-key-file`または初回のみ`--registration-token-file`を指定してください。APIキーの値自体をログへ出力しないでください。

### 既存の資格情報が親APIに拒否される

同じインストールコマンドへ有効なAPIキーを渡すと、CLIがSession Managerを再登録してKubernetes Secretを更新します。Secretを手動削除すると既存セッションへ影響する可能性があるため、通常は削除しないでください。

### Helmの待機がタイムアウトする

Podの状態とイベントを確認します。

```bash
kubectl --namespace ccplant-session get pods
kubectl --namespace ccplant-session describe deployment session-manager
kubectl --namespace ccplant-session get events --sort-by=.lastTimestamp
```

親APIへHTTPSで到達できること、クラスタからOCI Chartとコンテナイメージを取得できることも確認してください。

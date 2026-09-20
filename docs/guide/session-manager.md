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
- 親ccplantの設定画面で発行したSession Manager登録トークン

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

### 登録トークンを発行する

親ccplantへログインし、設定画面の「セッションマネージャー」から「登録トークンを発行」を選択します。登録トークンは15分間有効で、1回だけ利用できます。発行後は再表示できないため、その場で安全な場所へコピーしてください。

インストール先では、登録トークンだけを保存したファイルを用意します。ファイルを作成するときは、ほかのユーザーから読み取れない権限にしてください。

```bash
umask 077
read -rsp "Registration token: " REGISTRATION_TOKEN
printf '%s' "$REGISTRATION_TOKEN" > ./registration-token
unset REGISTRATION_TOKEN
```

### Session Managerをデプロイする

発行した登録トークンを使い、Session Managerを登録してデプロイします。`--upstream`には親ccplant APIの公開URLを指定します。`/api/v1`を省略したURLも利用できます。

```bash
ccplant session-manager install \
  --upstream https://ccplant.example.com \
  --registration-token-file ./registration-token \
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

登録トークンは初回登録にだけ使用し、永続的な接続トークンへ交換されます。インストールが成功したら、登録トークンのファイルを安全に削除してください。

### APIキーで登録トークンを自動発行する

自動化が必要な場合は、親APIのAPIキーを使ってCLIに登録トークンを発行させることもできます。通常の対話的なセットアップでは、設定画面から登録トークンを発行する方法を推奨します。

```bash
ccplant session-manager install \
  --upstream https://ccplant.example.com \
  --api-key-file ./parent-api-key \
  --namespace ccplant-session \
  --version X.Y.Z
```

APIキーファイルにはAPIキーだけを保存し、Gitへコミットしないでください。`AGENTAPI_KEY`環境変数から渡すこともできます。APIキーは登録にだけ使われ、Kubernetes SecretやHelm valuesには保存されません。

`--registration-token`で登録トークンを直接渡すこともできますが、シェル履歴への記録を避けるため`--registration-token-file`を推奨します。

## チームのSession Managerとして登録する

設定画面で対象チームのスコープへ切り替えてから登録トークンを発行します。トークンには所有スコープが紐づいているため、インストールコマンドへチームIDを渡す必要はありません。

```bash
ccplant session-manager install \
  --upstream https://ccplant.example.com \
  --registration-token-file ./registration-token \
  --namespace ccplant-session \
  --release team-builders \
  --pool builders \
  --version X.Y.Z
```

登録トークンを発行するユーザーには、対象チームへSession Managerを登録できる権限が必要です。APIキーでトークンを自動発行する場合に限り、`--scope team --team-id <team-id>`を指定します。

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

接続先URLまたはPoolを変更した場合、CLIは保存済みの資格情報を使って登録情報を更新します。既存インストールの通常の更新時は、登録トークンを指定しないでください。

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

### 登録トークンが必要と表示される

初回インストールでは、設定画面で新しい登録トークンを発行し、`--registration-token-file`で指定してください。トークンは15分で期限切れになり、使用後は再利用できません。

### 既存の資格情報が親APIに拒否される

設定画面で対象Manager用の登録トークンを再発行します。既存の接続Secretがある状態では登録トークンを指定できないため、メンテナンス時間を設け、既存セッションへの影響を確認してからSecretを置き換えてください。通常のアップグレードではSecretを削除せず、登録トークンも指定しません。

### Helmの待機がタイムアウトする

Podの状態とイベントを確認します。

```bash
kubectl --namespace ccplant-session get pods
kubectl --namespace ccplant-session describe deployment session-manager
kubectl --namespace ccplant-session get events --sort-by=.lastTimestamp
```

親APIへHTTPSで到達できること、クラスタからOCI Chartとコンテナイメージを取得できることも確認してください。

# デプロイ

本番環境では、統合Helm ChartによるKubernetesデプロイを基本構成とします。評価用途にはDocker Composeを利用できます。

## Helmでインストールする

リリースで公開されるOCI Chartを利用します。`VERSION` は利用するリリース番号へ置き換えてください。

```bash
helm upgrade --install ccplant \
  oci://ghcr.io/ccplant/charts/ccplant \
  --version VERSION \
  --namespace ccplant \
  --create-namespace \
  --values values.yaml
```

利用可能な設定は [`chart/ccplant/values.yaml`](https://github.com/ccplant/ccplant/blob/main/chart/ccplant/values.yaml) を参照してください。Secret値はGitへ直接コミットせず、Kubernetes Secretまたは利用中のSecret管理基盤から渡してください。

## インストール後の確認

```bash
kubectl --namespace ccplant get pods
kubectl --namespace ccplant get services
```

バックエンドCLIの `doctor` は、Helm Release、Deployment、参照されるSecretを検査し、Secretの値を表示せずに問題を報告します。

```bash
agentapi-proxy doctor --namespace ccplant --release ccplant
```

## 本番化チェックリスト

- 外部公開前に認証を有効化する
- TLSを終端し、公開URLを正しく設定する
- 永続KVストアと必要なバックアップを構成する
- API、ワーカー、セッションへ適切なリソース制限を設定する
- メトリクス、ログ、トレースとアラートを構成する
- エージェントの認証情報をSecretとして管理する

分離Chartから統合Chartへ移行する場合は、[Helm Chart移行ガイド](../helm-chart-migration)に従ってください。可観測性の例は[Grafana Cloud APM](../grafana-cloud-apm)にあります。

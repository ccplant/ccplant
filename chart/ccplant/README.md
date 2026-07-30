# ccplant Helm Chart

backendとfrontendを単一のHelm Releaseとしてデプロイします。

```bash
helm install ccplant oci://ghcr.io/ccplant/charts/ccplant \
  --version 0.1.0 \
  --namespace ccplant \
  --create-namespace
```

`backend.*`と`frontend.*`以下には各コンポーネントChartのvaluesを指定できます。

## 開発環境

Pull Request の CI は backend/frontend イメージを同じ commit SHA のタグでビルドし、
そのタグを指定した統合 `ccplant` chart を次のバージョンで publish します。

```text
0.1.0-dev.ccplant.<short-sha>
```

開発環境では個別の `agentapi-proxy` / `agentapi-ui` chart ではなく、この `ccplant`
chart を単一の Helm Release としてデプロイします。

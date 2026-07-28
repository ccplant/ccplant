# ccplant Helm Chart

backendとfrontendを単一のHelm Releaseとしてデプロイします。

```bash
helm install ccplant oci://ghcr.io/ccplant/charts/ccplant \
  --version 0.1.0 \
  --namespace ccplant \
  --create-namespace
```

`backend.*`と`frontend.*`以下には各コンポーネントChartのvaluesを指定できます。

# ccplant Helm Chart

backendとfrontendを単一のHelm Releaseとしてデプロイします。

デフォルトは、backend/frontend各1 replica、直接Kubernetes session有効、
SCIA・asset server・永続PVC・各種background worker・Redis無効の最小構成です。

frontendの認証Cookie用Secretを先に作成してください。

```bash
kubectl create namespace ccplant
kubectl create secret generic agentapi-ui-encryption \
  --namespace ccplant \
  --from-literal=cookie-encryption-secret="$(openssl rand -hex 32)"
```

```bash
helm install ccplant oci://ghcr.io/ccplant/charts/ccplant \
  --version 0.1.0 \
  --namespace ccplant \
  --create-namespace
```

`backend.*`と`frontend.*`以下には各コンポーネントChartのvaluesを指定できます。
明示的な最小構成例は [`values-minimal.yaml`](values-minimal.yaml) を参照してください。

注意事項:

- sessionの作業領域はデフォルトで`emptyDir`です。永続化する場合は
  `backend.kubernetesSession.pvc.enabled=true`とStorageClassを設定してください。
- proxyを複数replicaにする場合は、`backend.redis.enabled=true`または
  `backend.externalRedis.addr`を設定する必要があります。
- SCIA、asset配信、schedule、SlackBot cleanup、stock inventoryは必要な場合だけ有効化してください。

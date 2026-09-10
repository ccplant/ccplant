# Grafana Cloud Application Observability (APM)

agentapi-proxy の Go バックエンドは OpenTelemetry SDK で計装されています。OTLP
エンドポイントを設定すると、次のデータを OTLP/HTTP で送信します。

- Echo の受信 HTTP トレースと RED メトリクス（rate、error、duration）
- W3C Trace Context / Baggage の伝播
- Go ランタイムの CPU、メモリ、GC、goroutine メトリクス

`/health`、`/healthz`、`/ready`、`/readyz` は計装対象外です。ログは従来どおり
標準出力へ出ます。Kubernetes Monitoring / Grafana Alloy で Pod ログを収集してください。

## Grafana Cloud で接続情報を作る

1. Grafana Cloud の対象 stack を開く。
2. **Connections > Add new connection > OpenTelemetry** を開く。
3. Cloud Access Policy token を作り、表示される
   `OTEL_EXPORTER_OTLP_ENDPOINT` と `OTEL_EXPORTER_OTLP_HEADERS` を控える。

エンドポイントは `/otlp` までのベース URL を指定します。SDK が
`/v1/traces` と `/v1/metrics` を付加するため、ベース URL にこれらを付けないで
ください。

## Helm / Kubernetes

認証ヘッダーを Secret に保存します（値は Grafana Cloud の画面に表示されたものを
そのまま使用します）。

```bash
kubectl -n agentapi create secret generic grafana-cloud-otlp \
  --from-literal=OTEL_EXPORTER_OTLP_ENDPOINT='https://otlp-gateway-REGION.grafana.net/otlp' \
  --from-literal=OTEL_EXPORTER_OTLP_HEADERS='Authorization=Basic%20REDACTED'
```

values ファイルに以下を追加します。環境名などの非機密情報だけを values に置き、
endpoint と認証ヘッダーは上で作成した Secret から読み込みます。

```yaml
observability:
  openTelemetry:
    enabled: true
    serviceName: agentapi-proxy
    serviceNamespace: ccplant
    deploymentEnvironment: production
    # 本番では必要に応じて送信量を抑える（例: 10% sampling）
    tracesSampler: parentbased_traceidratio
    tracesSamplerArg: "0.1"
    secretRef:
      name: grafana-cloud-otlp
      endpointKey: OTEL_EXPORTER_OTLP_ENDPOINT
      headersKey: OTEL_EXPORTER_OTLP_HEADERS
```

`service.instance.id` には Pod 名、`service.version` には image tag（未指定時は
chart appVersion）が自動で入ります。

反映します。

```bash
helm upgrade --install agentapi-proxy ./backend/helm/agentapi-proxy \
  --namespace agentapi --create-namespace --values values-grafana.yaml
```

本番ではアプリから Grafana Cloud へ直接送る代わりに、Grafana Alloy をクラスタ内に
置く構成が推奨です。その場合は Secret の endpoint を Alloy の OTLP receiver
（例: `http://alloy.monitoring.svc:4318`）へ変更し、Grafana Cloud の認証は Alloy 側に
設定します。アプリの計装は変更不要です。

## Docker Compose / ローカル

Grafana Cloud の OpenTelemetry connection tile が表示する値を環境変数へ設定して
起動します。

```bash
export OTEL_SERVICE_NAME=agentapi-proxy
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_ENDPOINT='https://otlp-gateway-REGION.grafana.net/otlp'
export OTEL_EXPORTER_OTLP_HEADERS='Authorization=Basic%20REDACTED'
export OTEL_RESOURCE_ATTRIBUTES='deployment.environment=development,service.namespace=ccplant'
docker compose up --build
```

## 確認

起動ログに次が出れば SDK は有効です。

```text
[OTEL] OpenTelemetry OTLP trace and metric export enabled
```

API に数回リクエストした後、Grafana Cloud の **Application > Application
Observability** で `agentapi-proxy` を選びます。表示されない場合は次を確認します。

- endpoint が stack の OTLP gateway であり、Tempo の query endpoint ではない
- token に metrics / traces の write scope がある
- `OTEL_EXPORTER_OTLP_HEADERS` の `Basic ` が `Basic%20` になっている
- コンテナから gateway または Alloy の 4318/TCP に到達できる

`OTEL_EXPORTER_OTLP_ENDPOINT` が未設定、または `OTEL_SDK_DISABLED=true` の場合、
計装は no-op になり既存の動作を変えません。

## Cloudflare Worker から同じトレースを継続する

バックエンドは W3C Trace Context の `traceparent` / `tracestate` と `baggage` を
受け入れるため、Worker が送信したコンテキストを親として HTTP server span を
作成します。Worker とバックエンドの両方を同じ Grafana Cloud stack に送れば、同じ
Trace ID のウォーターフォールとして表示できます。

Cloudflare Workers 標準の自動トレーシングは、現時点では Cloudflare 外のサービスへ
Trace Context を伝播しません。一貫したトレースが必要な場合は、Worker 側を
Worker 対応の OpenTelemetry SDK で計装し、バックエンドへの outbound `fetch` に
W3C `traceparent` を挿入してください。Cloudflare 標準TraceのGrafana向けexportと
併用するとWorker spanが別Trace IDで重複するため、どちらか一方に統一します。

バックエンドの追加設定は不要です。既存の次の設定が伝播と送信を有効にします。

```yaml
observability:
  openTelemetry:
    enabled: true
    serviceName: agentapi-proxy
    deploymentEnvironment: production
    tracesSampler: parentbased_traceidratio
    tracesSamplerArg: "0.1"
    secretRef:
      name: grafana-cloud-otlp
```

`parentbased_traceidratio` により、Worker が `traceparent` で渡したサンプリング判断を
バックエンドでも尊重します。また、Cloudflare 経由のリクエストでは server span に
`cloudflare.ray_id` と `cloudflare.colo` を記録します。Trace Context が欠落した場合も、
Grafana と Cloudflare のログを `cloudflare.ray_id` で相関できます。

動作確認では、バックエンドへ送られるリクエストに有効な `traceparent` と `CF-Ray` が
含まれることを確認し、Grafana Tempo で `service.name="agentapi-proxy"` または
`cloudflare.ray_id="<Ray ID>"` を検索します。

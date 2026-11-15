# Moli CDP session sidecar design

## 1. Summary

Kubernetes session Pod ごとに [Moli](https://github.com/lexmount/moli) を 1 process 起動し、
同じ Pod の agent container から `http://127.0.0.1:9222` で CDP を利用できるようにする。
Moli は Chromium の wrapper ではなく、CDP、WebDriver Classic、WebDriver BiDi を同じ endpoint で
提供する headless browser である。既定の `moli serve` は `127.0.0.1:9222` に bind するため、
network namespace を共有する Kubernetes sidecar と相性がよい。

第一段階では Kubernetes session manager 単位の opt-in 機能として実装する。CDP を Pod 外へ公開する
Service や Ingress は作らない。native session manager は container sidecar という隔離境界を持たないため、
第一段階の対象外とする。

## 2. Goals and non-goals

### Goals

- 有効化した manager が作る通常 session と stock session の双方で Moli を起動する。
- agent から loopback endpoint を使い、Playwright の `connectOverCDP` 等で接続できるようにする。
- Moli の起動失敗を Pod readiness と session 起動結果に反映する。
- session の sandbox、resource limit、停止・再開、stock Pod hash の仕組みに統合する。
- image と機能 mode を operator が固定し、session ごとに任意 image や任意引数を注入させない。

### Non-goals

- CDP endpoint の cluster 外公開、共有 browser pool、複数 session からの browser 共有。
- Chrome と完全互換であることの保証。Moli の CDP 対応範囲は別途 compatibility test で管理する。
- browser profile の session 間共有。初期実装では browser state は session と同じ寿命にする。
- native session manager での process supervisor 実装。

## 3. User-facing configuration

standalone session-manager chart では `session.moli`、backend 組み込み manager では
`kubernetesSession.moli`、dedicated manager では `sessionManager.kubernetesSession.moli` を公開する。

```yaml
session:
  moli:
    enabled: false
    image: ghcr.io/ccplant/ccplant-browser:moli-<pinned-version>
    port: 9222
    layout: true
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: "1"
        memory: 512Mi
```

意味は次のとおりとする。

| field | contract |
| --- | --- |
| `enabled` | manager が作る全 session Pod への注入を有効化する。既定は `false`。 |
| `image` | digest または immutable version tag を推奨する。空文字は validation error。 |
| `port` | Pod 内 loopback CDP port。1..65535、session の provisioner port と重複不可。 |
| `layout` | `true` の場合だけ `--layout` を渡し、geometry、coordinate input、screenshot を有効化する。 |
| `resources` | sidecar 専用 request/limit。agent container の resource と混ぜない。 |

`--resource` と `--debugger` は初期公開しない。前者は外部 resource の取得量を大きく変え、後者は
停止中 script による session hang を招きうるためである。必要性が確認できた後に明示的な boolean として
追加する。任意の `extraArgs` は安全性と将来の CLI compatibility を損なうので公開しない。

設定は `sessionsettings.RuntimeProfile` に `Moli` block を加えて parent から external Kubernetes session
manager に継承する。runtime profile の revision/hash と stock Pod template hash に全 field を含める。
これにより設定または image 更新後に古い stock Pod が claim されることを防ぐ。

## 4. Pod construction

`KubernetesSessionManager.buildDeployment` で、sandbox、SCIA、DinD と同様に専用 builder
`buildMoliSidecar` を呼ぶ。

```yaml
containers:
  - name: moli
    image: ghcr.io/ccplant/ccplant-browser:moli-<pinned-version>
    args: ["serve", "--host", "127.0.0.1", "--port", "9222", "--layout"]
    ports:
      - name: moli-cdp
        containerPort: 9222
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
      readOnlyRootFilesystem: true
      runAsNonRoot: true
      runAsUser: 999
      runAsGroup: 999
    startupProbe:
      httpGet: {path: /json/version, port: moli-cdp}
      periodSeconds: 1
      failureThreshold: 30
    readinessProbe:
      httpGet: {path: /json/version, port: moli-cdp}
      periodSeconds: 5
      failureThreshold: 3
    livenessProbe:
      tcpSocket: {port: moli-cdp}
      periodSeconds: 10
      failureThreshold: 3
    volumeMounts:
      - {name: moli-tmp, mountPath: /tmp}
volumes:
  - name: moli-tmp
    emptyDir: {}
```

`/json/version` は CDP discovery の実動確認に使う。liveness は browser target の有無に依存しない TCP
probe とし、一時的な protocol response failure で browser state を破棄しにくくする。Moli container の
readiness が false なら Pod 全体も Ready にならず、session allocator は利用可能として扱わない。

Moli の既定 bind address は loopback だが command line に明記する。Kubernetes Pod の container は
network namespace を共有するため agent から到達でき、他 Pod からは到達できない。Service port、hostPort、
Ingress は作成しない。cross-origin の remote client を許可しないため `--remote-allow-origins` も渡さない。

agent container には以下を追加する。

```text
MOLI_CDP_ENDPOINT=http://127.0.0.1:9222
CDP_ENDPOINT=http://127.0.0.1:9222
```

前者を ccplant 固有の canonical variable、後者を tool integration 用 alias とする。既に同名の user env が
ある場合は生成値を優先し、sidecar と異なる endpoint が session 内へ注入されないようにする。無効時は
どちらも設定しない。

既存 `session.podTemplate` の strategic merge は Moli 注入後に適用される。`moli` container と `moli-tmp`
volume は `agentapi` container の command 等と同様に invariant として復元し、template による削除、image
差し替え、loopback bind の変更を許可しない。endpoint env は生成済み `agentapi` container に付与される。

## 5. Image contract

upstream repository は prebuilt binary の直接インストールを案内しているが、session Pod では起動時 download
を行わない。`ghcr.io/ccplant/ccplant-browser:moli-<version>` として、ccplant CI で version と checksum を固定した
multi-arch OCI image を作り、license notice と
実行時共有 library を同梱する。image contract は次のとおりとする。

- entrypoint は `moli`、または上記 `args` で `moli serve` が起動できること。
- linux/amd64 と linux/arm64 を release manifest に含めること。
- non-root UID/GID 999、read-only root filesystem、writable `/tmp` で起動できること。
- image version と Moli version を一対一にし、floating `latest` を chart default にしないこと。
- SBOM と provenance を発行し、release update は CDP smoke test 通過後に行うこと。

Moli upstream に公式 OCI image と互換性保証が追加された場合は、ccplant image を mirror から公式 digest の
pin に置き換えられる。

## 6. Network and security

Moli は UID 999 で実行する。現在の network-filter は UID 0 の sidecar traffic だけを redirect 対象外にする
ため、Moli の outbound HTTP(S) も agent と同じ transparent sandbox policy を通る。Moli container に
`HTTP_PROXY` 等を別途設定しない。これにより browser だけが session の egress 制限を迂回する経路を作らない。

CDP は browser の全権操作 API なので、loopback 限定を security boundary とする。ただし同じ Pod の
container は相互に loopback 接続できるため、DinD や executor 等も CDP にアクセス可能である。この信頼境界は
既存 session Pod 内 sidecar と同じであり、container 単位の認証は初期 scope 外とする。Pod 外公開が将来必要に
なった場合は bind address を変更するだけで済ませず、認証 proxy、TLS、NetworkPolicy、origin allowlist を
必須にする。

profile を永続化する将来機能では、workspace を直接共用しない。専用 volume と directory を使い、同一 profile
への同時 writer を禁止し、session snapshot / storage attachment の lifecycle と明示的に統合する。

## 7. Lifecycle and failure semantics

- Moli は Pod と同時に起動する。startup probe 成功前は session を Ready にしない。
- sidecar が crash した場合は Kubernetes が container を再起動する。browser context は失われるが agent process
  は継続するため、client は切断を検出して再接続できる。
- crash loop で Pod readiness が戻らなければ session status に `moli sidecar is not ready` を表示する。
- session suspend/delete では Pod/Deployment と一緒に停止する。再開後の browser state は復元しない。
- graceful Pod termination 時に Moli へ SIGTERM を送り、Pod の既存 termination grace period 内に終了させる。

agent の health endpoint だけを見ている現行 session start 判定は、Pod Ready condition も確認するようにする。
エラー診断では container 名 `moli` の waiting reason、restart count、直近 termination reason を安全な範囲で
status message に含める。ログ本文は自動で親 API に転送しない。

## 8. API and code changes

主な変更点は以下となる。

1. `pkg/config` と `pkg/sessionsettings` に Moli の設定 / `MoliRuntimeProfile` を追加し、default を実装する。
2. Helm values、manager Deployment の環境変数、runtime profile serialization に同じ設定を追加する。
3. Kubernetes session manager に sidecar builder、agent env、volume、probe、resource 設定を追加する。
4. stock template hash と runtime profile revision に Moli 設定を含める。
5. session readiness/status 集約に Moli probe failure の診断を追加する。
6. user guide に Playwright 接続例と Moli/Chrome compatibility の注意を追加する。

公開 session creation API や session profile schema には第一段階で field を追加しない。browser の有無を
untrusted な個々の prompt から切り替えるのではなく、operator が manager/pool の capability として選ぶ。
allocator capability に `moli_cdp_v1` を追加すれば、将来 browser-enabled pool を選択条件にできる。

## 9. Validation

### Unit and rendering tests

- config default、port validation、resource parsing、runtime profile round-trip。
- enabled/disabled の Pod spec snapshot。
- loopback bind、securityContext、probe、emptyDir、agent env が期待どおりであること。
- podTemplate が invariant を削除・上書きできないこと。
- Moli 設定変更で stock Pod hash が変わること。
- standalone、embedded、dedicated manager の Helm rendering test。

### Integration tests

実 image を使う Kubernetes test で次を確認する。

1. `GET http://127.0.0.1:9222/json/version` が成功する。
2. Playwright `connectOverCDP` で page を作成し、fixture URL の DOM を取得できる。
3. `layout=true` で screenshot を取得でき、`false` の contract と区別できる。
4. sandbox allow/deny policy が Moli の navigation にも適用される。
5. Moli process kill 後に container が再起動し、Pod readiness と CDP 接続が回復する。
6. suspend/resume 後に新しい endpoint が利用でき、古い browser context が復元されない。

Moli の更新 gate には Playwright に加え、`/json/protocol`、Target、Page、Runtime、Network の最小 raw-CDP
suite を置く。Moli が Chrome 互換を意図しない領域を ccplant の failure と誤認しないよう、採用する domain と
method を明文化して固定する。

## 10. Rollout

1. image pipeline と smoke test を先に追加する。
2. feature flag default-off で chart/config/runtime profile をリリースする。
3. non-stock の検証 manager で有効化し、起動時間、RSS、restart、navigation failure を観測する。
4. stock session を有効にし、template hash による入れ替えを確認する。
5. opt-in の production pool として公開する。既存 pool の default は変更しない。

監視対象は sidecar readiness、restart count、CDP ready latency、process RSS/CPU、navigation error count とする。
rollback は `enabled=false` とし、runtime profile revision 更新後に新規 Pod から sidecar を除去する。既存 session
を強制終了せず、stock Pod のみ新 hash へ段階的に入れ替える。

## 11. Temporary podTemplate prototype

本実装前の検証に限り、既存 `session.podTemplate` で `moli` container を追加できる。ただしこの方法は
endpoint env の保証、設定 validation、stock hash の意味付け、readiness error の説明が弱いため production
contract にはしない。prototype でも `127.0.0.1` bind、read-only root filesystem、non-root、resource limit、
`/json/version` probe、version/digest pin は本設計と同じにする。

# Monorepo migration

## Source repositories

初回取り込み元は以下です。

- backend: `takutakahashi/agentapi-proxy@59d37797a96491a000b5d3b65e98744aea8fb024`
- frontend: `takutakahashi/agentapi-ui@caf2a1f0383c29cded727175548205d566988bbd`

どちらも`git subtree add`で履歴を保ったまま取り込んでいます。

## Source of truth

移行後は`ccplant/ccplant`を唯一の開発元とします。旧リポジトリへの直接変更は
原則行わず、修正は先にモノレポへ入れます。

## Backport

`Backport components` workflowを手動実行すると、対象ディレクトリを
`git subtree split`で切り出し、旧リポジトリへ同期PRを作成します。

- `backend/` → `takutakahashi/agentapi-proxy`
- `frontend/` → `takutakahashi/agentapi-ui`

workflowには旧リポジトリへpushおよびPR作成できる
`BACKPORT_TOKEN` secretが必要です。

## Release

単一の`vX.Y.Z`タグからbackend/frontendのコンテナ、Goバイナリ、および
旧リポジトリ互換のvalues/templatesを持つ`backend`/`frontend` Helm Chartを同じ
`vX.Y.Z`バージョンで個別に発行します。統合`ccplant` Helm Chartも
`X.Y.Z`バージョンで同時に発行します。

## Helm Chart migration

分離した`agentapi-proxy`/`agentapi-ui` Releaseから`ccplant` Releaseへは、旧Releaseを
残したまま新Releaseを先にinstallするblue/green方式で移行します。

session provisionerはRelease名に依存しない次のServiceをcontrol planeとして参照します。

```text
http://control.<namespace>.svc.cluster.local:8080
```

`control` Serviceは`helm.sh/resource-policy: keep`で保持されます。新しい`ccplant`をshadow
installするときは、既存のcontrol Serviceとsession RBACを共有します。

```yaml
backend:
  controlPlaneService:
    create: false
  kubernetesSession:
    serviceAccountName: agentapi-proxy-session
    rbac:
      create: false
```

移行は次の順序で実施します。

1. 分離backendをcontrol Service対応versionへupgradeする
2. `ccplant`をshadow installし、既存sessionとSecretを参照できることを確認する
3. background workerを旧backendから新backendへ引き継ぐ
4. `control` ServiceのselectorとIngressを新Releaseへ切り替える
5. 旧frontendを削除する
6. control Service非対応のlegacy sessionをdrainする
7. session RBACを引き継いで旧backendを削除する

Secret/PVCの保持条件、rollback、検証項目などの詳細は
[分離Helm Chartからccplant Chartへの移行設計](helm-chart-migration.md)を参照してください。

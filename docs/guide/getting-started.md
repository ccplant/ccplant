# クイックスタート

ccplantでセッションを実行するには、利用できるPoolが必要です。まず、契約内容に応じて次のどちらかへ進んでください。

## Managed Poolを契約している方

Poolのセットアップは不要です。契約時に用意されたManaged Poolをそのまま利用できます。

ccplantへログインし、セッションを作成してください。実行先を明示する場合は、セッションプロファイルの「プール」で契約済みのManaged Poolを選択します。選択しない場合は、利用可能なPoolから自動的に選ばれます。

::: tip Poolが表示されない場合
画面を再読み込みしても契約済みのManaged Poolが表示されない場合は、契約先の管理者へお問い合わせください。ご自身でPoolをセットアップする必要はありません。
:::

## Managed Poolを契約していない方

セッションの実行基盤を用意し、Poolへ接続する必要があります。[Session Managerをセットアップする](./session-manager)へ進んでください。セットアップ後にccplantへ戻り、セッションを作成します。

## ローカルで試す

開発環境でccplant自体を試す場合は、Docker Composeを使ってバックエンドとWeb UIをローカルで起動できます。

## 必要なもの

- Git
- Docker EngineとDocker Compose v2
- エージェントを利用するための各プロバイダー認証情報

## 起動する

```bash
git clone https://github.com/ccplant/ccplant.git
cd ccplant
docker compose up --build
```

ビルドが完了したら、次のURLを開きます。

- Web UI: `http://localhost:3000`
- Backend API: `http://localhost:8080`
- Health check: `http://localhost:8080/health`

ローカル構成では静的認証とGitHub認証を無効にしています。公開ネットワークへそのまま配置しないでください。

## 動作を確認する

```bash
curl --fail http://localhost:8080/health
```

Web UIを開き、設定画面で利用するエージェントや認証情報を構成します。設定後、セッション一覧から新しい作業を開始できます。

## 停止する

フォアグラウンドで実行中なら `Ctrl+C` を押します。コンテナを停止するには次を実行します。

```bash
docker compose down
```

## ソースから開発する

バックエンドにはGo、フロントエンドにはBun 1.3.5を使用します。

```bash
make backend-test
make frontend-install
make frontend-test
```

全体像を理解するには[アーキテクチャ](./architecture)へ、本番運用を始めるには[デプロイ](./deployment)へ進んでください。

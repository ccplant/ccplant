# クイックスタート

Docker Composeを使い、ccplantのバックエンドとWeb UIをローカルで起動します。

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

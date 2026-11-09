# ローカルで起動・開発する

## k3s で API・session-manager・worker をまとめて起動する

Docker Engine、k3d 5.8.3、kubectl、Helm 3、jq、OpenSSL、make を用意します。
k3d は Docker 内に専用の k3s クラスター `ccplant-dev` を作成します。
他のクラスターへ操作を送らないよう、各コマンドは専用 context を明示します。

```bash
make k3s-up
# 別のターミナルで API を localhost に公開
make k3s-forward
# セッション作成・Pod Ready・一覧取得・セッションへの転送を検証
make k3s-smoke
```

`k3s-up` はソースから軽量 API image をビルドし、k3s へ取り込み、同じ image で
API・session-manager・worker を起動します。worker は schedule 処理を有効にします。
セッションには通常の ccplant-agent image を使い、作業領域は k3s の local-path PVC に保存します。
再実行すると image を再ビルドし、3つの Deployment を更新します。

API は `http://localhost:8080` です。認証キーは namespace `ccplant-dev` の
Secret `ccplant-local` の `admin-key` に保存されます。API リクエストには
`X-API-Key` ヘッダーで渡してください。各内部通信には別のキーを生成します。
実際にモデルへ作業を依頼するには、利用するプロバイダーの認証情報を設定してください。
smoke は初期メッセージを送信せず、モデルへの推論リクエストを必要としません。

開発用 API と manager のデータベースは `/tmp` に置かれ、Pod の再作成で失われます。
セッションの PVC も含めて専用クラスターを削除するには、次を実行します。

```bash
make k3s-down
```

`.github/workflows/k3s.yml` は同じコマンドで k3s を起動し、セッション作成まで検証します。
失敗時は Pod・イベント・各コンポーネントのログを出力します。

## Docker Compose

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

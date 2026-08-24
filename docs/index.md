---
layout: home

hero:
  name: ccplant
  text: Your agents, always within reach.
  tagline: AI coding agentsの実行環境、セッション、UIをひとつに。ローカルからKubernetesまで、チームの開発を止めずに育てるオープンソース基盤です。
  image:
    src: /logo.svg
    alt: ccplant
  actions:
    - theme: brand
      text: 5分で試す
      link: /guide/getting-started
    - theme: alt
      text: ccplantを知る
      link: /guide/what-is-ccplant

features:
  - icon: 🌱
    title: エージェントを育てる
    details: CodexやClaude Codeなどの実行セッションを作成し、状態・ログ・会話を一か所で管理します。
  - icon: 🧭
    title: どこからでも操作
    details: Web UIとネイティブアプリから、稼働中のセッションへリアルタイムにアクセスできます。
  - icon: 🛡️
    title: チームで安全に運用
    details: 認証、権限、永続化、監視を備え、Docker ComposeからKubernetesへ段階的に展開できます。
  - icon: ⚡
    title: APIファースト
    details: セッション作成・検索・ルーティングをAPIとして提供。既存の自動化や開発フローにも組み込めます。
  - icon: 📦
    title: ひとつのモノレポ
    details: Goバックエンド、Next.js UI、Tauriアプリ、Helm Chartを同じリリースサイクルで管理します。
  - icon: 🔓
    title: オープンソース
    details: 自分たちのインフラで実行でき、ワークロードや成長に合わせて構成を選べます。
---

## 植えて、つないで、育てる

ccplantは、AI coding agentを単発のローカルプロセスから、継続して使えるチームの基盤へ変えます。Proxyがセッションのライフサイクルを担い、Web UIが人とエージェントをつなぎ、Session Managerが実行場所を拡張します。

```text
Developer → Web / Native UI → ccplant API → Agent session
                                      └──→ Local / Kubernetes
```

[クイックスタートへ進む →](/guide/getting-started)

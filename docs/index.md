---
layout: home

hero:
  name: ccplant
  text: AIエージェントを、どこからでも動かす。
  tagline: セッションをリモートで実行し、多彩なトリガーから起動。好きなエージェントを、セルフホストした実行基盤で動かせます。
  image:
    src: /brand/ccplant-logo-approved.png
    alt: ccplant
  actions:
    - theme: brand
      text: 5分で試す
      link: /guide/getting-started
    - theme: alt
      text: ccplantを知る
      link: /guide/what-is-ccplant

features:
  - icon: 🌐
    title: リモートでセッション実行
    details: エージェントをリモート環境で起動し、ブラウザやデスクトップから接続。場所や端末に縛られず作業を継続できます。
  - icon: ⚡
    title: 多彩なトリガー
    details: Web UIやAPIに加え、スケジュール、Webhook、Slackなどを起点にセッションを自動実行できます。
  - icon: 🔀
    title: エージェントを自由に選択
    details: Codex、Claude Codeなどから用途に合うエージェントを選択。特定ベンダーへロックインされません。
  - icon: 🏠
    title: 実行基盤をセルフホスト
    details: エージェントとコードを自分たちの管理下で実行。ローカル環境からKubernetesまで、要件に合う基盤を選べます。
---

## エージェントの実行を、手元の端末から解放する

ccplantは、AI coding agentのセッションをリモートで作成・実行・再接続できるオープンソース基盤です。人がUIから開始する作業も、イベントやスケジュールから始まる自動処理も、同じセッション基盤で扱えます。

```text
UI / API / Schedule / Webhook / Slack
                  ↓
              ccplant API
                  ↓
     Codex / Claude Code / other agents
                  ↓
      Your infrastructure / Kubernetes
```

エージェントと実行場所は交換可能です。使いたいエージェントを選び、コードや認証情報を自分たちの管理するインフラに置いたまま運用できます。

[クイックスタートへ進む →](/guide/getting-started)

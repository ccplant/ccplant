import { defineConfig } from 'vitepress'

export default defineConfig({
  lang: 'ja-JP',
  title: 'ccplant',
  description: 'AI coding agentsを、どこからでも安全に動かすためのオープンソース基盤。',
  base: process.env.DOCS_BASE ?? '/ccplant/',
  cleanUrls: true,
  vite: {
    // Reuse the approved runtime brand assets instead of maintaining docs-only copies.
    publicDir: '../frontend/public'
  },
  lastUpdated: true,
  head: [
    ['meta', { name: 'theme-color', content: '#07130d' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'ccplant — Your agents, always within reach.' }],
    ['meta', { property: 'og:description', content: 'AI coding agentsを、どこからでも安全に動かすためのオープンソース基盤。' }]
  ],
  themeConfig: {
    logo: '/brand/ccplant-app-icon-approved.png',
    siteTitle: 'ccplant',
    nav: [
      { text: 'ガイド', link: '/guide/getting-started' },
      { text: 'アーキテクチャ', link: '/guide/architecture' },
      { text: '運用', link: '/guide/deployment' },
      { text: 'GitHub', link: 'https://github.com/ccplant/ccplant' }
    ],
    sidebar: [
      {
        text: 'はじめる',
        items: [
          { text: 'ccplantとは', link: '/guide/what-is-ccplant' },
          { text: 'クイックスタート', link: '/guide/getting-started' },
          { text: 'アーキテクチャ', link: '/guide/architecture' },
          { text: 'デプロイ', link: '/guide/deployment' }
        ]
      },
      {
        text: '運用ガイド',
        collapsed: false,
        items: [
          { text: 'KVストア', link: '/kv-store' },
          { text: 'セッション永続化', link: '/acp-session-persistence' },
          { text: 'Grafana Cloud APM', link: '/grafana-cloud-apm' },
          { text: 'Helm移行', link: '/helm-chart-migration' },
          { text: 'モノレポ移行', link: '/migration' }
        ]
      }
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/ccplant/ccplant' }],
    search: { provider: 'local' },
    editLink: {
      pattern: 'https://github.com/ccplant/ccplant/edit/main/docs/:path',
      text: 'GitHubでこのページを編集'
    },
    lastUpdated: { text: '最終更新' },
    outline: { label: 'このページの内容', level: [2, 3] },
    docFooter: { prev: '前へ', next: '次へ' },
    returnToTopLabel: '先頭へ戻る',
    sidebarMenuLabel: 'メニュー',
    darkModeSwitchLabel: 'テーマ'
  },
  sitemap: { hostname: 'https://ccplant.github.io/ccplant/' }
})

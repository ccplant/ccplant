'use client';

import { useEffect } from 'react';
import { useConfig } from '../../hooks/useConfig';

/**
 * リクエストホストに対応するアプリ名とアイコンを動的に設定する。
 */
export const DynamicFavicon: React.FC = () => {
  const { config } = useConfig();

  useEffect(() => {
    if (!config) return;

    document.title = config.appTitle;

    if (!config.faviconUrl) return;

    // 既存の favicon link タグを取得または作成
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }

    // favicon URL を更新
    link.href = config.faviconUrl;

    let appleIcon = document.querySelector<HTMLLinkElement>('link[rel="apple-touch-icon"]');
    if (!appleIcon) {
      appleIcon = document.createElement('link');
      appleIcon.rel = 'apple-touch-icon';
      document.head.appendChild(appleIcon);
    }
    appleIcon.href = config.faviconUrl;
  }, [config]);

  // このコンポーネントは何も描画しない
  return null;
};

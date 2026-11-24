'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  | 'dockerEnabled'
  | 'setDockerEnabled'
  | 'dockerRegistries'
  | 'addDockerRegistry'
  | 'removeDockerRegistry'
  | 'updateDockerRegistry'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileDockerSection({
  dockerEnabled,
  setDockerEnabled,
  dockerRegistries,
  addDockerRegistry,
  removeDockerRegistry,
  updateDockerRegistry,
}: Props) {
  return (
    <div className="space-y-5">
      {/* Docker in Docker (DinD) */}
      <div>
        <label className="flex items-start gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={dockerEnabled}
            onChange={(e) => setDockerEnabled(e.target.checked)}
            className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
          />
          <div>
            <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
              Docker in Docker (DinD) を有効にする
            </span>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
              セッション Pod に docker:dind サイドカーを追加し、
              <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded text-xs">DOCKER_HOST</code>{' '}
              を自動設定します。
            </p>
          </div>
        </label>

        {dockerEnabled && (
          <div className="mt-3 ml-6 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">認証済みレジストリ（任意）</span>
              <button
                type="button"
                onClick={addDockerRegistry}
                className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700"
              >
                + 追加
              </button>
            </div>
            {dockerRegistries.length === 0 && (
              <p className="text-xs text-gray-400 dark:text-gray-500">認証不要な場合は追加不要です。</p>
            )}
            {dockerRegistries.map((registry, index) => (
              <div key={index} className="border border-gray-200 dark:border-gray-600 rounded-lg p-3 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium text-gray-600 dark:text-gray-400">レジストリ #{index + 1}</span>
                  <button
                    type="button"
                    onClick={() => removeDockerRegistry(index)}
                    className="text-xs text-red-500 hover:text-red-600"
                  >
                    削除
                  </button>
                </div>
                <div>
                  <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">
                    サーバー（空 = Docker Hub）
                  </label>
                  <input
                    type="text"
                    value={registry.server}
                    onChange={(e) => updateDockerRegistry(index, 'server', e.target.value)}
                    placeholder="ghcr.io"
                    className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                  />
                </div>
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id={`insecure-${index}`}
                    checked={registry.insecure}
                    onChange={(e) => updateDockerRegistry(index, 'insecure', e.target.checked)}
                    className="w-3 h-3 rounded"
                  />
                  <label htmlFor={`insecure-${index}`} className="text-xs text-gray-500 dark:text-gray-400">
                    HTTP（insecure）レジストリ
                  </label>
                </div>
                <div>
                  <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">
                    K8s Secret 名（docker config JSON）
                  </label>
                  <input
                    type="text"
                    value={registry.secretName}
                    onChange={(e) => updateDockerRegistry(index, 'secretName', e.target.value)}
                    placeholder="my-registry-secret"
                    className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                  />
                </div>
                {!registry.secretName && (
                  <>
                    <div>
                      <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">ユーザー名</label>
                      <input
                        type="text"
                        value={registry.username}
                        onChange={(e) => updateDockerRegistry(index, 'username', e.target.value)}
                        placeholder="myuser"
                        className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                      />
                    </div>
                    <div>
                      <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">
                        パスワード / アクセストークン
                      </label>
                      <input
                        type="password"
                        value={registry.password}
                        onChange={(e) => updateDockerRegistry(index, 'password', e.target.value)}
                        placeholder="••••••••"
                        className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                      />
                    </div>
                  </>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

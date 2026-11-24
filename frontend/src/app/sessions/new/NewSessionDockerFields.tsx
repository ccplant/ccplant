'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionDockerFields(context: NewSessionFormState) {
  const {
    isCreating,
    dockerEnabled,
    setDockerEnabled,
    dockerRegistries,
    addDockerRegistry,
    removeDockerRegistry,
    updateDockerRegistry,
  } = context
  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <p className="text-xs font-medium text-gray-600 dark:text-gray-400">Docker in Docker (DinD)</p>
        <label className="flex items-center gap-2 cursor-pointer">
          <span className="text-xs text-gray-400 dark:text-gray-500">{dockerEnabled ? '有効' : '無効'}</span>
          <button
            type="button"
            role="switch"
            aria-checked={dockerEnabled}
            onClick={() => setDockerEnabled(!dockerEnabled)}
            disabled={isCreating}
            className={`relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-1 disabled:opacity-50 disabled:cursor-not-allowed ${
              dockerEnabled ? 'bg-blue-500' : 'bg-gray-300 dark:bg-gray-600'
            }`}
          >
            <span
              className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                dockerEnabled ? 'translate-x-4' : 'translate-x-0'
              }`}
            />
          </button>
        </label>
      </div>
      <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">
        セッション Pod に docker:dind サイドカーを追加し、
        <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">DOCKER_HOST</code> を自動設定します。
      </p>
      {dockerEnabled && (
        <div className="pl-1 space-y-3">
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium text-gray-600 dark:text-gray-400">認証済みレジストリ（任意）</p>
            <button
              type="button"
              onClick={addDockerRegistry}
              disabled={isCreating}
              className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700 disabled:opacity-50"
            >
              + 追加
            </button>
          </div>
          {dockerRegistries.length === 0 && (
            <p className="text-xs text-gray-400 dark:text-gray-500">レジストリ認証が不要な場合は追加不要です。</p>
          )}
          {dockerRegistries.map((registry, index) => (
            <div key={index} className="border border-gray-200 dark:border-gray-600 rounded-lg p-3 space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-gray-600 dark:text-gray-400">レジストリ #{index + 1}</span>
                <button
                  type="button"
                  onClick={() => removeDockerRegistry(index)}
                  disabled={isCreating}
                  className="text-xs text-red-500 hover:text-red-600 disabled:opacity-50"
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
                  disabled={isCreating}
                  className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                />
              </div>
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id={`insecure-${index}`}
                  checked={registry.insecure}
                  onChange={(e) => updateDockerRegistry(index, 'insecure', e.target.checked)}
                  disabled={isCreating}
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
                  disabled={isCreating}
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
                      disabled={isCreating}
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
                      disabled={isCreating}
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
  )
}

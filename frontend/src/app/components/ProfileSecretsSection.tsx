'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  'name' | 'availableSecrets' | 'selectedSecretIds' | 'setSelectedSecretIds' | 'setDirty'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileSecretsSection({ availableSecrets, selectedSecretIds, setSelectedSecretIds, setDirty }: Props) {
  return (
    <div className="space-y-5">
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">参照するシークレット</label>
        <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
          このプロファイルで開始するセッションへ投影するシークレットを選択します。値は表示されません。
        </p>
        {availableSecrets.length === 0 ? (
          <div className="rounded-lg bg-gray-50 py-8 text-center text-sm text-gray-500 dark:bg-gray-800 dark:text-gray-400">
            利用できるシークレットがありません。設定の「シークレット」から追加してください。
          </div>
        ) : (
          <div className="space-y-2">
            {availableSecrets.map((secret) => {
              const checked = selectedSecretIds.includes(secret.id)
              return (
                <label
                  key={secret.id}
                  className="flex cursor-pointer items-start gap-3 rounded-lg border border-gray-200 p-3 hover:border-gray-300 dark:border-gray-700 dark:hover:border-gray-600"
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => {
                      setSelectedSecretIds((ids) =>
                        checked ? ids.filter((id) => id !== secret.id) : [...ids, secret.id]
                      )
                      setDirty(true)
                    }}
                    className="mt-1 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                  />
                  <span className="min-w-0">
                    <span className="block text-sm font-medium text-gray-900 dark:text-white">{secret.name}</span>
                    <span className="mt-0.5 block truncate text-xs text-gray-500">キー: {secret.keys.join(', ')}</span>
                  </span>
                </label>
              )
            })}
          </div>
        )}
        {selectedSecretIds.length === 0 && availableSecrets.length > 0 && (
          <p className="mt-3 text-xs text-gray-500">
            未選択の場合、既存プロファイルとの互換性のため、この設定スコープで投影が設定されたすべてのシークレットを使用します。
          </p>
        )}
      </div>
    </div>
  )
}

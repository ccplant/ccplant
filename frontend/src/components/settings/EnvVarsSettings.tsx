'use client'

import { useEnvVarsSettings } from './useEnvVarsSettings'

interface EnvVarsSettingsProps {
  envVarKeys?: string[]  // 環境変数のキーのみ（読み取り時）
  onChange: (envVars: Record<string, string>) => void
}

export function EnvVarsSettings({ envVarKeys, onChange }: EnvVarsSettingsProps) {
  const {
    closeEditor, editKey, editValue, editingEntry, existingKeys, handleDelete,
    handleSave, isModalOpen, keyError, openEditor, setEditKey, setEditValue, setKeyError,
  } = useEnvVarsSettings(envVarKeys, onChange)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-4">
        <div>
          <p className="text-sm text-gray-600 dark:text-gray-400">
            セッション起動時に自動的に読み込まれるカスタム環境変数を設定します。
            <br />
            <span className="text-yellow-600 dark:text-yellow-400">
              ⚠️ セキュリティのため、既存の環境変数の値は表示されません（キーのみ表示）
            </span>
          </p>
        </div>
        <button
          onClick={() => openEditor({ key: '', value: '', isNew: true })}
          className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors text-sm"
        >
          + 追加
        </button>
      </div>

      {existingKeys.length === 0 ? (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-gray-800 rounded-lg">
          環境変数が設定されていません
        </div>
      ) : (
        <div className="space-y-2">
          {existingKeys.map((key) => (
            <div
              key={key}
              className="flex items-center justify-between p-4 border border-gray-200 dark:border-gray-700 rounded-lg hover:border-gray-300 dark:hover:border-gray-600 transition-colors"
            >
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-lg">🔒</span>
                  <div>
                    <p className="font-medium text-gray-900 dark:text-white font-mono">
                      {key}
                    </p>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      値: ••••••••（セキュリティのため非表示）
                    </p>
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => openEditor({ key, value: '', isNew: false })}
                  className="px-3 py-1 text-sm text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
                >
                  編集
                </button>
                <button
                  onClick={() => handleDelete(key)}
                  className="px-3 py-1 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 rounded transition-colors"
                >
                  削除
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Modal */}
      {isModalOpen && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-2xl w-full mx-4 max-h-[90vh] overflow-y-auto">
            <div className="p-6">
              <h3 className="text-xl font-bold text-gray-900 dark:text-white mb-4">
                {editingEntry?.isNew ? '環境変数を追加' : '環境変数を編集'}
              </h3>

              <div className="space-y-4">
                {/* 環境変数名 */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    環境変数名 *
                  </label>
                  <input
                    type="text"
                    value={editKey}
                    onChange={(e) => {
                      setEditKey(e.target.value)
                      setKeyError('')
                    }}
                    disabled={!editingEntry?.isNew}
                    placeholder="例: MY_API_KEY"
                    className={`w-full px-3 py-2 border rounded-md font-mono ${
                      keyError
                        ? 'border-red-500 focus:ring-red-500'
                        : 'border-gray-300 dark:border-gray-600 focus:ring-blue-500'
                    } dark:bg-gray-700 dark:text-white focus:ring-2 focus:border-transparent ${
                      !editingEntry?.isNew ? 'bg-gray-100 dark:bg-gray-900 cursor-not-allowed' : ''
                    }`}
                  />
                  {keyError && (
                    <p className="mt-1 text-sm text-red-600 dark:text-red-400">{keyError}</p>
                  )}
                  {!editingEntry?.isNew && (
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      環境変数名は変更できません。変更する場合は削除して再作成してください。
                    </p>
                  )}
                </div>

                {/* 環境変数の値 */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    値 *
                  </label>
                  <textarea
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    placeholder="環境変数の値を入力してください"
                    rows={3}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md dark:bg-gray-700 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent font-mono text-sm"
                  />
                  {!editingEntry?.isNew && (
                    <p className="mt-1 text-xs text-yellow-600 dark:text-yellow-400">
                      ⚠️ セキュリティのため、既存の値は表示されません。新しい値を入力してください。
                    </p>
                  )}
                </div>
              </div>

              <div className="flex justify-end gap-3 mt-6">
                <button
                  onClick={closeEditor}
                  className="px-4 py-2 text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-md hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
                >
                  キャンセル
                </button>
                <button
                  onClick={handleSave}
                  className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors"
                >
                  保存
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

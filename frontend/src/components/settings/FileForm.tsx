'use client'

import { CreateFileRequest } from '../../types/user_file'

export interface FileFormProps {
  form: CreateFileRequest
  onChange: (f: CreateFileRequest) => void
  onSubmit: (e: React.FormEvent) => void
  onCancel: () => void
  submitting: boolean
  error: string | null
  isEdit: boolean
}

export function FileForm({ form, onChange, onSubmit, onCancel, submitting, error, isEdit }: FileFormProps) {
  const inputClass =
    'w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500'

  return (
    <form onSubmit={onSubmit} className="space-y-3">
      {/* Name */}
      <div>
        <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
          表示名 <span className="text-gray-400 font-normal">(任意)</span>
        </label>
        <input
          type="text"
          value={form.name ?? ''}
          onChange={(e) => onChange({ ...form, name: e.target.value })}
          placeholder="例: GitHub SSH Key"
          className={inputClass}
        />
      </div>

      {/* Path */}
      <div>
        <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
          配置パス <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={form.path}
          onChange={(e) => onChange({ ...form, path: e.target.value })}
          placeholder="例: /home/agentapi/.ssh/id_ed25519"
          required
          className={inputClass}
        />
        <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
          セッション内コンテナでのファイルパス
        </p>
      </div>

      {/* Permissions */}
      <div>
        <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
          パーミッション
        </label>
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={form.permissions ?? ''}
            onChange={(e) => onChange({ ...form, permissions: e.target.value })}
            placeholder="0600"
            className={`${inputClass} w-28 font-mono`}
          />
          <div className="flex gap-1">
            {(['0600', '0644', '0700', '0755'] as const).map((p) => (
              <button
                key={p}
                type="button"
                onClick={() => onChange({ ...form, permissions: p })}
                className={`px-2 py-1.5 text-xs rounded border font-mono transition-colors ${
                  form.permissions === p
                    ? 'bg-blue-600 text-white border-blue-600'
                    : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:border-blue-400 dark:hover:border-blue-600'
                }`}
              >
                {p}
              </button>
            ))}
          </div>
        </div>
        <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
          octal 形式で指定します。未指定の場合は 0600 が使われます。
        </p>
      </div>

      {/* Content */}
      <div>
        <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
          ファイル内容
        </label>
        <textarea
          value={form.content ?? ''}
          onChange={(e) => onChange({ ...form, content: e.target.value })}
          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;..."
          rows={8}
          className={`${inputClass} font-mono text-xs resize-y`}
        />
        <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
          SSH 秘密鍵などを貼り付けてください
        </p>
      </div>

      {/* Error */}
      {error && (
        <p className="text-xs text-red-500 dark:text-red-400">{error}</p>
      )}

      {/* Buttons */}
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={!form.path.trim() || submitting}
          className="px-3 py-1.5 text-xs bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors flex items-center gap-1.5"
        >
          {submitting && (
            <svg className="w-3 h-3 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          )}
          {submitting ? '保存中...' : isEdit ? '更新' : '追加'}
        </button>
        <button
          type="button"
          onClick={onCancel}
          disabled={submitting}
          className="px-3 py-1.5 text-xs bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-md hover:bg-gray-300 dark:hover:bg-gray-600 disabled:opacity-50 transition-colors"
        >
          キャンセル
        </button>
      </div>
    </form>
  )
}

'use client'

import { FileForm } from './FileForm'
import { FileSettingsList } from './FileSettingsList'
import { useFileSettings } from './useFileSettings'

interface FileSettingsProps {
  userName: string
}

export function FileSettings({ userName }: FileSettingsProps) {
  const {
    deletingIds, editing, error, fetchFiles, files, form, handleCancel, handleDelete,
    handleEdit, handleNew, handleSubmit, loading, setForm, showContent, submitError,
    submitting, toggleContent,
  } = useFileSettings(userName)

  const isFormOpen = editing.kind !== 'none'

  return (
    <div className="space-y-4">
      {/* Description */}
      <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
        <p className="text-xs text-blue-700 dark:text-blue-300">
          SSH 鍵や設定ファイルなど任意のファイルを登録すると、セッション起動時に指定したパスへ自動配置されます。パーミッションは octal 形式（例: <code className="bg-blue-100 dark:bg-blue-800 px-1 rounded">0600</code>）で指定でき、未指定の場合は <code className="bg-blue-100 dark:bg-blue-800 px-1 rounded">0600</code> が使われます。
        </p>
      </div>

      {/* Error */}
      {error && (
        <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
          <p className="text-xs text-red-600 dark:text-red-400">{error}</p>
          <button onClick={fetchFiles} className="mt-1 text-xs text-blue-600 dark:text-blue-400 hover:underline">
            再試行
          </button>
        </div>
      )}

      {/* Loading */}
      {loading && (
        <div className="flex items-center gap-2 py-4 text-sm text-gray-500 dark:text-gray-400">
          <svg className="w-4 h-4 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          読み込み中...
        </div>
      )}

      {/* File list */}
      {!loading && files.length === 0 && !isFormOpen && (
        <p className="text-sm text-gray-500 dark:text-gray-400">
          登録済みのファイルがありません
        </p>
      )}

      <FileSettingsList
        deletingIds={deletingIds}
        editing={editing}
        error={submitError}
        files={files}
        form={form}
        loading={loading}
        onCancel={handleCancel}
        onChange={setForm}
        onDelete={handleDelete}
        onEdit={handleEdit}
        onSubmit={handleSubmit}
        onToggleContent={toggleContent}
        showContent={showContent}
        submitting={submitting}
      />

      {/* New file form */}
      {editing.kind === 'new' && (
        <div className="p-3 border border-blue-200 dark:border-blue-800 rounded-lg bg-blue-50 dark:bg-blue-900/10 space-y-3">
          <p className="text-xs font-medium text-gray-700 dark:text-gray-300">新しいファイルを登録</p>
          <FileForm
            form={form}
            onChange={setForm}
            onSubmit={handleSubmit}
            onCancel={handleCancel}
            submitting={submitting}
            error={submitError}
            isEdit={false}
          />
        </div>
      )}

      {/* Add button */}
      {!isFormOpen && !loading && (
        <button
          type="button"
          onClick={handleNew}
          className="flex items-center gap-1 text-sm text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 transition-colors"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          ファイルを追加
        </button>
      )}
    </div>
  )
}

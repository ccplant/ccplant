'use client'

import { CreateFileRequest, UserFile } from '../../types/user_file'
import { FileForm } from './FileForm'

interface FileSettingsRowProps {
  contentVisible: boolean
  error: string | null
  file: UserFile
  form: CreateFileRequest
  isDeleting: boolean
  isEditing: boolean
  isFormOpen: boolean
  onCancel: () => void
  onChange: (form: CreateFileRequest) => void
  onDelete: (file: UserFile) => void
  onEdit: (file: UserFile) => void
  onSubmit: (event: React.FormEvent) => void
  onToggleContent: (id: string) => void
  submitting: boolean
}

export function FileSettingsRow(props: FileSettingsRowProps) {
  const {
    contentVisible, error, file, form, isDeleting, isEditing, isFormOpen,
    onCancel, onChange, onDelete, onEdit, onSubmit, onToggleContent, submitting,
  } = props

  return (
  <div
    key={file.id}
    className={`border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden transition-opacity ${isDeleting ? 'opacity-50' : ''}`}
  >
    {/* Row */}
    <div className="flex items-center gap-3 p-3">
      {/* Icon */}
      <div className="flex-shrink-0 w-7 h-7 rounded-full bg-gray-100 dark:bg-gray-700 flex items-center justify-center">
        <svg className="w-3.5 h-3.5 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
        </svg>
      </div>
      
      {/* Info */}
      <div className="flex-1 min-w-0">
        {file.name && (
          <p className="text-sm font-medium text-gray-900 dark:text-white truncate">
            {file.name}
          </p>
        )}
        <code className="text-xs font-mono text-gray-600 dark:text-gray-400 break-all">
          {file.path}
        </code>
        <div className="flex items-center gap-2 mt-0.5">
          {file.permissions && (
            <span className="text-xs text-gray-400 dark:text-gray-500">{file.permissions}</span>
          )}
          {file.content ? (
            <span className="text-xs text-green-600 dark:text-green-400">
              {file.content.length} bytes
            </span>
          ) : (
            <span className="text-xs text-gray-400 dark:text-gray-500">内容なし</span>
          )}
        </div>
      </div>
      
      {/* Actions */}
      <div className="flex items-center gap-1 flex-shrink-0">
        {file.content && (
          <button
            type="button"
            onClick={() => onToggleContent(file.id)}
            className="text-xs px-2 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:border-blue-300 dark:hover:border-blue-700 hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
            title={contentVisible ? '内容を隠す' : '内容を表示'}
          >
            {contentVisible ? '隠す' : '表示'}
          </button>
        )}
        <button
          type="button"
          onClick={() => onEdit(file)}
          disabled={isDeleting || isFormOpen}
          className="text-xs px-2 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:border-blue-300 dark:hover:border-blue-700 hover:text-blue-600 dark:hover:text-blue-400 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          編集
        </button>
        <button
          type="button"
          onClick={() => onDelete(file)}
          disabled={isDeleting || isFormOpen}
          className="text-xs px-2 py-1 rounded border border-red-200 dark:border-red-700 text-red-500 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          {isDeleting ? '削除中...' : '削除'}
        </button>
      </div>
    </div>
      
    {/* Content preview */}
    {contentVisible && file.content && (
      <div className="border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900 p-3">
        <pre className="text-xs font-mono text-gray-700 dark:text-gray-300 whitespace-pre-wrap break-all max-h-40 overflow-y-auto">
          {file.content}
        </pre>
      </div>
    )}
      
    {/* Inline edit form */}
    {isEditing && (
      <div className="border-t border-gray-200 dark:border-gray-700 bg-blue-50 dark:bg-blue-900/10 p-3">
        <FileForm
          form={form}
          onChange={onChange}
          onSubmit={onSubmit}
          onCancel={onCancel}
          submitting={submitting}
          error={error}
          isEdit
        />
      </div>
    )}
  </div>
)
}

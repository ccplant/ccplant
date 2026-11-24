'use client'
import { LoaderCircle, X } from 'lucide-react'
import type { SessionListState } from './useSessionListViewState'
export function SessionListTemplateDialog(context: SessionListState) {
  const {
    templateTarget,
    suspendingSession,
    setTemplateTarget,
    templateName,
    setTemplateName,
    templateDescription,
    setTemplateDescription,
    templateizeSession,
  } = context
  if (!templateTarget) return null
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-gray-950/50 px-4 py-8"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !suspendingSession) setTemplateTarget(null)
      }}
    >
      <form
        onSubmit={templateizeSession}
        className="w-full max-w-lg overflow-hidden rounded-xl bg-white shadow-2xl dark:bg-gray-900"
        role="dialog"
        aria-modal="true"
        aria-labelledby="template-dialog-title"
      >
        <div className="flex items-start justify-between gap-4 border-b border-gray-200 px-5 py-4 dark:border-gray-700">
          <div>
            <h2 id="template-dialog-title" className="text-lg font-semibold text-gray-900 dark:text-white">
              ワークスペースとして保存
            </h2>
            <p className="mt-1 text-sm text-gray-600 dark:text-gray-400">
              現在のファイル、設定、会話のコンテキストを再利用できる状態で保存します。
            </p>
          </div>
          <button
            type="button"
            onClick={() => setTemplateTarget(null)}
            disabled={Boolean(suspendingSession)}
            className="rounded-md p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-800 disabled:opacity-50 dark:hover:bg-gray-800 dark:hover:text-white"
            aria-label="閉じる"
          >
            <X className="h-5 w-5" aria-hidden="true" />
          </button>
        </div>
        <div className="space-y-4 px-5 py-5">
          <label className="block">
            <span className="text-sm font-medium text-gray-800 dark:text-gray-200">ワークスペース名</span>
            <input
              autoFocus
              required
              value={templateName}
              onChange={(event) => setTemplateName(event.target.value)}
              maxLength={120}
              className="mt-1.5 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 outline-none focus:border-amber-500 focus:ring-2 focus:ring-amber-200 dark:border-gray-600 dark:bg-gray-950 dark:text-white dark:focus:ring-amber-900"
            />
          </label>
          <label className="block">
            <span className="text-sm font-medium text-gray-800 dark:text-gray-200">
              用途のメモ <span className="font-normal text-gray-500">（任意）</span>
            </span>
            <textarea
              value={templateDescription}
              onChange={(event) => setTemplateDescription(event.target.value)}
              rows={3}
              maxLength={500}
              placeholder="例: API改修を始めるための依存関係と開発ツールを準備済み"
              className="mt-1.5 w-full resize-none rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 outline-none focus:border-amber-500 focus:ring-2 focus:ring-amber-200 dark:border-gray-600 dark:bg-gray-950 dark:text-white dark:focus:ring-amber-900"
            />
          </label>
          <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-100">
            保存後、このセッションは終了して一覧から消えます。保存したワークスペースは専用画面から何度でも使用できます。
          </div>
        </div>
        <div className="flex justify-end gap-2 border-t border-gray-200 bg-gray-50 px-5 py-3 dark:border-gray-700 dark:bg-gray-900">
          <button
            type="button"
            onClick={() => setTemplateTarget(null)}
            disabled={Boolean(suspendingSession)}
            className="rounded-md px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-200 disabled:opacity-50 dark:text-gray-300 dark:hover:bg-gray-800"
          >
            キャンセル
          </button>
          <button
            type="submit"
            disabled={!templateName.trim() || Boolean(suspendingSession)}
            className="inline-flex min-w-32 items-center justify-center gap-2 rounded-md bg-amber-700 px-4 py-2 text-sm font-medium text-white hover:bg-amber-800 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {suspendingSession && <LoaderCircle className="h-4 w-4 animate-spin" aria-hidden="true" />}
            {suspendingSession ? '保存中…' : '保存して終了'}
          </button>
        </div>
      </form>
    </div>
  )
}

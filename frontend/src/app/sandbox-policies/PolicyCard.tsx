'use client'
import { SandboxPolicy } from '../../types/sandbox_policy'
import CopyableResourceId from '../components/CopyableResourceId'
export function PolicyCard({
  policy,
  onEdit,
  onDelete,
  onImportDomains,
}: {
  policy: SandboxPolicy
  onEdit: () => void
  onDelete: () => void
  onImportDomains: () => void
}) {
  const domains = policy.allowed_domains?.length
    ? { mode: '許可リスト', list: policy.allowed_domains }
    : policy.denied_domains?.length
      ? { mode: '拒否リスト', list: policy.denied_domains }
      : null

  return (
    <div className="min-w-0 bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5 flex flex-col gap-3">
      <div>
        <div className="flex min-w-0 items-center gap-2">
          <h3 className="min-w-0 truncate font-semibold text-gray-900 dark:text-white">{policy.name}</h3>
          {policy.count_mode && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300">
              カウント
            </span>
          )}
        </div>
        {policy.description && <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">{policy.description}</p>}
      </div>
      <div className="flex flex-wrap gap-2">
        <button
          onClick={onImportDomains}
          title="検出ドメインを確認"
          className="text-xs px-2 py-1 rounded border border-blue-200 dark:border-blue-800 text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20"
        >
          検出ドメインを確認
        </button>
        <button
          onClick={onEdit}
          className="text-xs px-2 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700"
        >
          編集
        </button>
        <button
          onClick={onDelete}
          className="text-xs px-2 py-1 rounded border border-red-200 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20"
        >
          削除
        </button>
      </div>
      {domains ? (
        <div>
          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 mb-2">
            {domains.mode}
          </span>
          <ul className="text-xs font-mono text-gray-600 dark:text-gray-400 space-y-0.5">
            {domains.list.slice(0, 5).map((d) => (
              <li key={d} className="truncate">
                {d}
              </li>
            ))}
            {domains.list.length > 5 && <li className="text-gray-400">... 他 {domains.list.length - 5} 件</li>}
          </ul>
        </div>
      ) : (
        <p className="text-xs text-gray-400">ドメイン未設定</p>
      )}
      <CopyableResourceId id={policy.id} />
    </div>
  )
}

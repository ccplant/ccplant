'use client'

import type { SessionPoolSupplier } from '@/types/session_pool'

export function PoolSupplierControls({
  supplier,
  onPatch,
}: {
  supplier: SessionPoolSupplier
  onPatch: (patch: { enabled?: boolean; draining?: boolean }) => void
}) {
  const active = supplier.enabled && !supplier.draining

  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${active ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300' : 'bg-gray-200 text-gray-700 dark:bg-gray-800 dark:text-gray-300'}`}>
        {active ? '有効' : '無効'}
      </span>
      <button
        type="button"
        title={active ? '新規割り当てを止め、待機Runnerを削除します。実行中のセッションは継続します' : 'このManagerからの新規割り当てを有効にします'}
        onClick={() => onPatch({ enabled: !active, draining: false })}
        className="text-xs text-blue-700 hover:underline dark:text-blue-300"
      >
        {active ? '無効化' : '有効化'}
      </button>
    </div>
  )
}

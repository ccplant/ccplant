'use client'

import type { SessionPoolSupplier } from '@/types/session_pool'

export function PoolSupplierOperationGuide() {
  return (
    <div className="rounded-lg border border-blue-200 bg-blue-50 p-3 text-xs text-blue-950 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-100">
      <p className="font-semibold">退避と供給無効化の違い</p>
      <dl className="mt-2 grid gap-2 sm:grid-cols-2">
        <div>
          <dt className="font-medium">退避（Drain）</dt>
          <dd className="mt-0.5 text-blue-800 dark:text-blue-200">メンテナンスや割り当て解除の前に使う一時的な状態です。</dd>
        </div>
        <div>
          <dt className="font-medium">供給を無効化</dt>
          <dd className="mt-0.5 text-blue-800 dark:text-blue-200">このManagerからの供給設定を、再度有効にするまで停止します。</dd>
        </div>
      </dl>
      <p className="mt-2 text-blue-800 dark:text-blue-200">どちらも新規セッションの割り当てを止め、待機Runnerを削除します。実行中のセッションは終了まで継続します。</p>
    </div>
  )
}

export function PoolSupplierControls({
  supplier,
  onPatch,
}: {
  supplier: SessionPoolSupplier
  onPatch: (patch: { enabled?: boolean; draining?: boolean }) => void
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${supplier.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300' : 'bg-gray-200 text-gray-700 dark:bg-gray-800 dark:text-gray-300'}`}>
        {supplier.enabled ? '供給有効' : '供給無効'}
      </span>
      {supplier.draining && <span className="rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">退避中</span>}
      <button
        type="button"
        title={supplier.draining
          ? supplier.enabled
            ? '退避状態を解除し、新規セッションの割り当てを再開します'
            : '退避状態を解除します。供給は無効のため、新規割り当ては再開しません'
          : '新規割り当てを止め、待機Runnerを削除します。実行中のセッションは継続します'}
        onClick={() => onPatch({ draining: !supplier.draining })}
        className="text-xs text-amber-700 hover:underline dark:text-amber-300"
      >
        {supplier.draining ? '退避を解除' : '退避を開始'}
      </button>
      <button
        type="button"
        title={supplier.enabled ? 'このManagerからの供給を無効にします。実行中のセッションは継続します' : 'このManagerからの供給を有効にします'}
        onClick={() => onPatch({ enabled: !supplier.enabled })}
        className="text-xs text-blue-700 hover:underline dark:text-blue-300"
      >
        {supplier.enabled ? '供給を無効化' : '供給を有効化'}
      </button>
    </div>
  )
}

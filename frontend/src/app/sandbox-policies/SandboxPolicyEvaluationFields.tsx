'use client'
import type { SandboxPolicySettingsState } from './useSandboxPolicySettings'
export function SandboxPolicyEvaluationFields(context: SandboxPolicySettingsState) {
  const { countMode, setCountMode } = context
  return (
    <section className="space-y-5 p-5 md:p-6">
      <div>
        <h2 className="text-base font-semibold text-gray-900 dark:text-white">評価</h2>
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
          ポリシーを実際にブロックへ反映するか、記録だけにするかを選びます。
        </p>
      </div>
      <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-gray-200 p-4 dark:border-gray-600">
        <input
          type="checkbox"
          checked={countMode}
          onChange={(e) => setCountMode(e.target.checked)}
          className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
        />
        <div>
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">カウントモード</span>
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
            ポリシーを評価してブロック対象ドメインを記録するが、トラフィックは実際にはブロックしない。本番適用前の監査に利用できる。
          </p>
        </div>
      </label>
    </section>
  )
}

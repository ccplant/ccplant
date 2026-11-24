'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  'name' | 'sandboxPolicyId' | 'setSandboxPolicyId' | 'sandboxPolicies' | 'scope'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileSandboxSection({ sandboxPolicyId, setSandboxPolicyId, sandboxPolicies }: Props) {
  return (
    <div className="space-y-5">
      {/* Network Sandbox */}
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">sandbox policy</label>
        <select
          value={sandboxPolicyId}
          onChange={(e) => setSandboxPolicyId(e.target.value)}
          className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
        >
          <option value="">ポリシーなし（count mode）</option>
          {sandboxPolicies.map((policy) => (
            <option key={policy.id} value={policy.id}>
              {policy.name}
              {policy.scope === 'team' ? ' [チーム]' : ''}
            </option>
          ))}
        </select>
        {sandboxPolicies.length === 0 && (
          <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
            適用できる sandbox policy がありません。count mode で起動します。
          </p>
        )}
      </div>
    </div>
  )
}

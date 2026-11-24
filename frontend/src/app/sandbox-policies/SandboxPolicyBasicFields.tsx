'use client'
import type { SandboxPolicySettingsState } from './useSandboxPolicySettings'
export function SandboxPolicyBasicFields(context: SandboxPolicySettingsState) {
  const { name, setName, description, setDescription } = context
  return (
    <section className="space-y-5 p-5 md:p-6">
      <div>
        <h2 className="text-base font-semibold text-gray-900 dark:text-white">基本設定</h2>
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
          一覧やセッションプロファイルで識別するための情報です。
        </p>
      </div>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          名前 <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="例: github-access"
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          required
        />
      </div>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">説明</label>
        <input
          type="text"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="例: GitHub と npm へのアクセスを許可"
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>
    </section>
  )
}

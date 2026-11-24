'use client'
import type { SandboxPolicySettingsState } from './useSandboxPolicySettings'
export function SandboxPolicyDomainFields(context: SandboxPolicySettingsState) {
  const { mode, setMode, domains, setDomains } = context
  return (
    <section className="space-y-5 p-5 md:p-6">
      <div>
        <h2 className="text-base font-semibold text-gray-900 dark:text-white">ドメイン</h2>
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
          許可または拒否するドメインを 1 行ずつ指定します。
        </p>
      </div>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">モード</label>
        <div className="grid gap-3 sm:grid-cols-2">
          {(['allowlist', 'denylist'] as const).map((m) => (
            <label
              key={m}
              className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors ${
                mode === m
                  ? 'border-blue-400 bg-blue-50 dark:border-blue-600 dark:bg-blue-900/20'
                  : 'border-gray-200 hover:border-gray-300 dark:border-gray-600 dark:hover:border-gray-500'
              }`}
            >
              <input
                type="radio"
                value={m}
                checked={mode === m}
                onChange={() => setMode(m)}
                className="mt-0.5 h-4 w-4 text-blue-600 focus:ring-blue-500"
              />
              <span>
                <span className="block text-sm font-medium text-gray-800 dark:text-gray-200">
                  {m === 'allowlist' ? '許可リスト' : '拒否リスト'}
                </span>
                <span className="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">
                  {m === 'allowlist' ? '指定したドメインのみ許可します' : '指定したドメインをブロックします'}
                </span>
              </span>
            </label>
          ))}
        </div>
      </div>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          ドメインリスト <span className="text-gray-400 font-normal">（ワイルドカード *.example.com 可）</span>
        </label>
        <textarea
          value={domains}
          onChange={(e) => setDomains(e.target.value)}
          placeholder={
            mode === 'allowlist' ? 'github.com\n*.github.com\nregistry.npmjs.org' : 'example.com\nbad-domain.com'
          }
          rows={12}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono resize-y"
        />
      </div>
    </section>
  )
}

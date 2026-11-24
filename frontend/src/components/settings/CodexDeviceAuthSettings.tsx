'use client'

import { AuthPhase, DeviceAuthState, useCodexDeviceAuth } from './useCodexDeviceAuth'

interface Props { hasCredentials: boolean; onAuthComplete?: () => void; scope?: 'user' | 'team'; teamId?: string }

function StatusBadge({ phase, starting, hasCredentials }: { phase: AuthPhase; starting: boolean; hasCredentials: boolean }) {
  if (phase === 'authorized') return <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400"><svg className="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" /></svg>認証済み</span>
  if (phase === 'polling' || starting) return <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400"><span className="animate-spin rounded-full h-3 w-3 border-b-2 border-blue-600 dark:border-blue-400 mr-1" />{starting ? '開始中' : '待機中'}</span>
  if (hasCredentials) return <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300">設定済み</span>
  return null
}

function AuthFlow({ auth, copied, onCopy, onCancel }: { auth: DeviceAuthState; copied: boolean; onCopy: () => void; onCancel: () => void }) {
  return <div className="space-y-3 border border-blue-200 dark:border-blue-700 rounded-lg p-4 bg-blue-50 dark:bg-blue-900/10">
    <p className="text-sm font-medium text-gray-700 dark:text-gray-300">以下の手順で認証を完了してください：</p>
    <ol className="text-sm text-gray-600 dark:text-gray-400 space-y-2 list-decimal list-inside"><li>下記の URL にアクセスしてください：<a href={auth.verificationUri} target="_blank" rel="noopener noreferrer" className="block mt-1 text-blue-600 dark:text-blue-400 hover:underline font-mono text-xs break-all">{auth.verificationUri}</a></li><li>以下のコードを入力してください：<div className="mt-1 flex items-center gap-2"><code className="px-3 py-1.5 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded font-mono text-lg font-bold tracking-widest text-gray-900 dark:text-white">{auth.userCode}</code><button type="button" onClick={onCopy} className="text-xs px-2 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors">{copied ? 'コピー済み' : 'コピー'}</button></div></li></ol>
    <p className="text-xs text-gray-500 dark:text-gray-400">認証完了後、自動で検出されます...</p>
    <button type="button" onClick={onCancel} className="text-xs px-3 py-1.5 rounded border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors">キャンセル</button>
  </div>
}

export function CodexDeviceAuthSettings({ hasCredentials, onAuthComplete, scope = 'user', teamId }: Props) {
  const state = useCodexDeviceAuth({ scope, teamId, onAuthComplete })
  if (state.configured === null) return <div className="text-sm text-gray-400 dark:text-gray-500">確認中...</div>
  if (!state.configured) return <div className="p-3 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-lg"><p className="text-xs text-yellow-700 dark:text-yellow-300">デバイス認証を利用するには、プロキシサーバーに <code className="bg-yellow-100 dark:bg-yellow-800 px-1 rounded">codex</code> コマンドがインストールされている必要があります。</p></div>
  return <div className="space-y-4">
    <div className="flex items-center justify-between"><div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300">デバイス認証</label><p className="text-xs text-gray-500 dark:text-gray-400">codex コマンドを使って認証を行います。{scope === 'team' ? `${teamId} の共有 auth.json` : '個人の auth.json'} として保存されます。</p></div><StatusBadge phase={state.phase} starting={state.starting} hasCredentials={hasCredentials} /></div>
    {(state.phase === 'error' || state.phase === 'denied') && <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg"><p className="text-xs text-red-700 dark:text-red-300">{state.phase === 'denied' ? '認証が拒否されました。もう一度お試しください。' : state.error || '認証エラーが発生しました。'}</p></div>}
    {state.phase === 'authorized' && <div className="p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg"><p className="text-xs text-green-700 dark:text-green-300">認証が完了しました。Codex の認証情報が保存されました。</p></div>}
    {state.phase === 'polling' && state.deviceAuth && <AuthFlow auth={state.deviceAuth} copied={state.copied} onCopy={state.copyCode} onCancel={state.cancel} />}
    {state.phase !== 'polling' && <div className="border-t border-gray-200 dark:border-gray-700 pt-4"><button type="button" onClick={state.start} disabled={state.starting} className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed transition-colors">{state.starting && <span className="animate-spin rounded-full h-3.5 w-3.5 border-b-2 border-white" />}{state.phase === 'authorized' ? '再認証する' : 'デバイス認証を開始'}</button></div>}
  </div>
}

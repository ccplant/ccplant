'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { RefreshCw, Terminal, X } from 'lucide-react'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import type { SessionPoolLogs } from '../../types/session_pool'

interface SessionLogsDialogProps {
  sessionId: string
  onClose: () => void
}

export default function SessionLogsDialog({ sessionId, onClose }: SessionLogsDialogProps) {
  const client = useMemo(() => createAgentAPIProxyClientFromStorage(), [])
  const [result, setResult] = useState<SessionPoolLogs | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      setResult(await client.getSessionLogs(sessionId, 500))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'ログの取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }, [client, sessionId])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [onClose])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-3 sm:p-6" role="dialog" aria-modal="true" aria-label="セッション Pod ログ" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <div className="flex max-h-[90vh] w-full max-w-5xl flex-col overflow-hidden rounded-xl border border-gray-700 bg-gray-950 text-gray-100 shadow-2xl">
        <div className="flex items-center justify-between gap-3 border-b border-gray-800 px-4 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-medium"><Terminal className="h-4 w-4" />セッション Pod ログ</div>
            <p className="truncate font-mono text-xs text-gray-400">{result?.source || sessionId}</p>
          </div>
          <div className="flex items-center gap-1">
            <button type="button" onClick={() => void load()} disabled={loading} className="rounded-md p-2 text-gray-400 hover:bg-gray-800 hover:text-white disabled:opacity-50" title="更新"><RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /></button>
            <button type="button" onClick={onClose} className="rounded-md p-2 text-gray-400 hover:bg-gray-800 hover:text-white" title="閉じる"><X className="h-4 w-4" /></button>
          </div>
        </div>
        {error ? <div className="m-4 rounded-md border border-red-800 bg-red-950/50 p-3 text-sm text-red-300">{error}</div> : (
          <pre className="min-h-64 flex-1 overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-xs leading-5 text-gray-300">{loading && !result ? 'ログを読み込み中...' : result?.lines.length ? result.lines.join('\n') : 'ログはありません'}</pre>
        )}
      </div>
    </div>
  )
}

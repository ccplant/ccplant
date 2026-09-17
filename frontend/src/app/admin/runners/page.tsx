'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { RefreshCw, Terminal } from 'lucide-react'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import { AdminSessionRunner, SessionPoolLogs } from '@/types/session_pool'
import {
  ItemList,
  ItemListEmpty,
  ItemListRow,
  RowAction,
  SettingsPageHeader,
  SettingsSubsection,
  StatusBadge,
} from '@/components/settings'

const statusTone = (status: AdminSessionRunner['status']) => {
  if (status === 'running' || status === 'idle') return 'green' as const
  if (status === 'claiming') return 'blue' as const
  if (status === 'draining') return 'amber' as const
  return 'neutral' as const
}

function RunnerLogPanel({ runner, result, onClose }: {
  runner: AdminSessionRunner
  result: SessionPoolLogs | null
  onClose: () => void
}) {
  return (
    <div className="mt-3 rounded-lg border border-gray-700 bg-gray-950 p-3 text-gray-100">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate font-mono text-xs">{runner.id}</p>
          {result?.source && <p className="truncate font-mono text-[10px] text-gray-500">{result.source}</p>}
        </div>
        <button type="button" onClick={onClose} className="text-xs text-gray-400 hover:text-white">閉じる</button>
      </div>
      <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded bg-black p-2 font-mono text-[11px] leading-4 text-gray-300">
        {result ? (result.lines.length ? result.lines.join('\n') : 'ログはありません') : 'ログを読み込み中...'}
      </pre>
    </div>
  )
}

export default function AdminRunnersPage() {
  const client = useMemo(() => createCurrentDeploymentAgentAPIProxyClient(), [])
  const [runners, setRunners] = useState<AdminSessionRunner[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [logRunner, setLogRunner] = useState<AdminSessionRunner | null>(null)
  const [logs, setLogs] = useState<SessionPoolLogs | null>(null)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      setRunners(await client.listAdminSessionRunners())
      setError('')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Runner の読み込みに失敗しました')
    } finally {
      setLoading(false)
    }
  }, [client])

  useEffect(() => { void reload() }, [reload])

  const showLogs = async (runner: AdminSessionRunner) => {
    setLogRunner(runner)
    setLogs(null)
    setError('')
    try {
      setLogs(await client.getAdminSessionRunnerLogs(runner.id, runner.manager_id))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Runner ログの取得に失敗しました')
    }
  }

  return (
    <>
      <SettingsPageHeader
        title="Runners"
        description="全 Session Manager の Runner、作成元 Pool、状態、紐づく Session を確認します。"
        action={
          <button type="button" onClick={() => void reload()} disabled={loading} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> 更新
          </button>
        }
      />

      {error && <div role="alert" className="mb-5 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-600 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">{error}</div>}

      <SettingsSubsection title="Runner 一覧" description={`${runners.length} runners`}>
        <ItemList>
          {!loading && runners.length === 0 && <ItemListEmpty>Runner はありません</ItemListEmpty>}
          {loading && runners.length === 0 && <ItemListEmpty>Runner を読み込み中...</ItemListEmpty>}
          {runners.map((runner) => (
            <ItemListRow
              key={`${runner.manager_id}:${runner.id}`}
              name={runner.id}
              meta={`${runner.manager_name || runner.manager_id} · ${runner.manager_id}`}
              badges={<>
                <StatusBadge tone={statusTone(runner.status)}>{runner.status}</StatusBadge>
                {runner.from_pool
                  ? <StatusBadge tone="blue">Pool: {runner.pool}</StatusBadge>
                  : <StatusBadge tone="amber">Pool から作成されていません</StatusBadge>}
                {!runner.online && <StatusBadge tone="neutral">Manager 未確認</StatusBadge>}
              </>}
              actions={<RowAction onClick={() => void showLogs(runner)} disabled={!runner.online}><span className="inline-flex items-center gap-1"><Terminal className="h-3 w-3" />ログ</span></RowAction>}
            >
              <dl className="mt-3 grid gap-2 text-xs sm:grid-cols-2">
                <div><dt className="text-gray-500 dark:text-gray-400">Session</dt><dd className="break-all font-mono text-gray-800 dark:text-gray-200">{runner.session_id || '紐づいていません'}</dd></div>
                <div><dt className="text-gray-500 dark:text-gray-400">Last seen</dt><dd className="text-gray-800 dark:text-gray-200">{runner.last_seen ? new Date(runner.last_seen).toLocaleString() : '—'}</dd></div>
              </dl>
              {logRunner?.manager_id === runner.manager_id && logRunner.id === runner.id && <RunnerLogPanel runner={runner} result={logs} onClose={() => { setLogRunner(null); setLogs(null) }} />}
            </ItemListRow>
          ))}
        </ItemList>
      </SettingsSubsection>
    </>
  )
}

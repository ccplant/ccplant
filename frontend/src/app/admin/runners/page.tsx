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

const runnerKey = (runner: AdminSessionRunner) => `${runner.manager_id}:${runner.id}`

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
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [deleting, setDeleting] = useState<Set<string>>(new Set())

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const nextRunners = await client.listAdminSessionRunners()
      const nextKeys = new Set(nextRunners.map(runnerKey))
      setRunners(nextRunners)
      setSelected((current) => new Set([...current].filter((key) => nextKeys.has(key))))
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

  const deleteRunner = async (runner: AdminSessionRunner) => {
    if (!window.confirm(`Runner「${runner.id}」を削除しますか？Pod、Service、PVC、関連する Secret と Session の紐づきも削除されます。`)) return
    setError('')
    const key = runnerKey(runner)
    setDeleting((current) => new Set(current).add(key))
    try {
      await client.deleteAdminSessionRunner(runner.id, runner.manager_id)
      if (logRunner?.manager_id === runner.manager_id && logRunner.id === runner.id) {
        setLogRunner(null)
        setLogs(null)
      }
      setSelected((current) => {
        const next = new Set(current)
        next.delete(key)
        return next
      })
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Runner の削除に失敗しました')
    } finally {
      setDeleting((current) => {
        const next = new Set(current)
        next.delete(key)
        return next
      })
    }
  }

  const selectedRunners = runners.filter((runner) => selected.has(runnerKey(runner)))
  const allSelected = runners.length > 0 && selectedRunners.length === runners.length

  const toggleRunner = (runner: AdminSessionRunner) => {
    const key = runnerKey(runner)
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(runners.map(runnerKey)))
  }

  const deleteSelected = async () => {
    if (selectedRunners.length === 0) return
    if (!window.confirm(`選択した ${selectedRunners.length} 件の Runner を削除しますか？Pod、Service、PVC、関連する Secret と Session の紐づきも削除されます。`)) return
    const targets = [...selectedRunners]
    const keys = new Set(targets.map(runnerKey))
    setError('')
    setDeleting((current) => new Set([...current, ...keys]))
    const results = await Promise.allSettled(
      targets.map((runner) => client.deleteAdminSessionRunner(runner.id, runner.manager_id)),
    )
    const failed = results.filter((result) => result.status === 'rejected').length
    const succeededKeys = new Set(targets.filter((_, index) => results[index].status === 'fulfilled').map(runnerKey))
    setSelected((current) => new Set([...current].filter((key) => !succeededKeys.has(key))))
    if (logRunner && succeededKeys.has(runnerKey(logRunner))) {
      setLogRunner(null)
      setLogs(null)
    }
    setDeleting((current) => new Set([...current].filter((key) => !keys.has(key))))
    await reload()
    if (failed > 0) setError(`${targets.length} 件中 ${failed} 件の Runner を削除できませんでした`)
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
        {runners.length > 0 && <div className="mb-3 flex flex-wrap items-center gap-3 rounded-lg border border-gray-200 bg-white px-4 py-3 dark:border-gray-700 dark:bg-gray-900">
          <label className="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
            <input type="checkbox" checked={allSelected} onChange={toggleAll} disabled={deleting.size > 0} className="h-4 w-4 rounded border-gray-300 text-blue-600" />
            すべて選択
          </label>
          <span className="text-xs text-gray-500 dark:text-gray-400">{selectedRunners.length} 件選択中</span>
          <button type="button" onClick={() => void deleteSelected()} disabled={selectedRunners.length === 0 || deleting.size > 0} className="ml-auto rounded-md border border-red-200 px-3 py-1.5 text-sm font-medium text-red-600 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-900/20">
            {deleting.size > 0 ? '削除中...' : '選択した Runner を削除'}
          </button>
        </div>}
        <ItemList>
          {!loading && runners.length === 0 && <ItemListEmpty>Runner はありません</ItemListEmpty>}
          {loading && runners.length === 0 && <ItemListEmpty>Runner を読み込み中...</ItemListEmpty>}
          {runners.map((runner) => (
            <ItemListRow
              key={runnerKey(runner)}
              name={<span className="inline-flex items-center gap-2"><input type="checkbox" aria-label={`${runner.id}を選択`} checked={selected.has(runnerKey(runner))} onChange={() => toggleRunner(runner)} disabled={deleting.has(runnerKey(runner))} className="h-4 w-4 rounded border-gray-300 text-blue-600" /><span>{runner.id}</span></span>}
              meta={`${runner.manager_name || runner.manager_id} · ${runner.manager_id}`}
              badges={<>
                <StatusBadge tone={statusTone(runner.status)}>{runner.status}</StatusBadge>
                {runner.from_pool
                  ? <StatusBadge tone="blue">Pool: {runner.pool}</StatusBadge>
                  : <StatusBadge tone="amber">Pool から作成されていません</StatusBadge>}
                {!runner.online && <StatusBadge tone="neutral">Manager 未確認</StatusBadge>}
              </>}
              actions={<>
                <RowAction onClick={() => void showLogs(runner)} disabled={!runner.online}><span className="inline-flex items-center gap-1"><Terminal className="h-3 w-3" />ログ</span></RowAction>
                <RowAction tone="danger" onClick={() => void deleteRunner(runner)} disabled={deleting.has(runnerKey(runner))}>{deleting.has(runnerKey(runner)) ? '削除中...' : '削除'}</RowAction>
              </>}
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

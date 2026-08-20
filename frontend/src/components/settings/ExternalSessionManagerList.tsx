'use client'

import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { ExternalSessionManagerConfig, ExternalSessionManagerOperationalStatus } from '@/types/settings'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { ItemList, ItemListEmpty, ItemListRow, RowAction, StatusBadge } from './ui/ItemList'

interface ExternalSessionManagerListProps {
  managers: ExternalSessionManagerConfig[]
  onChange: (managers: ExternalSessionManagerConfig[]) => void
  /** 保存直後に一度だけ表示できる接続トークン */
  revealedTokens: Record<string, string>
  onRegenerate: (esmId: string) => void
  regeneratingEsmId: string | null
  scope: 'user' | 'team'
  teamId?: string
}

interface EditForm {
  name: string
  pool: string
  pool_enabled: boolean
  automatic_assignment_enabled: boolean
}

const buildStartCommand = (token: string): string =>
  [
    'SESSION_MANAGER_ENABLED=true',
    'SESSION_MANAGER_UPSTREAM_URL=<parent-proxy-url>',
    `SESSION_MANAGER_CONNECTION_TOKEN=${token}`,
    `SESSION_MANAGER_HMAC_SECRET=${token}`,
    'AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL=<external-session-manager-url>',
    'agentapi-proxy server',
  ].join(' ')

export function ExternalSessionManagerList({
  managers,
  onChange,
  revealedTokens,
  onRegenerate,
  regeneratingEsmId,
  scope,
  teamId,
}: ExternalSessionManagerListProps) {
  const [editingIndex, setEditingIndex] = useState<number | null>(null)
  const [editForm, setEditForm] = useState<EditForm>({
    name: '',
    pool: '',
    pool_enabled: false,
    automatic_assignment_enabled: false,
  })
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [operationsId, setOperationsId] = useState<string | null>(null)
  const [operationalStatus, setOperationalStatus] = useState<ExternalSessionManagerOperationalStatus | null>(null)
  const [logs, setLogs] = useState<string[]>([])
  const [operationError, setOperationError] = useState<string | null>(null)
  const [operationBusy, setOperationBusy] = useState<string | null>(null)
  const [upgradeVersion, setUpgradeVersion] = useState('')

  const loadOperations = async (managerId: string) => {
    setOperationsId(managerId)
    setOperationBusy('load')
    setOperationError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const [status, logResult] = await Promise.all([
        client.getExternalSessionManagerOperationalStatus(managerId, scope, teamId),
        client.getExternalSessionManagerLogs(managerId, scope, teamId),
      ])
      setOperationalStatus(status)
      setLogs(logResult.lines)
      if (!upgradeVersion) setUpgradeVersion(status.version || '')
    } catch (error) {
      setOperationalStatus(null)
      setLogs([])
      setOperationError(error instanceof Error ? error.message : '運用情報の取得に失敗しました')
    } finally {
      setOperationBusy(null)
    }
  }

  const runOperation = async (managerId: string, operation: 'restart' | 'upgrade') => {
    if (operation === 'restart' && !window.confirm('Session Manager を再起動しますか？')) return
    if (operation === 'upgrade' && !window.confirm(`${upgradeVersion} にアップグレードしますか？`)) return
    setOperationBusy(operation)
    setOperationError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      if (operation === 'restart') await client.restartExternalSessionManager(managerId, scope, teamId)
      else await client.upgradeExternalSessionManager(managerId, upgradeVersion, scope, teamId)
      setOperationError(operation === 'restart' ? '再起動を受け付けました。数秒後に更新してください。' : 'アップグレードを受け付けました。完了後に再接続されます。')
    } catch (error) {
      setOperationError(error instanceof Error ? error.message : '操作に失敗しました')
    } finally {
      setOperationBusy(null)
    }
  }

  const startEdit = (index: number) => {
    const manager = managers[index]
    setEditingIndex(index)
    setEditForm({
      name: manager.name,
      pool: manager.pool ?? '',
      pool_enabled: manager.pool_enabled ?? false,
      automatic_assignment_enabled: manager.automatic_assignment_enabled ?? false,
    })
  }

  const saveEdit = () => {
    if (editingIndex === null || !editForm.name.trim() || !editForm.pool.trim()) return
    onChange(
      managers.map((manager, index) =>
        index === editingIndex
          ? {
              ...manager,
              name: editForm.name.trim(),
              pool: editForm.pool.trim(),
              pool_enabled: editForm.pool_enabled,
              automatic_assignment_enabled: editForm.automatic_assignment_enabled,
            }
          : manager
      )
    )
    setEditingIndex(null)
  }

  const toggleField = (index: number, field: 'pool_enabled' | 'automatic_assignment_enabled') => {
    onChange(
      managers.map((manager, current) =>
        current === index ? { ...manager, [field]: !manager[field] } : manager
      )
    )
  }

  const remove = (index: number) => {
    onChange(managers.filter((_, current) => current !== index))
    if (editingIndex === index) setEditingIndex(null)
  }

  const copy = async (key: string, text: string) => {
    await navigator.clipboard.writeText(text)
    setCopiedId(key)
    setTimeout(() => setCopiedId(null), 2000)
  }

  if (managers.length === 0) {
    return (
      <ItemList>
        <ItemListEmpty>External Session Manager は登録されていません</ItemListEmpty>
      </ItemList>
    )
  }

  return (
    <ItemList>
      {managers.map((manager, index) => {
        const token = manager.id ? revealedTokens[manager.id] : undefined
        const isEditing = editingIndex === index
        const heartbeatAge = manager.last_heartbeat_at
          ? Date.now() - new Date(manager.last_heartbeat_at).getTime()
          : Number.POSITIVE_INFINITY
        const heartbeatOnline = heartbeatAge < 45_000

        return (
          <ItemListRow
            key={manager.id || index}
            name={manager.name}
            meta={manager.id}
            badges={
              <>
                {manager.pool_enabled && <StatusBadge tone="blue">Pool 有効</StatusBadge>}
                {manager.automatic_assignment_enabled && (
                  <StatusBadge tone="amber">自動割り当て ON</StatusBadge>
                )}
                {manager.pool && <StatusBadge tone="violet">Pool: {manager.pool}</StatusBadge>}
                {(manager.has_connection_token || token) && (
                  <StatusBadge tone="green">token 設定済み</StatusBadge>
                )}
                <StatusBadge tone={heartbeatOnline ? 'green' : 'amber'}>
                  {heartbeatOnline ? 'Heartbeat 正常' : 'Heartbeat 未到達'}
                </StatusBadge>
                {manager.version && <StatusBadge tone="blue">v: {manager.version}</StatusBadge>}
                {typeof manager.active_sessions === 'number' && (
                  <StatusBadge tone="violet">稼働 {manager.active_sessions}</StatusBadge>
                )}
              </>
            }
            actions={
              !isEditing && (
                <>
                  <RowAction onClick={() => toggleField(index, 'pool_enabled')}>
                    {manager.pool_enabled ? 'Pool 停止' : 'Pool 有効化'}
                  </RowAction>
                  <RowAction onClick={() => toggleField(index, 'automatic_assignment_enabled')}>
                    {manager.automatic_assignment_enabled ? '自動停止' : '自動有効化'}
                  </RowAction>
                  <RowAction onClick={() => startEdit(index)}>編集</RowAction>
                  <RowAction onClick={() => manager.id && loadOperations(manager.id)} disabled={!manager.id}>
                    運用管理
                  </RowAction>
                  <RowAction
                    onClick={() => manager.id && onRegenerate(manager.id)}
                    disabled={!manager.id || regeneratingEsmId === manager.id}
                    title="接続トークンを再発行して保存"
                  >
                    {regeneratingEsmId === manager.id ? '再発行中...' : '再発行'}
                  </RowAction>
                  <RowAction tone="danger" onClick={() => remove(index)}>
                    削除
                  </RowAction>
                </>
              )
            }
          >
            {isEditing && (
              <div className="mt-3 space-y-3 rounded-md border border-blue-200 bg-blue-50 p-3 dark:border-blue-900 dark:bg-blue-900/10">
                <div>
                  <label className="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">
                    名前
                  </label>
                  <input
                    type="text"
                    value={editForm.name}
                    onChange={(e) => setEditForm((prev) => ({ ...prev, name: e.target.value }))}
                    className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-white"
                  />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">
                    Pool 名
                  </label>
                  <input
                    type="text"
                    value={editForm.pool}
                    onChange={(e) => setEditForm((prev) => ({ ...prev, pool: e.target.value }))}
                    className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-white"
                  />
                </div>
                <label className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    checked={editForm.pool_enabled}
                    onChange={(e) =>
                      setEditForm((prev) => ({ ...prev, pool_enabled: e.target.checked }))
                    }
                    className="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                  />
                  <span className="text-xs text-gray-700 dark:text-gray-300">
                    Pool の明示選択を有効にする
                  </span>
                </label>
                <label className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    checked={editForm.automatic_assignment_enabled}
                    onChange={(e) =>
                      setEditForm((prev) => ({
                        ...prev,
                        automatic_assignment_enabled: e.target.checked,
                      }))
                    }
                    className="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                  />
                  <span className="text-xs text-gray-700 dark:text-gray-300">
                    自動割り当てを有効にする
                  </span>
                </label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={saveEdit}
                    disabled={!editForm.name.trim() || !editForm.pool.trim()}
                    className="rounded-md bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    更新
                  </button>
                  <button
                    type="button"
                    onClick={() => setEditingIndex(null)}
                    className="rounded-md bg-gray-200 px-3 py-1.5 text-xs text-gray-700 transition-colors hover:bg-gray-300 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
                  >
                    キャンセル
                  </button>
                </div>
              </div>
            )}

            {token && manager.id && (
              <div className="mt-3 space-y-2 rounded-md border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-800 dark:bg-emerald-900/10">
                <p className="text-xs text-emerald-700 dark:text-emerald-300">
                  接続トークンは今だけ表示されます。必要な値をコピーしてください。
                </p>
                <div className="flex items-center gap-2">
                  <code className="min-w-0 flex-1 truncate font-mono text-xs text-gray-700 dark:text-gray-200">
                    {token}
                  </code>
                  <button
                    type="button"
                    onClick={() => copy(manager.id!, token)}
                    title="接続トークンをコピー"
                    className="flex-shrink-0 text-blue-600 transition-colors hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                  >
                    {copiedId === manager.id ? (
                      <Check className="h-3.5 w-3.5 text-green-500" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
                <button
                  type="button"
                  onClick={() => copy(`${manager.id}:command`, buildStartCommand(token))}
                  className="text-xs text-blue-600 transition-colors hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  {copiedId === `${manager.id}:command`
                    ? '起動コマンドをコピーしました'
                    : 'External Session Manager 起動コマンドをコピー'}
                </button>
              </div>
            )}

            {manager.id && operationsId === manager.id && (
              <div className="mt-3 space-y-3 rounded-md border border-gray-200 bg-gray-950 p-3 text-gray-100 dark:border-gray-700">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="text-xs">
                    {operationalStatus ? (
                      <span>接続中 · {operationalStatus.version} · uptime {Math.floor(operationalStatus.uptime_seconds / 60)}分 · {operationalStatus.active_sessions} sessions</span>
                    ) : (
                      <span>{operationBusy === 'load' ? '接続を確認中...' : '運用チャネル未接続'}</span>
                    )}
                  </div>
                  <div className="flex gap-2">
                    <button type="button" onClick={() => loadOperations(manager.id!)} disabled={operationBusy !== null} className="rounded bg-gray-700 px-2 py-1 text-xs hover:bg-gray-600 disabled:opacity-50">更新</button>
                    <button type="button" onClick={() => runOperation(manager.id!, 'restart')} disabled={operationBusy !== null || !operationalStatus} className="rounded bg-amber-700 px-2 py-1 text-xs hover:bg-amber-600 disabled:opacity-50">{operationBusy === 'restart' ? '再起動中...' : '再起動'}</button>
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  <input value={upgradeVersion} onChange={(e) => setUpgradeVersion(e.target.value)} placeholder="v1.20.0" className="rounded border border-gray-600 bg-gray-900 px-2 py-1 font-mono text-xs" />
                  <button type="button" onClick={() => runOperation(manager.id!, 'upgrade')} disabled={operationBusy !== null || !operationalStatus || !/^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(upgradeVersion)} className="rounded bg-blue-700 px-2 py-1 text-xs hover:bg-blue-600 disabled:opacity-50">{operationBusy === 'upgrade' ? '更新中...' : 'バージョンアップ'}</button>
                </div>
                {operationError && <p className="text-xs text-amber-300">{operationError}</p>}
                <div>
                  <p className="mb-1 text-xs text-gray-400">daemon logs (last 200 lines)</p>
                  <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded bg-black p-2 font-mono text-[11px] leading-4 text-gray-300">{logs.length ? logs.join('\n') : 'ログはありません'}</pre>
                </div>
              </div>
            )}
          </ItemListRow>
        )
      })}
    </ItemList>
  )
}

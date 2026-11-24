import { useState } from 'react'
import { ExternalSessionManagerConfig, ExternalSessionManagerOperationalStatus } from '@/types/settings'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'

interface Options { managers: ExternalSessionManagerConfig[]; onChange: (managers: ExternalSessionManagerConfig[]) => void; scope: 'user' | 'team'; teamId?: string }

export function useExternalSessionManagerList({ managers, onChange, scope, teamId }: Options) {
  const [editingIndex, setEditingIndex] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [operationsId, setOperationsId] = useState<string | null>(null)
  const [operationalStatus, setOperationalStatus] = useState<ExternalSessionManagerOperationalStatus | null>(null)
  const [logs, setLogs] = useState<string[]>([])
  const [operationError, setOperationError] = useState<string | null>(null)
  const [operationBusy, setOperationBusy] = useState<string | null>(null)
  const [upgradeVersion, setUpgradeVersion] = useState('')

  const loadOperations = async (managerId: string) => {
    setOperationsId(managerId); setOperationBusy('load'); setOperationError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const [status, result] = await Promise.all([client.getExternalSessionManagerOperationalStatus(managerId, scope, teamId), client.getExternalSessionManagerLogs(managerId, scope, teamId)])
      setOperationalStatus(status); setLogs(result.lines); setUpgradeVersion((current) => current || status.version || '')
    } catch (error) {
      setOperationalStatus(null); setLogs([]); setOperationError(error instanceof Error ? error.message : '運用情報の取得に失敗しました')
    } finally { setOperationBusy(null) }
  }
  const runOperation = async (managerId: string, operation: 'restart' | 'upgrade') => {
    const message = operation === 'restart' ? 'Session Manager を再起動しますか？' : `${upgradeVersion} にアップグレードしますか？`
    if (!window.confirm(message)) return
    setOperationBusy(operation); setOperationError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      if (operation === 'restart') await client.restartExternalSessionManager(managerId, scope, teamId)
      else await client.upgradeExternalSessionManager(managerId, upgradeVersion, scope, teamId)
      setOperationError(operation === 'restart' ? '再起動を受け付けました。数秒後に更新してください。' : 'アップグレードを受け付けました。完了後に再接続されます。')
    } catch (error) { setOperationError(error instanceof Error ? error.message : '操作に失敗しました') }
    finally { setOperationBusy(null) }
  }
  const startEdit = (index: number) => { setEditingIndex(index); setEditName(managers[index].name) }
  const saveEdit = () => {
    if (editingIndex === null || !editName.trim()) return
    onChange(managers.map((manager, index) => index === editingIndex ? { ...manager, name: editName.trim() } : manager)); setEditingIndex(null)
  }
  const remove = (index: number) => { onChange(managers.filter((_, current) => current !== index)); if (editingIndex === index) setEditingIndex(null) }
  const copy = async (key: string, text: string) => { await navigator.clipboard.writeText(text); setCopiedId(key); setTimeout(() => setCopiedId(null), 2000) }
  return { editingIndex, editName, setEditName, copiedId, operationsId, operationalStatus, logs, operationError, operationBusy, upgradeVersion, setUpgradeVersion, loadOperations, runOperation, startEdit, saveEdit, remove, copy, cancelEdit: () => setEditingIndex(null) }
}
export type ExternalManagerListState = ReturnType<typeof useExternalSessionManagerList>

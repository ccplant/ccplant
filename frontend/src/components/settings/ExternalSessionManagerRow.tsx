import { Check, Copy } from 'lucide-react'
import { ExternalSessionManagerConfig } from '@/types/settings'
import { ItemListRow, RowAction, StatusBadge } from './ui/ItemList'
import type { ExternalManagerListState } from './useExternalSessionManagerList'

const buildStartCommand = (token: string) => ['SESSION_MANAGER_ENABLED=true', 'SESSION_MANAGER_UPSTREAM_URL=<parent-proxy-url>', `SESSION_MANAGER_CONNECTION_TOKEN=${token}`, `SESSION_MANAGER_HMAC_SECRET=${token}`, 'AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL=<external-session-manager-url>', 'agentapi-proxy server'].join(' ')

function EditPanel({ state }: { state: ExternalManagerListState }) {
  return <div className="mt-3 space-y-3 rounded-md border border-blue-200 bg-blue-50 p-3 dark:border-blue-900 dark:bg-blue-900/10"><div><label className="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">名前</label><input type="text" value={state.editName} onChange={(event) => state.setEditName(event.target.value)} className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-white" /></div><div className="flex gap-2"><button type="button" onClick={state.saveEdit} disabled={!state.editName.trim()} className="rounded-md bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50">更新</button><button type="button" onClick={state.cancelEdit} className="rounded-md bg-gray-200 px-3 py-1.5 text-xs text-gray-700 transition-colors hover:bg-gray-300 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600">キャンセル</button></div></div>
}

function TokenPanel({ managerId, token, state }: { managerId: string; token: string; state: ExternalManagerListState }) {
  return <div className="mt-3 space-y-2 rounded-md border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-800 dark:bg-emerald-900/10">
    <p className="text-xs text-emerald-700 dark:text-emerald-300">接続トークンは今だけ表示されます。必要な値をコピーしてください。</p>
    <div className="flex items-center gap-2"><code className="min-w-0 flex-1 truncate font-mono text-xs text-gray-700 dark:text-gray-200">{token}</code><button type="button" onClick={() => state.copy(managerId, token)} title="接続トークンをコピー" className="flex-shrink-0 text-blue-600 transition-colors hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300">{state.copiedId === managerId ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}</button></div>
    <button type="button" onClick={() => state.copy(`${managerId}:command`, buildStartCommand(token))} className="text-xs text-blue-600 transition-colors hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300">{state.copiedId === `${managerId}:command` ? '起動コマンドをコピーしました' : 'External Session Manager 起動コマンドをコピー'}</button>
  </div>
}

function OperationsPanel({ managerId, state }: { managerId: string; state: ExternalManagerListState }) {
  const validVersion = /^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(state.upgradeVersion)
  return <div className="mt-3 space-y-3 rounded-md border border-gray-200 bg-gray-950 p-3 text-gray-100 dark:border-gray-700">
    <div className="flex flex-wrap items-center justify-between gap-2"><div className="text-xs">{state.operationalStatus ? <span>接続中 · {state.operationalStatus.version} · uptime {Math.floor(state.operationalStatus.uptime_seconds / 60)}分 · {state.operationalStatus.active_sessions} sessions</span> : <span>{state.operationBusy === 'load' ? '接続を確認中...' : '運用チャネル未接続'}</span>}</div><div className="flex gap-2"><button type="button" onClick={() => state.loadOperations(managerId)} disabled={state.operationBusy !== null} className="rounded bg-gray-700 px-2 py-1 text-xs hover:bg-gray-600 disabled:opacity-50">更新</button><button type="button" onClick={() => state.runOperation(managerId, 'restart')} disabled={state.operationBusy !== null || !state.operationalStatus} className="rounded bg-amber-700 px-2 py-1 text-xs hover:bg-amber-600 disabled:opacity-50">{state.operationBusy === 'restart' ? '再起動中...' : '再起動'}</button></div></div>
    <div className="flex flex-wrap gap-2"><input value={state.upgradeVersion} onChange={(event) => state.setUpgradeVersion(event.target.value)} placeholder="v1.20.0" className="rounded border border-gray-600 bg-gray-900 px-2 py-1 font-mono text-xs" /><button type="button" onClick={() => state.runOperation(managerId, 'upgrade')} disabled={state.operationBusy !== null || !state.operationalStatus || !validVersion} className="rounded bg-blue-700 px-2 py-1 text-xs hover:bg-blue-600 disabled:opacity-50">{state.operationBusy === 'upgrade' ? '更新中...' : 'バージョンアップ'}</button></div>
    {state.operationError && <p className="text-xs text-amber-300">{state.operationError}</p>}<div><p className="mb-1 text-xs text-gray-400">daemon logs (last 200 lines)</p><pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded bg-black p-2 font-mono text-[11px] leading-4 text-gray-300">{state.logs.length ? state.logs.join('\n') : 'ログはありません'}</pre></div>
  </div>
}

interface Props { manager: ExternalSessionManagerConfig; index: number; token?: string; state: ExternalManagerListState; onRegenerate: (id: string) => void; regeneratingEsmId: string | null }
export function ExternalSessionManagerRow({ manager, index, token, state, onRegenerate, regeneratingEsmId }: Props) {
  const editing = state.editingIndex === index
  const online = manager.last_heartbeat_at ? Date.now() - new Date(manager.last_heartbeat_at).getTime() < 45_000 : false
  const badges = <>{(manager.has_connection_token || token) && <StatusBadge tone="green">token 設定済み</StatusBadge>}<StatusBadge tone={online ? 'green' : 'amber'}>{online ? 'Heartbeat 正常' : 'Heartbeat 未到達'}</StatusBadge>{manager.version && <StatusBadge tone="blue">v: {manager.version}</StatusBadge>}{typeof manager.active_sessions === 'number' && <StatusBadge tone="violet">稼働 {manager.active_sessions}</StatusBadge>}</>
  const actions = !editing && <><RowAction onClick={() => state.startEdit(index)}>編集</RowAction><RowAction onClick={() => manager.id && state.loadOperations(manager.id)} disabled={!manager.id}>運用管理</RowAction><RowAction onClick={() => manager.id && onRegenerate(manager.id)} disabled={!manager.id || regeneratingEsmId === manager.id} title="接続トークンを再発行して保存">{regeneratingEsmId === manager.id ? '再発行中...' : '再発行'}</RowAction><RowAction tone="danger" onClick={() => state.remove(index)}>削除</RowAction></>
  return <ItemListRow name={manager.name} meta={manager.id} badges={badges} actions={actions}>{editing && <EditPanel state={state} />}{token && manager.id && <TokenPanel managerId={manager.id} token={token} state={state} />}{manager.id && state.operationsId === manager.id && <OperationsPanel managerId={manager.id} state={state} />}</ItemListRow>
}

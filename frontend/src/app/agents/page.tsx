'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import SessionProfileSelect from '../components/SessionProfileSelect'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { createACPServerClientFromStorage } from '../../lib/acp-server-client'
import { sendControllerCommand, waitForControllerReady } from '../../lib/controller-agent-runtime'
import { getACPServerEnabled } from '../../types/settings'
import {
  buildControllerBootstrapMessage,
  buildControllerMessage,
  createControllerAgent,
  loadControllerAgents,
  replaceControllerAgent,
  saveControllerAgents,
} from '../../lib/controller-agent-store'
import type { ControllerAgent, ControllerAgentRun } from '../../types/controller_agent'
import type { Session } from '../../types/agentapi'

const statusStyle: Record<ControllerAgent['status'], string> = {
  idle: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300',
  starting: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300',
  working: 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300',
  error: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300',
}

export default function AgentsPage() {
  const { getScopeParams } = useTeamScope()
  const [agents, setAgents] = useState<ControllerAgent[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [instructions, setInstructions] = useState('依頼を分解し、必要なら作業セッションを作成して、結果を統合してください。')
  const [profileId, setProfileId] = useState('')
  const [maxChildSessions, setMaxChildSessions] = useState(4)
  const [commands, setCommands] = useState<Record<string, string>>({})
  const [workers, setWorkers] = useState<Record<string, Session[]>>({})
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  useEffect(() => setAgents(loadControllerAgents()), [])

  useEffect(() => {
    const refreshWorkers = async () => {
      const currentAgents = loadControllerAgents()
      if (currentAgents.length === 0) return
      try {
        const sessions = (await createAgentAPIProxyClientFromStorage().search({ limit: 100 })).sessions || []
        setWorkers(Object.fromEntries(currentAgents.map((agent) => [
          agent.id,
          sessions.filter((session) => session.tags?.parent_agent_id === agent.id && session.tags?.session_role === 'worker'),
        ])))
      } catch {
        // Agent commands remain available while the session list is temporarily unavailable.
      }
    }
    void refreshWorkers()
    const interval = window.setInterval(refreshWorkers, 5000)
    return () => window.clearInterval(interval)
  }, [agents.length])

  const persist = (next: ControllerAgent[]) => {
    setAgents(next)
    saveControllerAgents(next)
  }

  const createAgent = async () => {
    if (!name.trim() || !instructions.trim()) return
    setCreating(true)
    setCreateError(null)
    const scope = getScopeParams()
    let controllerProfileId = ''
    let agent = createControllerAgent({
      name: name.trim(),
      description: description.trim(),
      instructions: instructions.trim(),
      source_session_profile_id: profileId || undefined,
      max_child_sessions: maxChildSessions,
      ...scope,
    })
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const headers: Record<string, string> = {
        Authorization: 'Bearer ${AGENTAPI_KEY}',
        'X-Session-ID': '${AGENTAPI_SESSION_ID}',
        'X-Agent-ID': agent.id,
        'X-Agent-Scope': scope.scope,
        'X-Max-Child-Sessions': String(maxChildSessions),
      }
      if (scope.team_id) headers['X-Agent-Team-ID'] = scope.team_id
      const profile = await client.createSessionProfile({
        name: `[Agent] ${agent.name}`,
        description: `Controller runtime profile for ${agent.name}`,
        ...scope,
        config: {
          source_session_profile_id: profileId || undefined,
          mcp_servers: {
            ccplant_sessions: {
              type: 'http',
              url: '${PROVISIONER_PROXY_URL}/mcp',
              headers,
            },
          },
        },
      })
      controllerProfileId = profile.id
      agent = { ...agent, session_profile_id: profile.id, status: 'starting' }
      const session = await client.start({
        params: { message: buildControllerBootstrapMessage(agent) },
        tags: { controller_agent_id: agent.id, controller_agent_name: agent.name, session_role: 'controller' },
        session_profile_id: profile.id,
        ...scope,
      })
      agent = { ...agent, controller_session_id: session.session_id, status: 'idle', updated_at: new Date().toISOString() }
      persist([agent, ...agents])
      setName('')
      setDescription('')
      setProfileId('')
      setMaxChildSessions(4)
      setShowCreate(false)
    } catch (error) {
      if (controllerProfileId) {
        await createAgentAPIProxyClientFromStorage().deleteSessionProfile(controllerProfileId).catch(() => undefined)
      }
      setCreateError(error instanceof Error ? error.message : 'Agentの作成に失敗しました')
    } finally {
      setCreating(false)
    }
  }

  const removeAgent = async (agent: ControllerAgent) => {
    if (!window.confirm(`「${agent.name}」とController Sessionを削除しますか？`)) return
    const client = createAgentAPIProxyClientFromStorage()
    if (agent.controller_session_id) await client.delete(agent.controller_session_id).catch(() => undefined)
    if (agent.session_profile_id) await client.deleteSessionProfile(agent.session_profile_id).catch(() => undefined)
    persist(agents.filter((item) => item.id !== agent.id))
  }

  const runAgent = async (agent: ControllerAgent) => {
    const command = commands[agent.id]?.trim()
    if (!command) return
    const now = new Date().toISOString()
    const run: ControllerAgentRun = { id: `${Date.now()}`, command, status: 'running', created_at: now }
    let current: ControllerAgent = { ...agent, status: agent.controller_session_id ? 'working' : 'starting', runs: [run, ...agent.runs], updated_at: now }
    persist(replaceControllerAgent(agents, current))
    setCommands((value) => ({ ...value, [agent.id]: '' }))

    try {
      const client = createAgentAPIProxyClientFromStorage()
      if (!current.controller_session_id) throw new Error('Controller Sessionがありません。Agentを作り直してください。')
      await waitForControllerReady(() => client.getSessionStatus(current.controller_session_id!))
      const message = buildControllerMessage(current, command, false)
      const globalACPEnabled = getACPServerEnabled()
      await sendControllerCommand({
        sessionId: current.controller_session_id,
        message,
        globalACPEnabled,
        restClient: client,
        acpServerClient: globalACPEnabled ? createACPServerClientFromStorage() : undefined,
      })
      current = { ...current, status: 'working', runs: current.runs.map((item) => item.id === run.id ? { ...item, status: 'submitted' } : item), updated_at: new Date().toISOString() }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : '指令の送信に失敗しました'
      current = { ...current, status: 'error', runs: current.runs.map((item) => item.id === run.id ? { ...item, status: 'failed', error: errorMessage } : item), updated_at: new Date().toISOString() }
    }
    const latest = loadControllerAgents()
    persist(replaceControllerAgent(latest, current))
  }

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar title="Agents" showSettingsButton>
        <div className="md:hidden"><NavigationTabs /></div>
      </TopBar>

      <div className="mx-auto max-w-6xl px-4 py-6 md:px-8 md:py-8">
        <div className="mb-6 flex items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">司令塔Agents</h1>
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">常駐するController Sessionが、必要に応じてWorker Sessionを自律的に起動します。</p>
          </div>
          <button onClick={() => setShowCreate((value) => !value)} className="rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700">
            {showCreate ? '閉じる' : 'Agentを作成'}
          </button>
        </div>

        <div className="mb-6 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
          PoC: Agent定義はこのブラウザに保存されます。ControllerとWorkerは実際のSessionとして動作します。
        </div>

        {showCreate && (
          <section className="mb-8 rounded-xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-800">
            <h2 className="mb-4 text-lg font-semibold text-gray-900 dark:text-white">新しいAgent</h2>
            <div className="grid gap-4 md:grid-cols-2">
              <label className="text-sm text-gray-700 dark:text-gray-200">名前
                <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Backend Lead" className="mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-gray-600 dark:bg-gray-900" />
              </label>
              <label className="text-sm text-gray-700 dark:text-gray-200">説明
                <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="バックエンド開発の司令塔" className="mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-gray-600 dark:bg-gray-900" />
              </label>
              <label className="text-sm text-gray-700 dark:text-gray-200 md:col-span-2">常設指示
                <textarea value={instructions} onChange={(e) => setInstructions(e.target.value)} rows={4} className="mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-gray-600 dark:bg-gray-900" />
              </label>
              <div className="md:col-span-2">
                <span className="mb-1 block text-sm text-gray-700 dark:text-gray-200">ControllerのベースProfile</span>
                <SessionProfileSelect value={profileId} onChange={setProfileId} />
              </div>
              <label className="text-sm text-gray-700 dark:text-gray-200">Worker Session上限
                <input type="number" min={1} max={16} value={maxChildSessions} onChange={(e) => setMaxChildSessions(Math.max(1, Math.min(16, Number(e.target.value) || 1)))} className="mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-gray-600 dark:bg-gray-900" />
              </label>
            </div>
            {createError && <p role="alert" className="mt-4 text-sm text-red-600 dark:text-red-400">{createError}</p>}
            <div className="mt-5 flex justify-end">
              <button disabled={creating || !name.trim() || !instructions.trim()} onClick={createAgent} className="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40">{creating ? 'Controllerを起動中…' : '作成して起動'}</button>
            </div>
          </section>
        )}

        {agents.length === 0 ? (
          <div className="rounded-xl border border-dashed border-gray-300 bg-white py-16 text-center dark:border-gray-700 dark:bg-gray-800">
            <p className="font-medium text-gray-700 dark:text-gray-200">まだAgentがありません</p>
            <p className="mt-1 text-sm text-gray-500">Agentを作り、最初の指令を送ってみてください。</p>
          </div>
        ) : (
          <div className="grid gap-5 lg:grid-cols-2">
            {agents.map((agent) => (
              <article key={agent.id} className="rounded-xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-800">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{agent.name}</h2>
                      <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${statusStyle[agent.status]}`}>{agent.status}</span>
                    </div>
                    <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">{agent.description || '説明なし'}</p>
                  </div>
                  <button onClick={() => removeAgent(agent)} className="text-xs text-gray-400 hover:text-red-600">削除</button>
                </div>

                <div className="mt-4 rounded-lg bg-gray-50 p-3 text-sm text-gray-600 dark:bg-gray-900/60 dark:text-gray-300">
                  <p className="line-clamp-3 whitespace-pre-wrap">{agent.instructions}</p>
                  <div className="mt-2 flex flex-wrap gap-2 text-xs text-gray-400">
                    <span>{agent.scope === 'team' ? agent.team_id : 'Personal'}</span>
                    <span>·</span><span>Worker上限 {agent.max_child_sessions}</span>
                    <span>·</span><span>{agent.source_session_profile_id ? 'ベースProfile指定' : '標準構成'}</span>
                  </div>
                </div>

                <div className="mt-4 flex gap-2">
                  <textarea aria-label={`${agent.name}への指令`} value={commands[agent.id] || ''} onChange={(e) => setCommands((value) => ({ ...value, [agent.id]: e.target.value }))} rows={2} placeholder="このAgentへの指令…" className="min-w-0 flex-1 resize-none rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" />
                  <button onClick={() => runAgent(agent)} disabled={!commands[agent.id]?.trim() || agent.runs.some((run) => run.status === 'running')} className="self-stretch rounded-lg bg-blue-600 px-4 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-40">指令</button>
                </div>

                {agent.controller_session_id && (
                  <div className="mt-3 flex items-center justify-between text-xs">
                    <span className="truncate text-gray-400">Session: {agent.controller_session_id}</span>
                    <Link href={`/sessions/${agent.controller_session_id}`} className="ml-3 shrink-0 font-medium text-blue-600 hover:text-blue-700">会話を開く →</Link>
                  </div>
                )}

                <div className="mt-4 border-t border-gray-100 pt-3 dark:border-gray-700">
                  <div className="mb-2 flex items-center justify-between">
                    <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Worker Sessions</p>
                    <span className="text-xs text-gray-400">{workers[agent.id]?.length || 0} / {agent.max_child_sessions}</span>
                  </div>
                  {(workers[agent.id]?.length || 0) === 0 ? (
                    <p className="text-sm text-gray-400">ControllerがWorkerを作成すると、ここに表示されます。</p>
                  ) : (workers[agent.id] || []).map((worker) => (
                    <Link key={worker.session_id} href={`/sessions/${worker.session_id}`} className="mb-2 flex items-center justify-between rounded-lg bg-gray-50 px-3 py-2 text-sm hover:bg-gray-100 dark:bg-gray-900/60 dark:hover:bg-gray-900">
                      <span className="truncate text-gray-600 dark:text-gray-300">{worker.annotations?.running_task || worker.description || worker.session_id}</span>
                      <span className="ml-3 shrink-0 text-xs text-gray-400">{worker.status}</span>
                    </Link>
                  ))}
                </div>

                {agent.runs.length > 0 && (
                  <div className="mt-4 border-t border-gray-100 pt-3 dark:border-gray-700">
                    <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-400">Recent runs</p>
                    {agent.runs.slice(0, 3).map((run) => (
                      <div key={run.id} className="mb-2">
                        <div className="flex items-start justify-between gap-3 text-sm">
                          <span className="line-clamp-1 text-gray-600 dark:text-gray-300">{run.command}</span>
                          <span className={run.status === 'failed' ? 'shrink-0 text-red-500' : 'shrink-0 text-gray-400'} title={run.error}>{run.status}</span>
                        </div>
                        {run.error && <p className="mt-1 break-words text-xs text-red-500">{run.error}</p>}
                      </div>
                    ))}
                  </div>
                )}
              </article>
            ))}
          </div>
        )}
      </div>
    </main>
  )
}

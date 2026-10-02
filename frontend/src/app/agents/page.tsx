'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import SessionProfileSelect from '../components/SessionProfileSelect'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import {
  buildControllerMessage,
  createControllerAgent,
  loadControllerAgents,
  replaceControllerAgent,
  saveControllerAgents,
} from '../../lib/controller-agent-store'
import type { ControllerAgent, ControllerAgentRun } from '../../types/controller_agent'

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
  const [commands, setCommands] = useState<Record<string, string>>({})

  useEffect(() => setAgents(loadControllerAgents()), [])

  const persist = (next: ControllerAgent[]) => {
    setAgents(next)
    saveControllerAgents(next)
  }

  const createAgent = () => {
    if (!name.trim() || !instructions.trim()) return
    const agent = createControllerAgent({
      name: name.trim(),
      description: description.trim(),
      instructions: instructions.trim(),
      session_profile_id: profileId || undefined,
      ...getScopeParams(),
    })
    persist([agent, ...agents])
    setName('')
    setDescription('')
    setProfileId('')
    setShowCreate(false)
  }

  const removeAgent = (agent: ControllerAgent) => {
    if (!window.confirm(`「${agent.name}」を削除しますか？ セッション自体は削除されません。`)) return
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
      const firstRun = !current.controller_session_id
      const message = buildControllerMessage(current, command, firstRun)
      if (firstRun) {
        const session = await client.start({
          params: { message },
          tags: { controller_agent_id: current.id, controller_agent_name: current.name },
          session_profile_id: current.session_profile_id,
          scope: current.scope,
          team_id: current.team_id,
        })
        current = { ...current, controller_session_id: session.session_id }
      } else {
        await client.sendSessionMessage(current.controller_session_id!, { content: message, type: 'user' })
      }
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
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">論理Agentを固定の窓口にして、背後のSessionへ継続的に指令します。</p>
          </div>
          <button onClick={() => setShowCreate((value) => !value)} className="rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700">
            {showCreate ? '閉じる' : 'Agentを作成'}
          </button>
        </div>

        <div className="mb-6 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
          PoC: Agent定義はこのブラウザに保存されます。実行時には本物のSessionを作成・再利用します。
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
                <span className="mb-1 block text-sm text-gray-700 dark:text-gray-200">Session Profile</span>
                <SessionProfileSelect value={profileId} onChange={setProfileId} />
              </div>
            </div>
            <div className="mt-5 flex justify-end">
              <button disabled={!name.trim() || !instructions.trim()} onClick={createAgent} className="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40">作成</button>
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
                    <span>·</span><span>{agent.session_profile_id ? 'Profile指定' : 'Profile自動選択'}</span>
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

                {agent.runs.length > 0 && (
                  <div className="mt-4 border-t border-gray-100 pt-3 dark:border-gray-700">
                    <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-400">Recent runs</p>
                    {agent.runs.slice(0, 3).map((run) => (
                      <div key={run.id} className="mb-2 flex items-start justify-between gap-3 text-sm">
                        <span className="line-clamp-1 text-gray-600 dark:text-gray-300">{run.command}</span>
                        <span className={run.status === 'failed' ? 'shrink-0 text-red-500' : 'shrink-0 text-gray-400'}>{run.status}</span>
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

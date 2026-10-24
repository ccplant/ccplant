'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { Bot, FolderGit2 } from 'lucide-react'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import type { SessionStateAgent, SessionStateComponent, SessionStateConfig } from '../../types/session_profile'
import type { Storage } from '../../types/storage'

const optionClass = 'flex gap-3 rounded-lg border border-gray-200 p-3 dark:border-gray-700'

export default function SessionStateStorageEditor({ value, onChange, scope, teamId, agentType }: {
  value: SessionStateConfig
  onChange: (value: SessionStateConfig) => void
  scope: 'user' | 'team'
  teamId?: string
  agentType?: string
}) {
  const [storages, setStorages] = useState<Storage[]>([])
  const [error, setError] = useState('')
  useEffect(() => {
    let current = true
    createAgentAPIProxyClientFromStorage().getStorages({ scope, team_id: teamId })
      .then(result => { if (current) { setStorages(result.storages); setError('') } })
      .catch(err => { if (current) setError(err instanceof Error ? err.message : 'Storage の取得に失敗しました') })
    return () => { current = false }
  }, [scope, teamId])

  const components = value.components ?? ['agent_state']
  const agents = value.agents ?? ['codex', 'claude']
  const toggleComponent = (component: SessionStateComponent) => onChange({
    ...value,
    components: components.includes(component) ? components.filter(item => item !== component) : [...components, component],
  })
  const toggleAgent = (agent: SessionStateAgent) => onChange({
    ...value,
    agents: agents.includes(agent) ? agents.filter(item => item !== agent) : [...agents, agent],
  })
  const agentStateEnabled = components.includes('agent_state')
  const fixedAgent = agentType?.includes('codex') ? 'codex' : agentType?.includes('claude') ? 'claude' : ''

  return <section className="space-y-4 border-b border-gray-200 pb-6 dark:border-gray-700">
    <div className="flex items-start justify-between gap-4">
      <div>
        <h3 className="font-medium">セッション情報の保持</h3>
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">次のセッションへ引き継ぐ情報だけをStorageへ保存します。認証情報と生成設定は保存しません。</p>
      </div>
      <label className="flex shrink-0 items-center gap-2 text-sm font-medium">
        <input type="checkbox" checked={value.enabled} onChange={event => onChange({ ...value, enabled: event.target.checked })} />
        有効
      </label>
    </div>

    <div className={value.enabled ? 'space-y-4' : 'space-y-4 opacity-50'} aria-disabled={!value.enabled}>
      <label className="block text-sm font-medium">保存先Storage
        <select disabled={!value.enabled} className="mt-1 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 disabled:cursor-not-allowed dark:border-gray-600 dark:bg-gray-700" value={value.storage_id ?? ''} onChange={event => onChange({ ...value, storage_id: event.target.value })}>
          <option value="">選択してください</option>
          {value.storage_id && !storages.some(storage => storage.id === value.storage_id) && <option value={value.storage_id}>未取得のStorage ({value.storage_id})</option>}
          {storages.map(storage => <option key={storage.id} value={storage.id}>{storage.name}</option>)}
        </select>
      </label>
      {error && <p className="text-sm text-red-600 dark:text-red-400">{error}</p>}
      <Link href="/storages" className="inline-block text-sm text-blue-600 dark:text-blue-400">Storageを管理</Link>

      <fieldset className="space-y-2">
        <legend className="mb-2 text-sm font-medium">保持する情報</legend>
        <label className={optionClass}>
          <input disabled={!value.enabled} className="mt-1" type="checkbox" checked={agentStateEnabled} onChange={() => toggleComponent('agent_state')} />
          <Bot size={18} className="mt-0.5 shrink-0 text-indigo-600 dark:text-indigo-400" />
          <span><span className="block text-sm font-medium">会話とエージェント状態</span><span className="block text-xs text-gray-500 dark:text-gray-400">会話履歴、resume index、エージェントの状態DB</span></span>
        </label>
        <label className={optionClass}>
          <input disabled={!value.enabled} className="mt-1" type="checkbox" checked={components.includes('workspace')} onChange={() => toggleComponent('workspace')} />
          <FolderGit2 size={18} className="mt-0.5 shrink-0 text-indigo-600 dark:text-indigo-400" />
          <span><span className="block text-sm font-medium">作業ディレクトリ</span><span className="block text-xs text-gray-500 dark:text-gray-400">repositoryの変更、未追跡ファイル、非Git workspace</span></span>
        </label>
      </fieldset>

      <fieldset className={agentStateEnabled ? 'space-y-2' : 'space-y-2 opacity-50'}>
        <legend className="mb-2 text-sm font-medium">対象エージェント</legend>
        <div className="flex flex-wrap gap-4">
          {(['codex', 'claude'] as const).map(agent => <label key={agent} className="flex items-center gap-2 text-sm">
            <input disabled={!value.enabled || !agentStateEnabled} type="checkbox" checked={agents.includes(agent)} onChange={() => toggleAgent(agent)} />
            {agent === 'codex' ? 'Codex' : 'Claude'}
          </label>)}
        </div>
        {fixedAgent && !agents.includes(fixedAgent) && <p className="text-sm text-red-600 dark:text-red-400">このProfileのエージェント（{fixedAgent === 'codex' ? 'Codex' : 'Claude'}）を選択してください。</p>}
      </fieldset>
      <p className="text-xs text-amber-700 dark:text-amber-300">同じ保存先を使うセッションは同時に起動しないでください。状態DBや作業ファイルが競合する可能性があります。</p>
    </div>
  </section>
}

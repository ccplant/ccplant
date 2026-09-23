'use client'

import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, ArrowRight, Check, ChevronLeft, Loader2, Search, WandSparkles } from 'lucide-react'
import { AgentAPIProxyError, createAgentAPIProxyClientFromStorage, TransferableResourceType } from '@/lib/agentapi-proxy-client'
import { SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../../../SettingsScopeContext'

type Resource = { id: string; name: string; description?: string; type: TransferableResourceType }

const groups: { type: TransferableResourceType; label: string }[] = [
  { type: 'session_profile', label: 'セッションプロファイル' },
  { type: 'sandbox_policy', label: 'サンドボックスポリシー' },
  { type: 'memory', label: 'メモリ' },
  { type: 'webhook', label: 'Webhook' },
  { type: 'slackbot', label: 'SlackBot' },
]

export default function TeamMigrationPage() {
  const { scopeId, userTeams, userTeamNames } = useSettingsScope()
  const [sourceTeam, setSourceTeam] = useState('')
  const [resources, setResources] = useState<Resource[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [step, setStep] = useState<1 | 2 | 3>(1)
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [migrating, setMigrating] = useState(false)
  const [completed, setCompleted] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const sourceTeams = userTeams.filter((team) => team !== scopeId)
  const teamName = (id: string) => userTeamNames[id] || id

  useEffect(() => {
    setSourceTeam('')
    setResources([])
    setSelected(new Set())
    setStep(1)
  }, [scopeId])

  const loadResources = async () => {
    if (!sourceTeam) return
    setLoading(true)
    setError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const [memories, webhooks, slackbots, profiles, policies] = await Promise.all([
        client.listMemories({ scope: 'team', team_id: sourceTeam }),
        client.getWebhooks({ scope: 'team', team_id: sourceTeam, limit: 100 }),
        client.getSlackBots({ scope: 'team', team_id: sourceTeam, limit: 100 }),
        client.getSessionProfiles({ scope: 'team', team_id: sourceTeam }),
        client.getSandboxPolicies({ scope: 'team', team_id: sourceTeam }),
      ])
      const next: Resource[] = [
        ...profiles.session_profiles.map((item) => ({ id: item.id, name: item.name, description: item.description, type: 'session_profile' as const })),
        ...policies.sandbox_policies.map((item) => ({ id: item.id, name: item.name, description: item.description, type: 'sandbox_policy' as const })),
        ...memories.memories.map((item) => ({ id: item.id, name: item.title, description: item.content, type: 'memory' as const })),
        ...webhooks.webhooks.map((item) => ({ id: item.id, name: item.name, description: `${item.type} · ${item.status}`, type: 'webhook' as const })),
        ...slackbots.slackbots.map((item) => ({ id: item.id, name: item.name, description: item.status, type: 'slackbot' as const })),
      ]
      setResources(next)
      setSelected(new Set(next.map((item) => `${item.type}:${item.id}`)))
      setStep(2)
    } catch (reason) {
      setError(reason instanceof AgentAPIProxyError ? reason.message : 'リソースを読み込めませんでした')
    } finally {
      setLoading(false)
    }
  }

  const chosen = useMemo(() => resources.filter((item) => selected.has(`${item.type}:${item.id}`)), [resources, selected])
  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return needle ? resources.filter((item) => `${item.name} ${item.description || ''}`.toLowerCase().includes(needle)) : resources
  }, [resources, query])

  const toggle = (key: string) => setSelected((current) => {
    const next = new Set(current)
    if (next.has(key)) next.delete(key); else next.add(key)
    return next
  })

  const migrate = async () => {
    setMigrating(true)
    setCompleted(0)
    setError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      for (const item of chosen) {
        const request = { resource_type: item.type, resource_id: item.id, target_scope: 'team' as const, target_team_id: scopeId }
        await client.transferResource({ ...request, dry_run: true })
        await client.transferResource(request)
        setCompleted((value) => value + 1)
      }
      setStep(3)
    } catch (reason) {
      setError(reason instanceof AgentAPIProxyError ? reason.message : '移行中にエラーが発生しました。完了済みのリソースは移行先に残っています。')
    } finally {
      setMigrating(false)
    }
  }

  return (
    <div className="max-w-4xl space-y-6">
      <SettingsPageHeader title="移行アシスタント" description={`別のチームから ${teamName(scopeId)} へリソースを付け替えます。`} />

      <ol className="grid grid-cols-3 gap-2" aria-label="移行ステップ">
        {['移行元', 'リソース', '完了'].map((label, index) => {
          const number = index + 1
          const active = step >= number
          return <li key={label} className={`flex items-center gap-2 border-b-2 px-1 pb-3 text-sm font-medium ${active ? 'border-blue-600 text-blue-700 dark:text-blue-300' : 'border-gray-200 text-gray-400 dark:border-gray-700'}`}><span className={`flex size-6 items-center justify-center rounded-full text-xs ${active ? 'bg-blue-600 text-white' : 'bg-gray-100 dark:bg-gray-800'}`}>{step > number ? <Check className="size-3.5" /> : number}</span>{label}</li>
        })}
      </ol>

      {error && <div role="alert" className="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">{error}</div>}

      {step === 1 && <section className="rounded-xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-700 dark:bg-gray-900">
        <h2 className="text-base font-semibold text-gray-900 dark:text-white">統合するチームを選択</h2>
        <p className="mt-1 text-sm text-gray-500">選択したチームのリソースを、現在のチームへ移行します。元のチームは削除されません。</p>
        <label className="mt-6 block text-sm font-medium text-gray-700 dark:text-gray-200">移行元チーム
          <select value={sourceTeam} onChange={(event) => setSourceTeam(event.target.value)} className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm dark:border-gray-600 dark:bg-gray-950">
            <option value="">チームを選択してください</option>
            {sourceTeams.map((team) => <option key={team} value={team}>{teamName(team)} ({team})</option>)}
          </select>
        </label>
        {sourceTeams.length === 0 && <p className="mt-3 text-sm text-amber-700 dark:text-amber-300">移行できる別のチームがありません。</p>}
        <div className="mt-6 flex items-center justify-end"><button onClick={loadResources} disabled={!sourceTeam || loading} className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{loading && <Loader2 className="size-4 animate-spin" />}リソースを確認<ArrowRight className="size-4" /></button></div>
      </section>}

      {step === 2 && <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold text-gray-900 dark:text-white">移行するリソースを選択</h2><p className="text-sm text-gray-500">{teamName(sourceTeam)} から {teamName(scopeId)} へ</p></div><div className="relative"><Search className="absolute left-3 top-2.5 size-4 text-gray-400" /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="リソースを検索" className="rounded-lg border border-gray-300 bg-white py-2 pl-9 pr-3 text-sm dark:border-gray-600 dark:bg-gray-900" /></div></div>
        {groups.map((group) => {
          const items = visible.filter((item) => item.type === group.type)
          const allItems = resources.filter((item) => item.type === group.type)
          if (!items.length && query) return null
          const allSelected = allItems.length > 0 && allItems.every((item) => selected.has(`${item.type}:${item.id}`))
          return <div key={group.type} className="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900"><div className="flex items-center justify-between border-b border-gray-100 bg-gray-50 px-4 py-3 dark:border-gray-800 dark:bg-gray-800/50"><label className="flex items-center gap-3 text-sm font-semibold"><input type="checkbox" checked={allSelected} disabled={!allItems.length} onChange={() => setSelected((current) => { const next = new Set(current); allItems.forEach((item) => allSelected ? next.delete(`${item.type}:${item.id}`) : next.add(`${item.type}:${item.id}`)); return next })} />{group.label}</label><span className="text-xs text-gray-500">{allItems.filter((item) => selected.has(`${item.type}:${item.id}`)).length} / {allItems.length}</span></div><div className="divide-y divide-gray-100 dark:divide-gray-800">{items.length ? items.map((item) => { const key = `${item.type}:${item.id}`; return <label key={key} className="flex cursor-pointer items-start gap-3 px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-800/40"><input className="mt-1" type="checkbox" checked={selected.has(key)} onChange={() => toggle(key)} /><span className="min-w-0"><span className="block text-sm font-medium text-gray-900 dark:text-white">{item.name}</span>{item.description && <span className="block truncate text-xs text-gray-500">{item.description}</span>}</span></label> }) : <p className="px-4 py-3 text-sm text-gray-400">リソースはありません</p>}</div></div>
        })}
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><div className="flex gap-2"><AlertTriangle className="mt-0.5 size-4 shrink-0" /><p>所有権を付け替える操作です。Webhook や SlackBot が参照するプロファイルも一緒に選択してください。移行前に各リソースを検証します。</p></div></div>
        <div className="flex items-center justify-between"><button onClick={() => setStep(1)} disabled={migrating} className="inline-flex items-center gap-1 rounded-lg px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"><ChevronLeft className="size-4" />戻る</button><button onClick={migrate} disabled={!chosen.length || migrating} className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{migrating ? <Loader2 className="size-4 animate-spin" /> : <WandSparkles className="size-4" />}{migrating ? `${completed} / ${chosen.length} 移行中` : `${chosen.length}件を移行`}</button></div>
      </section>}

      {step === 3 && <section className="rounded-xl border border-emerald-200 bg-emerald-50 p-8 text-center dark:border-emerald-900 dark:bg-emerald-950/20"><span className="mx-auto flex size-12 items-center justify-center rounded-full bg-emerald-600 text-white"><Check className="size-6" /></span><h2 className="mt-4 text-lg font-semibold text-gray-900 dark:text-white">移行が完了しました</h2><p className="mt-1 text-sm text-gray-600 dark:text-gray-300">{completed}件のリソースを {teamName(scopeId)} に付け替えました。</p><button onClick={() => { setSourceTeam(''); setResources([]); setSelected(new Set()); setStep(1) }} className="mt-6 rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-900">別のチームを移行</button></section>}
    </div>
  )
}

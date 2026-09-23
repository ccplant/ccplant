'use client'

import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, ArrowRight, Check, ChevronLeft, Loader2, Search, WandSparkles } from 'lucide-react'
import { AgentAPIProxyError, createAgentAPIProxyClientFromStorage, TransferableResourceType } from '@/lib/agentapi-proxy-client'
import { SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../../../SettingsScopeContext'
import type { Webhook } from '@/types/webhook'
import type { SlackBot } from '@/types/slackbot'
import type { SessionProfile } from '@/types/session_profile'
import type { SandboxPolicy } from '@/types/sandbox_policy'
import type { Schedule } from '@/types/schedule'

type MigrationResourceType = TransferableResourceType | 'schedule'
type MigratablePayload = Webhook | SlackBot | Schedule | SessionProfile | SandboxPolicy
type Resource = { id: string; name: string; description?: string; type: MigrationResourceType; payload?: MigratablePayload }
type SourceMode = 'current' | 'external'

interface ExternalSourceResponse {
  webhooks: Webhook[] | { webhooks?: Webhook[] }
  slackbots: SlackBot[] | { slackbots?: SlackBot[] }
  schedules: Schedule[] | { schedules?: Schedule[] }
  profiles: SessionProfile[] | { session_profiles?: SessionProfile[] }
  policies: SandboxPolicy[] | { sandbox_policies?: SandboxPolicy[] }
}

const groups: { type: MigrationResourceType; label: string }[] = [
  { type: 'session_profile', label: 'セッションプロファイル' },
  { type: 'schedule', label: 'スケジュール' },
  { type: 'sandbox_policy', label: 'サンドボックスポリシー' },
  { type: 'webhook', label: 'Webhook' },
  { type: 'slackbot', label: 'SlackBot' },
]

export default function TeamMigrationPage() {
  const { scopeId, userTeams, userTeamNames } = useSettingsScope()
  const [sourceMode, setSourceMode] = useState<SourceMode>('current')
  const [sourceTeam, setSourceTeam] = useState('')
  const [apiUrl, setApiUrl] = useState('')
  const [apiToken, setApiToken] = useState('')
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
    setApiToken('')
    setResources([])
    setSelected(new Set())
    setStep(1)
  }, [scopeId])

  const loadResources = async () => {
    if (!sourceTeam || (sourceMode === 'external' && (!apiUrl.trim() || !apiToken.trim()))) return
    setLoading(true)
    setError(null)
    try {
      let webhooks: Webhook[]
      let slackbots: SlackBot[]
      let schedules: Schedule[]
      let profiles: SessionProfile[]
      let policies: SandboxPolicy[]
      if (sourceMode === 'external') {
        const response = await fetch('/api/team-migration/source', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ api_url: apiUrl, token: apiToken, team_id: sourceTeam }),
        })
        const data = await response.json() as ExternalSourceResponse & { message?: string }
        if (!response.ok) throw new Error(data.message || '移行元へ接続できませんでした')
        webhooks = Array.isArray(data.webhooks) ? data.webhooks : data.webhooks.webhooks || []
        slackbots = Array.isArray(data.slackbots) ? data.slackbots : data.slackbots.slackbots || []
        schedules = Array.isArray(data.schedules) ? data.schedules : data.schedules.schedules || []
        profiles = Array.isArray(data.profiles) ? data.profiles : data.profiles.session_profiles || []
        policies = Array.isArray(data.policies) ? data.policies : data.policies.sandbox_policies || []
      } else {
        const client = createAgentAPIProxyClientFromStorage()
        const result = await Promise.all([
          client.getWebhooks({ scope: 'team', team_id: sourceTeam, limit: 100 }),
          client.getSlackBots({ scope: 'team', team_id: sourceTeam, limit: 100 }),
          client.getSchedules({ scope: 'team', team_id: sourceTeam, limit: 100 }),
          client.getSessionProfiles({ scope: 'team', team_id: sourceTeam }),
          client.getSandboxPolicies({ scope: 'team', team_id: sourceTeam }),
        ])
        webhooks = result[0].webhooks
        slackbots = result[1].slackbots
        schedules = result[2].schedules
        profiles = result[3].session_profiles
        policies = result[4].sandbox_policies
      }
      const next: Resource[] = [
        ...policies.map((item) => ({ id: item.id, name: item.name, description: item.description, type: 'sandbox_policy' as const, payload: item })),
        ...profiles.map((item) => ({ id: item.id, name: item.name, description: item.description, type: 'session_profile' as const, payload: item })),
        ...schedules.map((item) => ({ id: item.id, name: item.name, description: item.cron_expr || item.scheduled_at, type: 'schedule' as const, payload: item })),
        ...webhooks.map((item) => ({ id: item.id, name: item.name, description: `${item.type} · ${item.status}`, type: 'webhook' as const, payload: item })),
        ...slackbots.map((item) => ({ id: item.id, name: item.name, description: item.status, type: 'slackbot' as const, payload: item })),
      ]
      setResources(next)
      setSelected(new Set(next.map((item) => `${item.type}:${item.id}`)))
      if (sourceMode === 'external') setApiToken('')
      setStep(2)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'リソースを読み込めませんでした')
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
      const idMap = new Map<string, string>()
      for (const item of chosen) {
        if (sourceMode === 'current' && item.type !== 'schedule') {
          const request = { resource_type: item.type, resource_id: item.id, target_scope: 'team' as const, target_team_id: scopeId }
          await client.transferResource({ ...request, dry_run: true })
          await client.transferResource(request)
        } else if (item.payload) {
          if (item.type === 'sandbox_policy') {
            const value = item.payload as SandboxPolicy
            const created = await client.createSandboxPolicy({ name: value.name, description: value.description, allowed_domains: value.allowed_domains, denied_domains: value.denied_domains, count_mode: value.count_mode, scope: 'team', team_id: scopeId })
            idMap.set(item.id, created.id)
          } else if (item.type === 'session_profile') {
            const value = item.payload as SessionProfile
            const config = value.config ? structuredClone(value.config) : undefined
            if (config?.sandbox_policy_id) config.sandbox_policy_id = idMap.get(config.sandbox_policy_id)
            if (config?.source_session_profile_id) config.source_session_profile_id = idMap.get(config.source_session_profile_id)
            if (config?.settings_team_id) config.settings_team_id = scopeId
            const created = await client.createSessionProfile({ name: value.name, description: value.description, is_default: value.is_default, selector_tags: value.selector_tags, config, scope: 'team', team_id: scopeId })
            idMap.set(item.id, created.id)
          } else if (item.type === 'schedule') {
            const value = item.payload as Schedule
            const sessionConfig = value.session_config ? structuredClone(value.session_config) : undefined
            if (sourceMode === 'external' && sessionConfig?.session_profile_id) sessionConfig.session_profile_id = idMap.get(sessionConfig.session_profile_id)
            const created = await client.createSchedule({ name: value.name, scheduled_at: value.scheduled_at, cron_expr: value.cron_expr, timezone: value.timezone, session_config: sessionConfig, scope: 'team', team_id: scopeId })
            if (value.status !== 'active') await client.updateSchedule(created.id, { status: value.status })
            idMap.set(item.id, created.id)
          } else if (item.type === 'webhook') {
            const value = item.payload as Webhook
            const sessionConfig = value.session_config ? structuredClone(value.session_config) : undefined
            if (sessionConfig?.session_profile_id) sessionConfig.session_profile_id = idMap.get(sessionConfig.session_profile_id)
            const triggers = structuredClone(value.triggers || [])
            for (const trigger of triggers) {
              const profileId = trigger.session_config?.session_profile_id
              if (profileId) trigger.session_config!.session_profile_id = idMap.get(profileId)
            }
            const created = await client.createWebhook({ name: value.name, type: value.type, github: value.github, signature_header: value.signature_header, signature_type: value.signature_type, signature_prefix: value.signature_prefix, triggers, session_config: sessionConfig, scope: 'team', team_id: scopeId })
            idMap.set(item.id, created.id)
          } else if (item.type === 'slackbot') {
            const value = item.payload as SlackBot
            const sessionConfig = value.session_config ? structuredClone(value.session_config) : undefined
            if (sessionConfig?.session_profile_id) sessionConfig.session_profile_id = idMap.get(sessionConfig.session_profile_id)
            const created = await client.createSlackBot({ name: value.name, allowed_event_types: value.allowed_event_types, allowed_channel_names: value.allowed_channel_names, allowed_user_ids: value.allowed_user_ids, session_config: sessionConfig, max_sessions: value.max_sessions, notify_on_session_created: value.notify_on_session_created, allow_bot_messages: value.allow_bot_messages, scope: 'team', team_id: scopeId })
            idMap.set(item.id, created.id)
          }
        }
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
        <div className="mt-6 grid grid-cols-2 gap-2 rounded-lg bg-gray-100 p-1 dark:bg-gray-800">
          {([['current', 'このインスタンス'], ['external', '別のインスタンス']] as const).map(([value, label]) => <button key={value} type="button" onClick={() => { setSourceMode(value); setSourceTeam(''); setError(null) }} className={`rounded-md px-3 py-2 text-sm font-medium ${sourceMode === value ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-700 dark:text-white' : 'text-gray-500'}`}>{label}</button>)}
        </div>
        {sourceMode === 'current' ? <>
          <label className="mt-5 block text-sm font-medium text-gray-700 dark:text-gray-200">移行元チーム
            <select value={sourceTeam} onChange={(event) => setSourceTeam(event.target.value)} className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm dark:border-gray-600 dark:bg-gray-950">
              <option value="">チームを選択してください</option>
              {sourceTeams.map((team) => <option key={team} value={team}>{teamName(team)} ({team})</option>)}
            </select>
          </label>
          {sourceTeams.length === 0 && <p className="mt-3 text-sm text-amber-700 dark:text-amber-300">移行できる別のチームがありません。</p>}
        </> : <div className="mt-5 space-y-4">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-200">API URL<input type="url" value={apiUrl} onChange={(event) => setApiUrl(event.target.value)} placeholder="https://other.example.com/api/proxy" autoComplete="url" className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm dark:border-gray-600 dark:bg-gray-950" /></label>
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-200">API token<input type="password" value={apiToken} onChange={(event) => setApiToken(event.target.value)} placeholder="API token を貼り付け" autoComplete="off" className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 font-mono text-sm dark:border-gray-600 dark:bg-gray-950" /></label>
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-200">移行元 Team ID<input value={sourceTeam} onChange={(event) => setSourceTeam(event.target.value)} placeholder="organization/team" className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm dark:border-gray-600 dark:bg-gray-950" /></label>
          <p className="text-xs text-gray-500">token は保存されず、一覧の取得にだけ使用します。移行先には送信されません。</p>
        </div>}
        <div className="mt-6 flex items-center justify-end"><button onClick={loadResources} disabled={!sourceTeam || loading || (sourceMode === 'external' && (!apiUrl.trim() || !apiToken.trim()))} className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{loading && <Loader2 className="size-4 animate-spin" />}リソースを確認<ArrowRight className="size-4" /></button></div>
      </section>}

      {step === 2 && <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold text-gray-900 dark:text-white">移行するリソースを選択</h2><p className="text-sm text-gray-500">{sourceMode === 'external' ? sourceTeam : teamName(sourceTeam)} から {teamName(scopeId)} へ</p></div><div className="relative"><Search className="absolute left-3 top-2.5 size-4 text-gray-400" /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="リソースを検索" className="rounded-lg border border-gray-300 bg-white py-2 pl-9 pr-3 text-sm dark:border-gray-600 dark:bg-gray-900" /></div></div>
        {groups.map((group) => {
          const items = visible.filter((item) => item.type === group.type)
          const allItems = resources.filter((item) => item.type === group.type)
          if (!items.length && query) return null
          const allSelected = allItems.length > 0 && allItems.every((item) => selected.has(`${item.type}:${item.id}`))
          return <div key={group.type} className="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900"><div className="flex items-center justify-between border-b border-gray-100 bg-gray-50 px-4 py-3 dark:border-gray-800 dark:bg-gray-800/50"><label className="flex items-center gap-3 text-sm font-semibold"><input type="checkbox" checked={allSelected} disabled={!allItems.length} onChange={() => setSelected((current) => { const next = new Set(current); allItems.forEach((item) => allSelected ? next.delete(`${item.type}:${item.id}`) : next.add(`${item.type}:${item.id}`)); return next })} />{group.label}</label><span className="text-xs text-gray-500">{allItems.filter((item) => selected.has(`${item.type}:${item.id}`)).length} / {allItems.length}</span></div><div className="divide-y divide-gray-100 dark:divide-gray-800">{items.length ? items.map((item) => { const key = `${item.type}:${item.id}`; return <label key={key} className="flex cursor-pointer items-start gap-3 px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-800/40"><input className="mt-1" type="checkbox" checked={selected.has(key)} onChange={() => toggle(key)} /><span className="min-w-0"><span className="block text-sm font-medium text-gray-900 dark:text-white">{item.name}</span>{item.description && <span className="block truncate text-xs text-gray-500">{item.description}</span>}</span></label> }) : <p className="px-4 py-3 text-sm text-gray-400">リソースはありません</p>}</div></div>
        })}
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><div className="flex gap-2"><AlertTriangle className="mt-0.5 size-4 shrink-0" /><p>{sourceMode === 'external' ? '別インスタンスには元データを残したまま、新しいリソースを作成します。Webhook の secret と SlackBot の token は安全上取得できないため、移行後に再設定してください。' : 'リソースの所有権を付け替えます。Schedule は移行元にも残るコピーとして作成されます。参照するプロファイルも一緒に選択してください。'}</p></div></div>
        <div className="flex items-center justify-between"><button onClick={() => setStep(1)} disabled={migrating} className="inline-flex items-center gap-1 rounded-lg px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"><ChevronLeft className="size-4" />戻る</button><button onClick={migrate} disabled={!chosen.length || migrating} className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{migrating ? <Loader2 className="size-4 animate-spin" /> : <WandSparkles className="size-4" />}{migrating ? `${completed} / ${chosen.length} 移行中` : `${chosen.length}件を移行`}</button></div>
      </section>}

      {step === 3 && <section className="rounded-xl border border-emerald-200 bg-emerald-50 p-8 text-center dark:border-emerald-900 dark:bg-emerald-950/20"><span className="mx-auto flex size-12 items-center justify-center rounded-full bg-emerald-600 text-white"><Check className="size-6" /></span><h2 className="mt-4 text-lg font-semibold text-gray-900 dark:text-white">移行が完了しました</h2><p className="mt-1 text-sm text-gray-600 dark:text-gray-300">{completed}件のリソースを {teamName(scopeId)} に{sourceMode === 'external' ? '作成' : '付け替え'}しました。</p><button onClick={() => { setSourceTeam(''); setApiToken(''); setResources([]); setSelected(new Set()); setStep(1) }} className="mt-6 rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-900">別のチームを移行</button></section>}
    </div>
  )
}

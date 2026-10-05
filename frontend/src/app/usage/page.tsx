'use client'

import { useEffect, useMemo, useState } from 'react'
import { Activity, Download, Timer, Zap } from 'lucide-react'
import { Area, AreaChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import NavigationTabs from '../components/NavigationTabs'
import TopBar from '../components/TopBar'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { formatCompactNumber, formatDuration } from './format'

type View = 'model' | 'runtime'
type Range = '7d' | '30d' | '90d'
interface UsageBreakdown { key: string; events: number; input_tokens: number; output_tokens: number; cached_input_tokens: number; cache_creation_tokens: number; reasoning_tokens: number }
interface UsageSummary extends Omit<UsageBreakdown, 'key'> { by_model: UsageBreakdown[]; by_session: UsageBreakdown[] }
interface RuntimeBucket { start: string; runtime_seconds: number; running_seconds: number; suspended_seconds: number; peak_concurrent: number }
interface RuntimeSession { session_id: string; runtime_seconds: number; running_seconds: number; suspended_seconds: number; current_status: string }
interface RuntimeDashboard {
  from: string; to: string; as_of: string; is_partial: boolean; timezone: string; coverage_started_at?: string
  summary: { runtime_seconds: number; running_seconds: number; suspended_seconds: number; sessions: number; peak_concurrent: number }
  trend: RuntimeBucket[]; by_session: RuntimeSession[]; available_pools: string[]
}

const ranges: Array<{ value: Range; label: string; days: number }> = [
  { value: '7d', label: '7日', days: 7 }, { value: '30d', label: '30日', days: 30 }, { value: '90d', label: '90日', days: 90 },
]
const totalTokens = (value: Pick<UsageBreakdown, 'input_tokens' | 'output_tokens' | 'cached_input_tokens' | 'cache_creation_tokens'>) => value.input_tokens + value.output_tokens + value.cached_input_tokens + value.cache_creation_tokens
const monthValue = (date = new Date()) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`
const monthBounds = (value: string) => { const [year, month] = value.split('-').map(Number); return { from: new Date(year, month - 1, 1).toISOString(), to: new Date(year, month, 1).toISOString() } }

function StatusMessage({ children, error = false }: { children: React.ReactNode; error?: boolean }) {
  return <div role={error ? 'alert' : 'status'} className={`border-l-4 px-5 py-4 text-sm ${error ? 'border-red-500 bg-red-50 text-red-800 dark:bg-red-950/30 dark:text-red-200' : 'border-blue-500 bg-blue-50 text-blue-800 dark:bg-blue-950/30 dark:text-blue-200'}`}>{children}</div>
}

export default function UsagePage() {
  const { selectedTeam } = useTeamScope()
  const [view, setView] = useState<View>('model'); const [range, setRange] = useState<Range>('30d'); const [month, setMonth] = useState(monthValue); const [pool, setPool] = useState('')
  const [usage, setUsage] = useState<UsageSummary | null>(null); const [runtime, setRuntime] = useState<RuntimeDashboard | null>(null); const [loading, setLoading] = useState(true); const [error, setError] = useState('')
  const usageParams = useMemo(() => { const option = ranges.find(item => item.value === range) ?? ranges[1]; const params = new URLSearchParams({ from: new Date(Date.now() - option.days * 86_400_000).toISOString() }); if (selectedTeam) params.set('team_id', selectedTeam); return params }, [range, selectedTeam])
  const runtimeParams = useMemo(() => { const params = new URLSearchParams({ ...monthBounds(month), timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', breakdown_limit: '20' }); if (selectedTeam) params.set('team_id', selectedTeam); if (pool) params.set('pool', pool); return params }, [month, pool, selectedTeam])

  useEffect(() => {
    let active = true; const controller = new AbortController(); setLoading(true); setError(''); if (view === 'model') setUsage(null); else setRuntime(null)
    const load = async () => {
      try {
        const path = view === 'model' ? `/api/proxy/usage?${usageParams}` : `/api/proxy/session-usage/dashboard?${runtimeParams}`
        const response = await fetch(path, { signal: controller.signal, cache: 'no-store' })
        if (!response.ok) { if (response.status === 404) throw new Error('この利用統計は環境で有効になっていません。'); if (response.status === 403) throw new Error('選択したチームの利用統計を表示する権限がありません。'); throw new Error('利用統計を取得できませんでした。もう一度お試しください。') }
        const data = await response.json(); if (!active) return; if (view === 'model') setUsage(data); else setRuntime(data)
      } catch (caught) { if (active && !(caught instanceof DOMException && caught.name === 'AbortError')) setError(caught instanceof Error ? caught.message : '利用統計を取得できませんでした。') }
      finally { if (active) setLoading(false) }
    }
    void load(); const refresh = view === 'runtime' && month === monthValue() ? window.setInterval(load, 60_000) : undefined
    return () => { active = false; controller.abort(); if (refresh) window.clearInterval(refresh) }
  }, [view, month, runtimeParams, usageParams])

  const usageTotal = usage ? totalTokens(usage) : 0
  return <main className="min-h-dvh bg-gray-50 text-gray-900 dark:bg-gray-950 dark:text-gray-100">
    <TopBar title="Usage" subtitle={selectedTeam ? `Team: ${selectedTeam}` : 'Personal usage'} showSettingsButton><NavigationTabs className="w-44" /></TopBar>
    <div className="mx-auto max-w-6xl px-4 py-6 md:px-8 md:py-9">
      <div className="flex flex-col gap-4 border-b border-gray-200 pb-5 dark:border-gray-800 md:flex-row md:items-end md:justify-between">
        <div className="inline-flex w-fit rounded-lg bg-gray-200 p-1 dark:bg-gray-800" aria-label="利用統計の種類">
          <ViewButton active={view === 'model'} onClick={() => setView('model')}>Model usage</ViewButton><ViewButton active={view === 'runtime'} onClick={() => setView('runtime')}>Session runtime</ViewButton>
        </div>
        {view === 'model' ? <div className="flex items-center gap-2"><select aria-label="対象期間" value={range} onChange={event => setRange(event.target.value as Range)} className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900">{ranges.map(option => <option key={option.value} value={option.value}>過去{option.label}</option>)}</select><a href={`/api/proxy/usage/export.parquet?${usageParams}`} className="inline-flex items-center gap-2 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium hover:border-blue-400 dark:border-gray-700 dark:bg-gray-900"><Download className="h-4 w-4" />Parquet</a></div>
          : <div className="flex flex-wrap items-center gap-2"><label htmlFor="runtime-month" className="text-sm text-gray-500">対象月</label><input id="runtime-month" type="month" value={month} max={monthValue()} onChange={event => setMonth(event.target.value)} className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900" />{runtime && runtime.available_pools.length > 1 && <select aria-label="Pool" value={pool} onChange={event => setPool(event.target.value)} className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900"><option value="">All pools</option>{runtime.available_pools.map(value => <option key={value}>{value}</option>)}</select>}</div>}
      </div>
      {loading && <div aria-live="polite" className="mt-8 h-72 animate-pulse rounded-xl bg-gray-200 dark:bg-gray-800" />}
      {!loading && error && <div className="mt-8"><StatusMessage error>{error}</StatusMessage></div>}
      {!loading && !error && view === 'model' && usage && (usage.events === 0 ? <div className="mt-8"><StatusMessage>この期間のモデル利用データはありません。期間を広げて確認してください。</StatusMessage></div> : <div className="mt-8 space-y-8"><section className="border-b border-gray-200 pb-8 dark:border-gray-800"><div className="flex flex-wrap items-baseline gap-x-8 gap-y-3"><Metric label="Total tokens" value={formatCompactNumber(usageTotal)} primary /><Metric label="Responses" value={usage.events.toLocaleString()} /></div><div className="mt-6 flex flex-wrap gap-x-6 gap-y-2 text-sm tabular-nums text-gray-600 dark:text-gray-300"><span>Input {formatCompactNumber(usage.input_tokens)}</span><span>Output {formatCompactNumber(usage.output_tokens)}</span><span>Cached {formatCompactNumber(usage.cached_input_tokens)}</span></div></section><div className="grid gap-8 lg:grid-cols-2"><Breakdown title="Sessions" items={usage.by_session} /><Breakdown title="Models" items={usage.by_model} /></div></div>)}
      {!loading && !error && view === 'runtime' && runtime && (runtime.summary.sessions === 0 ? <div className="mt-8"><StatusMessage>この月のセッション稼働データはありません。</StatusMessage></div> : <RuntimeView runtime={runtime} />)}
    </div>
  </main>
}

function ViewButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) { return <button type="button" onClick={onClick} aria-pressed={active} className={`rounded-md px-4 py-2 text-sm font-medium ${active ? 'bg-white text-gray-950 shadow-sm dark:bg-gray-700 dark:text-white' : 'text-gray-600 dark:text-gray-300'}`}>{children}</button> }
function Metric({ label, value, primary = false }: { label: React.ReactNode; value: string; primary?: boolean }) { return <div><div className="text-sm text-gray-500">{label}</div><div className={`mt-1 font-semibold tabular-nums text-gray-950 dark:text-white ${primary ? 'text-4xl' : 'text-xl'}`}>{value}</div></div> }

function RuntimeView({ runtime }: { runtime: RuntimeDashboard }) {
  return <div className="mt-8 space-y-8"><section className="border-b border-gray-200 pb-8 dark:border-gray-800"><div className="flex flex-wrap items-end justify-between gap-5"><div className="flex flex-wrap items-baseline gap-x-8 gap-y-3"><Metric label={<span className="flex items-center gap-2"><Timer className="h-4 w-4" />Runtime</span>} value={formatDuration(runtime.summary.runtime_seconds)} primary /><Metric label={<span className="flex items-center gap-2"><Zap className="h-4 w-4" />Running</span>} value={formatDuration(runtime.summary.running_seconds)} /><Metric label={<span className="flex items-center gap-2"><Activity className="h-4 w-4" />Peak concurrent</span>} value={String(runtime.summary.peak_concurrent)} /></div>{runtime.is_partial && <div className="text-xs text-gray-500">{new Date(runtime.as_of).toLocaleString()} 時点</div>}</div>
    <div className="mt-7 h-72" aria-label="日ごとのセッション稼働時間"><ResponsiveContainer width="100%" height="100%"><AreaChart data={runtime.trend} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}><CartesianGrid strokeDasharray="3 3" vertical={false} stroke="currentColor" opacity={0.12} /><XAxis dataKey="start" tickFormatter={value => new Date(value).toLocaleDateString(undefined, { day: 'numeric' })} fontSize={12} /><YAxis tickFormatter={value => `${Math.round(Number(value) / 3600)}h`} fontSize={12} width={44} /><Tooltip labelFormatter={value => new Date(value).toLocaleDateString()} formatter={(value, name) => [formatDuration(Number(value)), name === 'runtime_seconds' ? 'Runtime' : 'Running']} /><Legend formatter={value => value === 'runtime_seconds' ? 'Runtime' : 'Running'} /><Area type="monotone" dataKey="runtime_seconds" stroke="#2563eb" fill="#2563eb" fillOpacity={0.16} isAnimationActive={false} /><Area type="monotone" dataKey="running_seconds" stroke="#8b5cf6" fill="#8b5cf6" fillOpacity={0.22} isAnimationActive={false} /></AreaChart></ResponsiveContainer></div></section>
    {runtime.coverage_started_at && new Date(runtime.coverage_started_at) > new Date(runtime.from) && <StatusMessage>記録開始以前の時間は含まれていません。</StatusMessage>}<RuntimeTable sessions={runtime.by_session} total={runtime.summary.runtime_seconds} /></div>
}

function Breakdown({ title, items }: { title: string; items: UsageBreakdown[] }) { return <section><h2 className="text-lg font-semibold">{title}</h2><div className="mt-3 divide-y divide-gray-200 border-y border-gray-200 dark:divide-gray-800 dark:border-gray-800">{items.slice(0, 10).map(item => <div key={item.key || 'unknown'} className="grid grid-cols-[minmax(0,1fr)_auto] gap-4 py-3 text-sm"><div className="truncate font-medium" title={item.key}>{item.key || 'Unknown'}</div><div className="text-right tabular-nums"><div>{formatCompactNumber(totalTokens(item))} tokens</div><div className="text-xs text-gray-500">{item.events.toLocaleString()} responses</div></div></div>)}</div></section> }
function RuntimeTable({ sessions, total }: { sessions: RuntimeSession[]; total: number }) { return <section><h2 className="text-lg font-semibold">Sessions</h2><div className="mt-3 divide-y divide-gray-200 border-y border-gray-200 dark:divide-gray-800 dark:border-gray-800">{sessions.map(session => <div key={session.session_id} className="grid gap-2 py-4 text-sm sm:grid-cols-[minmax(0,1fr)_8rem_8rem_8rem] sm:items-center"><div className="truncate font-medium" title={session.session_id}>{session.session_id.slice(0, 12)}</div><div className="tabular-nums"><span className="text-gray-500 sm:hidden">Runtime </span>{formatDuration(session.runtime_seconds)}</div><div className="tabular-nums"><span className="text-gray-500 sm:hidden">Running </span>{formatDuration(session.running_seconds)}</div><div className="flex items-center justify-between gap-3 sm:justify-end"><span className="text-xs text-gray-500">{total ? `${(session.runtime_seconds / total * 100).toFixed(1)}%` : '0%'}</span><span className="rounded-full bg-gray-100 px-2 py-1 text-xs dark:bg-gray-800">{session.current_status}</span></div></div>)}</div></section> }

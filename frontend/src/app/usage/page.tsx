'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Activity, ArrowDownToLine, ArrowUpFromLine, BrainCircuit, Database, RefreshCw } from 'lucide-react'
import Link from 'next/link'
import { Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { UsageBreakdown, UsageSummary } from '../../types/usage'

type Range = '7d' | '30d' | '90d' | 'all'

const emptySummary: UsageSummary = {
  events: 0,
  input_tokens: 0,
  output_tokens: 0,
  cached_input_tokens: 0,
  cache_creation_tokens: 0,
  reasoning_tokens: 0,
  by_day: [],
  by_model: [],
  by_session: [],
}

function formatTokens(value: number): string {
  return new Intl.NumberFormat('ja-JP', { notation: value >= 10000 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value)
}

function exactTokens(value: number): string {
  return new Intl.NumberFormat('ja-JP').format(value)
}

function usageTotal(item: UsageBreakdown): number {
  return item.input_tokens + item.output_tokens + item.cached_input_tokens + item.cache_creation_tokens
}

function SummaryCard({ label, value, detail, icon }: { label: string; value: number; detail: string; icon: ReactNode }) {
  return (
    <div className="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-gray-500 dark:text-gray-400">{label}</p>
          <p className="mt-2 text-3xl font-semibold tracking-tight text-gray-950 dark:text-white" title={exactTokens(value)}>{formatTokens(value)}</p>
          <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">{detail}</p>
        </div>
        <span className="rounded-xl bg-indigo-50 p-2.5 text-indigo-600 dark:bg-indigo-950/60 dark:text-indigo-300">{icon}</span>
      </div>
    </div>
  )
}

function BreakdownTable({ title, rows, sessions = false }: { title: string; rows: UsageBreakdown[]; sessions?: boolean }) {
  const max = Math.max(...rows.map(usageTotal), 1)
  return (
    <section className="overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div className="border-b border-gray-100 px-5 py-4 dark:border-gray-800">
        <h2 className="font-semibold text-gray-900 dark:text-white">{title}</h2>
      </div>
      {rows.length === 0 ? (
        <p className="px-5 py-12 text-center text-sm text-gray-400">この期間のデータはありません</p>
      ) : (
        <div className="divide-y divide-gray-100 dark:divide-gray-800">
          {rows.slice(0, 20).map((row) => (
            <div key={row.key} className="px-5 py-4">
              <div className="flex items-center justify-between gap-4">
                <div className="min-w-0">
                  {sessions ? (
                    <Link href={`/sessions/${row.key}`} className="font-mono text-sm text-indigo-600 hover:underline dark:text-indigo-400">{row.key.slice(0, 12)}</Link>
                  ) : (
                    <p className="truncate text-sm font-medium text-gray-800 dark:text-gray-200" title={row.key}>{row.key}</p>
                  )}
                  <p className="mt-0.5 text-xs text-gray-400">{exactTokens(row.events)} responses</p>
                </div>
                <p className="shrink-0 text-sm font-semibold tabular-nums text-gray-900 dark:text-white">{formatTokens(usageTotal(row))}</p>
              </div>
              <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
                <div className="h-full rounded-full bg-gradient-to-r from-indigo-500 to-cyan-400" style={{ width: `${Math.max((usageTotal(row) / max) * 100, 1)}%` }} />
              </div>
              <div className="mt-2 flex gap-3 text-[11px] text-gray-400">
                <span>In {formatTokens(row.input_tokens)}</span>
                <span>Out {formatTokens(row.output_tokens)}</span>
                <span>Cache {formatTokens(row.cached_input_tokens + row.cache_creation_tokens)}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function UsageChart({ rows }: { rows: UsageBreakdown[] }) {
  const data = rows.map((row) => ({
    date: row.key,
    label: new Intl.DateTimeFormat('ja-JP', { month: 'numeric', day: 'numeric' }).format(new Date(`${row.key}T00:00:00Z`)),
    Input: row.input_tokens,
    Output: row.output_tokens,
    Cache: row.cached_input_tokens + row.cache_creation_tokens,
  }))

  return (
    <section className="mt-6 rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div className="mb-5">
        <h2 className="font-semibold text-gray-900 dark:text-white">Token usage over time</h2>
        <p className="mt-1 text-xs text-gray-400">日別のトークン利用量</p>
      </div>
      {data.length === 0 ? (
        <p className="py-16 text-center text-sm text-gray-400">この期間のデータはありません</p>
      ) : (
        <div className="h-80 w-full" data-testid="usage-chart">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="currentColor" className="text-gray-200 dark:text-gray-800" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tick={{ fontSize: 12, fill: '#9ca3af' }} minTickGap={24} />
              <YAxis tickFormatter={formatTokens} tickLine={false} axisLine={false} tick={{ fontSize: 12, fill: '#9ca3af' }} width={48} />
              <Tooltip formatter={(value) => exactTokens(Number(value))} labelFormatter={(_, payload) => payload?.[0]?.payload?.date || ''} contentStyle={{ borderRadius: 12, borderColor: '#e5e7eb' }} />
              <Legend iconType="circle" iconSize={8} wrapperStyle={{ fontSize: 12 }} />
              <Bar dataKey="Input" stackId="tokens" fill="#6366f1" radius={[0, 0, 0, 0]} />
              <Bar dataKey="Cache" stackId="tokens" fill="#22d3ee" radius={[0, 0, 0, 0]} />
              <Bar dataKey="Output" stackId="tokens" fill="#a855f7" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </section>
  )
}

export default function UsagePage() {
  const { selectedTeam } = useTeamScope()
  const [range, setRange] = useState<Range>('30d')
  const [summary, setSummary] = useState<UsageSummary>(emptySummary)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const from = useMemo(() => {
    if (range === 'all') return undefined
    const days = Number(range.slice(0, -1))
    return new Date(Date.now() - days * 86400000).toISOString()
  }, [range])

  const loadUsage = useCallback(async () => {
    setLoading(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const data = await client.getUsage({ team_id: selectedTeam || undefined, from })
      setSummary({ ...emptySummary, ...data, by_day: data.by_day || [], by_model: data.by_model || [], by_session: data.by_session || [] })
      setError(null)
    } catch (err) {
      console.error('[UsagePage] Failed to fetch usage:', err)
      setError('利用量を取得できませんでした。proxyで利用量収集が有効か確認してください。')
    } finally {
      setLoading(false)
    }
  }, [from, selectedTeam])

  useEffect(() => { loadUsage() }, [loadUsage])

  const cacheTokens = summary.cached_input_tokens + summary.cache_creation_tokens
  const measuredTokens = summary.input_tokens + summary.output_tokens + cacheTokens

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
      <TopBar title="Usage" subtitle={selectedTeam ? `Team: ${selectedTeam}` : 'Personal usage'} showSettingsButton>
        <NavigationTabs className="w-44" />
      </TopBar>
      <div className="mx-auto max-w-7xl px-4 py-6 md:px-8 md:py-8">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
          <div className="inline-flex rounded-xl border border-gray-200 bg-white p-1 dark:border-gray-800 dark:bg-gray-900">
            {(['7d', '30d', '90d', 'all'] as Range[]).map((value) => (
              <button key={value} onClick={() => setRange(value)} className={`rounded-lg px-3 py-1.5 text-sm font-medium transition ${range === value ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'}`}>
                {value === 'all' ? 'All' : value.toUpperCase()}
              </button>
            ))}
          </div>
          <button onClick={loadUsage} disabled={loading} className="inline-flex items-center gap-2 rounded-xl border border-gray-200 bg-white px-3 py-2 text-sm text-gray-600 shadow-sm hover:bg-gray-50 disabled:opacity-50 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-300 dark:hover:bg-gray-800">
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> 更新
          </button>
        </div>

        {error ? (
          <div className="rounded-2xl border border-red-200 bg-red-50 px-5 py-8 text-center text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">{error}</div>
        ) : (
          <>
            <div className={`grid gap-4 sm:grid-cols-2 xl:grid-cols-4 ${loading ? 'animate-pulse opacity-60' : ''}`}>
              <SummaryCard label="Measured tokens" value={measuredTokens} detail={`${exactTokens(summary.events)} responses`} icon={<Activity className="h-5 w-5" />} />
              <SummaryCard label="Input" value={summary.input_tokens} detail="Non-cached input" icon={<ArrowDownToLine className="h-5 w-5" />} />
              <SummaryCard label="Output" value={summary.output_tokens} detail="Generated tokens" icon={<ArrowUpFromLine className="h-5 w-5" />} />
              <SummaryCard label="Cache" value={cacheTokens} detail={`${formatTokens(summary.cache_creation_tokens)} created`} icon={<Database className="h-5 w-5" />} />
            </div>
            {summary.reasoning_tokens > 0 && (
              <div className="mt-4 flex items-center gap-2 rounded-xl border border-violet-200 bg-violet-50 px-4 py-3 text-sm text-violet-700 dark:border-violet-900 dark:bg-violet-950/30 dark:text-violet-300">
                <BrainCircuit className="h-4 w-4" /> Reasoning tokens: <strong>{exactTokens(summary.reasoning_tokens)}</strong>
              </div>
            )}
            <UsageChart rows={summary.by_day} />
            <div className="mt-6 grid gap-6 lg:grid-cols-2">
              <BreakdownTable title="By model" rows={summary.by_model} />
              <BreakdownTable title="By session" rows={summary.by_session} sessions />
            </div>
          </>
        )}
      </div>
    </main>
  )
}

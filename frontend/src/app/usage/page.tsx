'use client'

import { useCallback, useMemo, useState } from 'react'
import { Area, Bar, CartesianGrid, ComposedChart, Legend, Line, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { Database, Play, RefreshCw } from 'lucide-react'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { queryUsageParquet } from '../../lib/usage-parquet'
import { usagePresets } from './presets'

type Range = '7d' | '30d' | '90d'
type ChartType = 'bar' | 'line' | 'area'
type QueryRow = Record<string, unknown>

const colors = ['#6366f1', '#06b6d4', '#a855f7', '#f59e0b', '#10b981', '#ef4444']

function displayValue(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'number') return new Intl.NumberFormat('ja-JP', { maximumFractionDigits: 2 }).format(value)
  return String(value)
}

export default function UsagePage() {
  const { selectedTeam } = useTeamScope()
  const initialPreset = usagePresets[0]
  const [range, setRange] = useState<Range>('30d')
  const [sql, setSql] = useState(initialPreset.sql)
  const [chartType, setChartType] = useState<ChartType>(initialPreset.chart)
  const [rows, setRows] = useState<QueryRow[]>([])
  const [xColumn, setXColumn] = useState('')
  const [yColumns, setYColumns] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const exportUrl = useMemo(() => {
    const days = Number(range.slice(0, -1))
    const params = new URLSearchParams({ from: new Date(Date.now() - days * 86400000).toISOString() })
    if (selectedTeam) params.set('team_id', selectedTeam)
    return `/api/proxy/usage/export.parquet?${params}`
  }, [range, selectedTeam])

  const columns = rows.length > 0 ? Object.keys(rows[0]) : []
  const numericColumns = columns.filter((column) => rows.some((row) => typeof row[column] === 'number'))
  const chartRows = rows.map((row) => Object.fromEntries(Object.entries(row).map(([key, value]) => [key, value instanceof Date ? value.toISOString() : value])))

  const runQuery = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const result = await queryUsageParquet<QueryRow>(exportUrl, sql)
      setRows(result)
      const nextColumns = result.length > 0 ? Object.keys(result[0]) : []
      const nextNumeric = nextColumns.filter((column) => result.some((row) => typeof row[column] === 'number'))
      setXColumn(nextColumns.find((column) => !nextNumeric.includes(column)) || nextColumns[0] || '')
      setYColumns(nextNumeric.slice(0, 4))
    } catch (err) {
      console.error('[UsageWorkbench] Query failed:', err)
      setError(err instanceof Error ? err.message : 'SQLの実行に失敗しました')
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [exportUrl, sql])

  const selectPreset = (preset: typeof usagePresets[number]) => {
    setSql(preset.sql)
    setChartType(preset.chart)
  }

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
      <TopBar title="Usage SQL" subtitle={selectedTeam ? `Team: ${selectedTeam}` : 'Personal usage'} showSettingsButton>
        <NavigationTabs className="w-44" />
      </TopBar>
      <div className="mx-auto max-w-7xl space-y-6 px-4 py-6 md:px-8">
        <section className="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white"><Database className="h-4 w-4 text-indigo-500" /> Parquet query</div>
            <div className="inline-flex rounded-lg border border-gray-200 p-1 dark:border-gray-700">
              {(['7d', '30d', '90d'] as Range[]).map((value) => <button key={value} onClick={() => setRange(value)} className={`rounded-md px-3 py-1 text-xs font-medium ${range === value ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-500'}`}>{value.toUpperCase()}</button>)}
            </div>
          </div>
          <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
            {usagePresets.map((preset) => <button key={preset.id} onClick={() => selectPreset(preset)} className="rounded-xl border border-gray-200 p-3 text-left hover:border-indigo-400 hover:bg-indigo-50/50 dark:border-gray-700 dark:hover:border-indigo-600 dark:hover:bg-indigo-950/20"><span className="block text-sm font-medium text-gray-800 dark:text-gray-200">{preset.label}</span><span className="mt-1 block text-xs text-gray-400">{preset.description}</span></button>)}
          </div>
          <textarea value={sql} onChange={(event) => setSql(event.target.value)} spellCheck={false} className="mt-4 min-h-64 w-full resize-y rounded-xl border border-gray-800 bg-gray-950 p-4 font-mono text-sm leading-6 text-gray-100 outline-none focus:border-indigo-500" aria-label="Usage SQL" />
          <div className="mt-3 flex items-center justify-between gap-3">
            <p className="truncate text-xs text-gray-400">SQLはブラウザ内のDuckDB-Wasmでのみ実行されます</p>
            <button onClick={runQuery} disabled={loading || !sql.trim()} className="inline-flex items-center gap-2 rounded-xl bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-50">{loading ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />} 実行</button>
          </div>
          {error && <p className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{error}</p>}
        </section>

        {rows.length > 0 && <>
          <section className="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
            <div className="flex flex-wrap items-center gap-3">
              <select value={chartType} onChange={(event) => setChartType(event.target.value as ChartType)} className="rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900"><option value="bar">棒グラフ</option><option value="line">折れ線</option><option value="area">エリア</option></select>
              <label className="text-xs text-gray-500">X軸 <select value={xColumn} onChange={(event) => setXColumn(event.target.value)} className="ml-1 rounded-lg border border-gray-200 bg-white px-2 py-2 text-sm dark:border-gray-700 dark:bg-gray-900">{columns.map((column) => <option key={column}>{column}</option>)}</select></label>
              <div className="flex flex-wrap gap-2">{numericColumns.map((column) => <label key={column} className="flex items-center gap-1 text-xs text-gray-500"><input type="checkbox" checked={yColumns.includes(column)} onChange={() => setYColumns((current) => current.includes(column) ? current.filter((item) => item !== column) : [...current, column])} />{column}</label>)}</div>
            </div>
            <div className="mt-5 h-96">
              <ResponsiveContainer width="100%" height="100%"><ComposedChart data={chartRows}><CartesianGrid strokeDasharray="3 3" vertical={false} /><XAxis dataKey={xColumn} tick={{ fontSize: 11 }} minTickGap={20} /><YAxis tick={{ fontSize: 11 }} /><Tooltip formatter={(value) => displayValue(value)} /><Legend />{yColumns.map((column, index) => chartType === 'bar' ? <Bar key={column} dataKey={column} fill={colors[index % colors.length]} /> : chartType === 'area' ? <Area key={column} dataKey={column} stroke={colors[index % colors.length]} fill={colors[index % colors.length]} fillOpacity={0.18} /> : <Line key={column} dataKey={column} stroke={colors[index % colors.length]} dot={false} strokeWidth={2} />)}</ComposedChart></ResponsiveContainer>
            </div>
          </section>
          <section className="overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-gray-800 dark:bg-gray-900"><div className="border-b border-gray-200 px-5 py-3 text-sm font-medium dark:border-gray-800">Result: {rows.length.toLocaleString()} rows</div><div className="max-h-96 overflow-auto"><table className="min-w-full text-left text-xs"><thead className="sticky top-0 bg-gray-100 dark:bg-gray-800"><tr>{columns.map((column) => <th key={column} className="whitespace-nowrap px-4 py-2 font-semibold">{column}</th>)}</tr></thead><tbody className="divide-y divide-gray-100 dark:divide-gray-800">{rows.slice(0, 200).map((row, index) => <tr key={index}>{columns.map((column) => <td key={column} className="whitespace-nowrap px-4 py-2 font-mono">{displayValue(row[column])}</td>)}</tr>)}</tbody></table></div>{rows.length > 200 && <p className="px-5 py-3 text-xs text-gray-400">先頭200行を表示しています</p>}</section>
        </>}
      </div>
    </main>
  )
}

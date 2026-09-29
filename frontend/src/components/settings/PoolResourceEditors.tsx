'use client'

import { FormEvent, useState } from 'react'
import type { LogicalSessionPool, SessionPoolBinding, SessionPoolSupplier } from '@/types/session_pool'

const input = 'w-full rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-900 dark:border-gray-700 dark:bg-gray-950 dark:text-white'
const button = 'rounded-md border border-blue-300 px-2.5 py-1.5 text-xs font-medium text-blue-700 hover:bg-blue-50 disabled:opacity-50 dark:border-blue-800 dark:text-blue-300 dark:hover:bg-blue-950/30'

function labelsToText(labels?: Record<string, string>) {
  return Object.entries(labels ?? {}).map(([key, value]) => `${key}=${value}`).join('\n')
}

function textToLabels(value: string) {
  return Object.fromEntries(value.split('\n').map((line) => line.trim()).filter(Boolean).map((line) => {
    const separator = line.indexOf('=')
    if (separator < 1) throw new Error(`ラベルは key=value 形式で入力してください: ${line}`)
    return [line.slice(0, separator).trim(), line.slice(separator + 1).trim()]
  }))
}

export function PoolSettingsEditor({ pool, onSave }: {
  pool: LogicalSessionPool
  onSave: (patch: { enabled?: boolean; labels?: Record<string, string> }) => Promise<void>
}) {
  const [editing, setEditing] = useState(false)
  const [labels, setLabels] = useState(labelsToText(pool.labels))
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setError('')
    setSaving(true)
    try {
      await onSave({ labels: textToLabels(labels) })
      setEditing(false)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Poolの更新に失敗しました')
    } finally {
      setSaving(false)
    }
  }

  if (!editing) return <button type="button" className={button} onClick={() => { setLabels(labelsToText(pool.labels)); setEditing(true) }}>設定を編集</button>
  return <form onSubmit={submit} className="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
    <label className="block text-xs text-gray-500 dark:text-gray-400">Labels（1行に key=value）
      <textarea aria-label={`${pool.name}のLabels`} rows={3} className={`${input} mt-1 font-mono`} value={labels} onChange={(event) => setLabels(event.target.value)} placeholder={'arch=amd64\nregion=ap-northeast-1'} />
    </label>
    {error && <p role="alert" className="text-xs text-red-600 dark:text-red-400">{error}</p>}
    <div className="flex gap-2"><button disabled={saving} className={button}>{saving ? '保存中...' : '保存'}</button><button type="button" className={button} onClick={() => setEditing(false)}>キャンセル</button></div>
  </form>
}

export function SupplierSettingsEditor({ supplier, onSave }: {
  supplier: SessionPoolSupplier
  onSave: (patch: { min_idle?: number; max_runners?: number; enabled?: boolean; draining?: boolean }) => Promise<void>
}) {
  const [editing, setEditing] = useState(false)
  const [minIdle, setMinIdle] = useState(supplier.min_idle ?? 0)
  const [maxRunners, setMaxRunners] = useState(supplier.max_runners ?? 0)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setError('')
    setSaving(true)
    try { await onSave({ min_idle: minIdle, max_runners: maxRunners }); setEditing(false) } catch (reason) { setError(reason instanceof Error ? reason.message : 'Supplierの更新に失敗しました') } finally { setSaving(false) }
  }

  if (!editing) return <button type="button" className={button} onClick={() => { setMinIdle(supplier.min_idle ?? 0); setMaxRunners(supplier.max_runners ?? 0); setEditing(true) }}>編集</button>
  return <form onSubmit={submit} className="grid w-full gap-2 rounded-lg border border-gray-200 p-3 sm:grid-cols-2 dark:border-gray-700">
    <label className="text-xs text-gray-500">Min idle<input aria-label="Min idle" required min={0} type="number" className={`${input} mt-1`} value={minIdle} onChange={(event) => setMinIdle(Number(event.target.value))} /></label>
    <label className="text-xs text-gray-500">Max runners（0は無制限）<input aria-label="Max runners" required min={0} type="number" className={`${input} mt-1`} value={maxRunners} onChange={(event) => setMaxRunners(Number(event.target.value))} /></label>
    {error && <p role="alert" className="text-xs text-red-600 sm:col-span-2 dark:text-red-400">{error}</p>}
    <div className="flex gap-2 sm:col-span-2"><button disabled={saving} className={button}>{saving ? '保存中...' : '保存'}</button><button type="button" className={button} onClick={() => setEditing(false)}>キャンセル</button></div>
  </form>
}

export function BindingSettingsEditor({ binding, onSave }: {
  binding: SessionPoolBinding
  onSave: (patch: { roles?: Array<'use' | 'manage'>; enabled?: boolean; priority?: number; max_concurrent?: number }) => Promise<void>
}) {
  const initialRoles = binding.roles ?? (binding.role === 'manage_and_use' ? ['use', 'manage'] : [binding.role])
  const [editing, setEditing] = useState(false)
  const [roles, setRoles] = useState<Array<'use' | 'manage'>>(initialRoles)
  const [priority, setPriority] = useState(binding.priority ?? 0)
  const [maxConcurrent, setMaxConcurrent] = useState(binding.max_concurrent ?? 0)
  const [enabled, setEnabled] = useState(binding.enabled)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (roles.length === 0) return
    setError('')
    setSaving(true)
    try { await onSave({ roles, priority, max_concurrent: maxConcurrent, enabled }); setEditing(false) } catch (reason) { setError(reason instanceof Error ? reason.message : 'Bindingの更新に失敗しました') } finally { setSaving(false) }
  }

  if (!editing) return <button type="button" className={button} onClick={() => { setRoles(initialRoles); setPriority(binding.priority ?? 0); setMaxConcurrent(binding.max_concurrent ?? 0); setEnabled(binding.enabled); setEditing(true) }}>編集</button>
  return <form onSubmit={submit} className="mt-2 grid gap-2 rounded-lg border border-gray-200 p-3 sm:grid-cols-2 dark:border-gray-700">
    <fieldset className="flex gap-4 sm:col-span-2"><legend className="text-xs text-gray-500">Roles</legend>{(['use', 'manage'] as const).map((role) => <label key={role} className="flex items-center gap-1.5 text-sm"><input type="checkbox" checked={roles.includes(role)} disabled={binding.subject_type === 'all' && role === 'manage'} onChange={(event) => setRoles((current) => event.target.checked ? [...current, role] : current.filter((value) => value !== role))} />{role}</label>)}</fieldset>
    <label className="text-xs text-gray-500">Priority<input aria-label="Priority" type="number" className={`${input} mt-1`} value={priority} onChange={(event) => setPriority(Number(event.target.value))} /></label>
    <label className="text-xs text-gray-500">Max concurrent（0は無制限）<input aria-label="Max concurrent" min={0} type="number" className={`${input} mt-1`} value={maxConcurrent} onChange={(event) => setMaxConcurrent(Number(event.target.value))} /></label>
    <label className="flex items-center gap-2 text-sm sm:col-span-2"><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />有効</label>
    {error && <p role="alert" className="text-xs text-red-600 sm:col-span-2 dark:text-red-400">{error}</p>}
    <div className="flex gap-2 sm:col-span-2"><button disabled={saving || roles.length === 0} className={button}>{saving ? '保存中...' : '保存'}</button><button type="button" className={button} onClick={() => setEditing(false)}>キャンセル</button></div>
  </form>
}

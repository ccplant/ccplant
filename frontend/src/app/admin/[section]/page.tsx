'use client'

import { use, useEffect, useState } from 'react'
import { notFound } from 'next/navigation'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { AdminSettingsDocument, AdminSettingsSections } from '@/types/admin-settings'
import { getAdminSection, AdminField } from '../config'
import { useToast } from '@/contexts/ToastContext'

function getValue(section: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((value, key) => value && typeof value === 'object' ? (value as Record<string, unknown>)[key] : undefined, section)
}

function setValue(section: Record<string, unknown>, path: string, value: unknown): Record<string, unknown> {
  const copy = structuredClone(section)
  const parts = path.split('.')
  let current = copy
  parts.slice(0, -1).forEach((part) => {
    if (!current[part] || typeof current[part] !== 'object') current[part] = {}
    current = current[part] as Record<string, unknown>
  })
  current[parts[parts.length - 1]] = value
  return copy
}

interface TeamRoleRule {
  role: string
  permissions: string[]
  env_file?: string
}

interface TeamRoleRow extends TeamRoleRule {
  team: string
}

function parseTeamRoleRows(value: unknown): { rows: TeamRoleRow[]; error?: string } {
  if (value === undefined || value === null || value === '') return { rows: [] }
  try {
    const parsed = typeof value === 'string' ? JSON.parse(value) : value
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('mapping must be an object')
    return {
      rows: Object.entries(parsed as Record<string, unknown>).map(([team, rawRule]) => {
        const rule = rawRule && typeof rawRule === 'object' ? rawRule as Record<string, unknown> : {}
        return {
          team,
          role: typeof rule.role === 'string' ? rule.role : 'user',
          permissions: Array.isArray(rule.permissions) ? rule.permissions.filter((item): item is string => typeof item === 'string') : [],
          env_file: typeof rule.env_file === 'string' ? rule.env_file : '',
        }
      }),
    }
  } catch {
    return { rows: [], error: '既存のJSONを読み込めません。JSONを修正してからフォーム編集に切り替えてください。' }
  }
}

function serializeTeamRoleRows(rows: TeamRoleRow[]): string {
  const mapping = Object.fromEntries(rows.filter((row) => row.team.trim()).map(({ team, role, permissions, env_file }) => [
    team.trim(),
    { role: role.trim() || 'user', permissions, ...(env_file?.trim() ? { env_file: env_file.trim() } : {}) },
  ]))
  return JSON.stringify(mapping, null, 2)
}

function TeamRoleMappingField({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
  const parsed = parseTeamRoleRows(value)
  const inputClass = 'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-900 dark:text-white'

  if (parsed.error) return <div className="mt-2 space-y-2"><p className="text-xs text-red-600 dark:text-red-400">{parsed.error}</p><textarea rows={6} className={inputClass} value={typeof value === 'string' ? value : JSON.stringify(value, null, 2)} onChange={(event) => onChange(event.target.value)} /></div>

  return <TeamRoleMappingBuilder initialRows={parsed.rows} onChange={onChange} />
}

function TeamRoleMappingBuilder({ initialRows, onChange }: { initialRows: TeamRoleRow[]; onChange: (value: unknown) => void }) {
  const [rows, setRows] = useState(initialRows)
  const inputClass = 'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-900 dark:text-white'
  const updateRows = (next: TeamRoleRow[]) => {
    setRows(next)
    onChange(serializeTeamRoleRows(next))
  }
  const normalizedTeams = rows.map((row) => row.team.trim()).filter(Boolean)
  const hasDuplicate = new Set(normalizedTeams).size !== normalizedTeams.length

  return <div className="mt-2 space-y-3">
    {rows.map((row, index) => <div key={index} className="rounded-lg border border-gray-200 p-4 dark:border-gray-700">
      <div className="grid gap-3 md:grid-cols-2">
        <div><label className="text-xs font-medium text-gray-600 dark:text-gray-400">Organization / Team</label><input className={inputClass} value={row.team} placeholder="example-org/platform" onChange={(event) => updateRows(rows.map((item, itemIndex) => itemIndex === index ? { ...item, team: event.target.value } : item))} /></div>
        <div><label className="text-xs font-medium text-gray-600 dark:text-gray-400">Role</label><input className={inputClass} list="admin-team-role-options" value={row.role} placeholder="user または admin" onChange={(event) => updateRows(rows.map((item, itemIndex) => itemIndex === index ? { ...item, role: event.target.value } : item))} /></div>
        <div><label className="text-xs font-medium text-gray-600 dark:text-gray-400">Permissions</label><textarea rows={2} className={inputClass} value={row.permissions.join('\n')} placeholder="1行に1つ（例: session:access）" onChange={(event) => updateRows(rows.map((item, itemIndex) => itemIndex === index ? { ...item, permissions: event.target.value.split(/[\n,]/).map((item) => item.trim()).filter(Boolean) } : item))} /></div>
        <div><label className="text-xs font-medium text-gray-600 dark:text-gray-400">Env File（任意）</label><input className={inputClass} value={row.env_file || ''} placeholder="admin.env" onChange={(event) => updateRows(rows.map((item, itemIndex) => itemIndex === index ? { ...item, env_file: event.target.value } : item))} /></div>
      </div>
      <div className="mt-3 text-right"><button type="button" className="text-sm text-red-600 hover:text-red-700 dark:text-red-400" onClick={() => updateRows(rows.filter((_, itemIndex) => itemIndex !== index))}>このTeamを削除</button></div>
    </div>)}
    <datalist id="admin-team-role-options"><option value="user" /><option value="admin" /></datalist>
    <button type="button" className="rounded-md border border-blue-600 px-3 py-2 text-sm font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/30" onClick={() => updateRows([...rows, { team: '', role: 'user', permissions: [] }])}>＋ Teamを追加</button>
    {hasDuplicate && <p className="text-xs text-red-600 dark:text-red-400">同じ Organization / Team が複数あります。最後の設定だけが保存されます。</p>}
    {rows.length === 0 && <p className="text-xs text-gray-500">Team mapping はまだありません。「Teamを追加」から作成できます。</p>}
  </div>
}

function Field({ field, value, configured, onChange }: { field: AdminField; value: unknown; configured: boolean; onChange: (value: unknown) => void }) {
  const baseClass = 'mt-1 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-900 dark:text-white'
  if (field.type === 'toggle') return <button type="button" role="switch" aria-checked={Boolean(value)} onClick={() => onChange(!value)} className={`relative mt-2 inline-flex h-6 w-11 rounded-full transition-colors ${value ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-600'}`}><span className={`inline-block h-5 w-5 translate-y-0.5 rounded-full bg-white shadow transition-transform ${value ? 'translate-x-5' : 'translate-x-0.5'}`} /></button>
  if (field.type === 'team-role-mapping') return <TeamRoleMappingField value={value} onChange={onChange} />
  if (field.type === 'textarea') return <textarea rows={4} className={baseClass} value={typeof value === 'string' ? value : ''} placeholder={field.placeholder} onChange={(event) => onChange(event.target.value)} />
  if (field.type === 'select') return <select className={baseClass} value={typeof value === 'string' ? value : ''} onChange={(event) => onChange(event.target.value)}>{field.options?.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select>
  return <div><input className={baseClass} type={field.type === 'secret' ? 'password' : field.type} value={typeof value === 'string' || typeof value === 'number' ? value : ''} placeholder={field.type === 'secret' && configured ? '設定済み（変更時のみ入力）' : field.placeholder} onChange={(event) => onChange(field.type === 'number' ? (event.target.value === '' ? '' : Number(event.target.value)) : event.target.value)} />{field.type === 'secret' && configured && <p className="mt-1 text-xs text-emerald-600">現在の値が設定されています</p>}</div>
}

export default function AdminSectionPage({ params }: { params: Promise<{ section: string }> }) {
  const { section: sectionID } = use(params)
  const definition = getAdminSection(sectionID)
  const [document, setDocument] = useState<AdminSettingsDocument | null>(null)
  const [sections, setSections] = useState<AdminSettingsSections>({})
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { showToast } = useToast()

  useEffect(() => {
    createAgentAPIProxyClientFromStorage().getAdminSettings().then((data) => { setDocument(data); setSections(data.sections || {}) }).catch(() => setError('設定を読み込めませんでした'))
  }, [])

  if (!definition) notFound()
  if (!document && !error) return <div className="rounded-lg border bg-white p-8 text-center text-gray-500 dark:border-gray-700 dark:bg-gray-800">設定を読み込んでいます…</div>

  const section = sections[sectionID] || {}
  const changed = document ? JSON.stringify(sections) !== JSON.stringify(document.sections) : false
  const save = async () => {
    if (!document) return
    setSaving(true); setError(null)
    try {
      const saved = await createAgentAPIProxyClientFromStorage().updateAdminSettings({ base_version: document.version, sections })
      setDocument(saved); setSections(saved.sections); showToast(`設定をversion ${saved.version}として保存しました`, 'success')
    } catch (err) {
      setError(err instanceof Error && err.message.includes('409') ? '別の管理者が更新しました。再読み込みしてください。' : '設定の保存に失敗しました')
    } finally { setSaving(false) }
  }

  return <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-700 dark:bg-gray-800">
    <div className="mb-6 flex items-start justify-between gap-4"><div><h2 className="text-2xl font-bold text-gray-900 dark:text-white">{definition.title}</h2><p className="mt-1 text-sm text-gray-600 dark:text-gray-400">{definition.description}</p></div><span className="whitespace-nowrap rounded-full bg-gray-100 px-3 py-1 text-xs text-gray-600 dark:bg-gray-700 dark:text-gray-300">version {document?.version || 0}</span></div>
    {error && <div className="mb-5 rounded-md bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{error}</div>}
    <div className="space-y-6">{definition.fields.map((field) => <div key={field.path}><label className="block text-sm font-medium text-gray-800 dark:text-gray-200">{field.label}</label>{field.description && <p className="mt-0.5 text-xs text-gray-500">{field.description}</p>}<Field field={field} value={getValue(section, field.path)} configured={Boolean(document?.secret_configured[`${sectionID}.${field.path}`])} onChange={(value) => setSections((previous) => ({ ...previous, [sectionID]: setValue(previous[sectionID] || {}, field.path, value) }))} /></div>)}</div>
    <div className="mt-8 flex items-center justify-between border-t border-gray-200 pt-5 dark:border-gray-700"><span className="text-sm text-amber-600">{changed ? '未保存の変更があります' : ''}</span><button onClick={save} disabled={!changed || saving} className="rounded-md bg-blue-600 px-5 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-50">{saving ? '保存中…' : '新しいversionとして保存'}</button></div>
  </div>
}

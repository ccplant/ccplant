'use client'

import { useEffect, useState } from 'react'
import { FilterGroup, SessionFilter } from '../../lib/filter-utils'

interface Props { filterGroups: FilterGroup[]; currentFilters: SessionFilter; onFiltersChange: (filters: SessionFilter) => void; isVisible?: boolean; onToggleVisibility?: () => void }
const statusOptions = [
  ['all', 'All Statuses'], ['creating', '作成中'], ['starting', '起動中'], ['error', '起動失敗'],
  ['timeout', '起動タイムアウト'], ['active', 'Active'], ['unhealthy', 'Unhealthy'], ['stopped', 'Stopped'],
] as const

function filterBucket(filters: SessionFilter, groupKey: string) {
  if (groupKey.startsWith('tags.')) return { values: filters.tagFilters, key: groupKey.slice(5) }
  if (groupKey.startsWith('metadata.')) return { values: filters.metadataFilters, key: groupKey.slice(9) }
  if (groupKey.startsWith('environment.')) return { values: filters.environmentFilters, key: groupKey.slice(12) }
  return null
}

function changeFilterValue(filters: SessionFilter, groupKey: string, value: string, checked: boolean) {
  const next = { ...filters, tagFilters: { ...filters.tagFilters }, metadataFilters: { ...filters.metadataFilters }, environmentFilters: { ...filters.environmentFilters } }
  const bucket = filterBucket(next, groupKey)
  if (!bucket) return next
  const current = bucket.values[bucket.key] || []
  const values = checked ? [...current, value] : current.filter((item) => item !== value)
  if (values.length) bucket.values[bucket.key] = values; else delete bucket.values[bucket.key]
  return next
}

function StatusFilters({ filters, onChange }: { filters: SessionFilter; onChange: (filters: SessionFilter) => void }) {
  return <div className="mb-6"><h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">Status</h3><div className="space-y-2">{statusOptions.map(([value, label]) => <label key={value} className="flex items-center cursor-pointer"><input type="radio" name="status" value={value} checked={value === 'all' ? !filters.status : filters.status === value} onChange={() => onChange({ ...filters, status: value === 'all' ? undefined : value })} className="mr-2 text-blue-600 focus:ring-blue-500" /><span className="text-sm text-gray-900 dark:text-gray-100">{label}</span></label>)}</div></div>
}

interface GroupsProps { groups: FilterGroup[]; filters: SessionFilter; expanded: Set<string>; onToggle: (key: string) => void; onChange: (filters: SessionFilter) => void }
function DynamicGroups({ groups, filters, expanded, onToggle, onChange }: GroupsProps) {
  if (!groups.length) return <div className="text-sm text-gray-500 dark:text-gray-400 text-center py-8">No metadata or environment filters available. Start some sessions to see filter options.</div>
  return <>{groups.map((group) => <div key={group.key} className="mb-4">
    <button onClick={() => onToggle(group.key)} className="flex items-center justify-between w-full text-left text-sm font-medium text-gray-700 dark:text-gray-300 mb-2 hover:text-gray-900 dark:hover:text-gray-100"><span>{group.label}</span><svg className={`w-4 h-4 transition-transform ${expanded.has(group.key) ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" /></svg></button>
    {expanded.has(group.key) && <div className="space-y-2 pl-2 max-h-48 overflow-y-auto">{group.values.map((item) => <label key={item.value} className="flex items-center justify-between cursor-pointer group"><div className="flex items-center min-w-0 flex-1"><input type="checkbox" checked={filterBucket(filters, group.key)?.values[filterBucket(filters, group.key)?.key || '']?.includes(item.value) || false} onChange={(event) => onChange(changeFilterValue(filters, group.key, item.value, event.target.checked))} className="mr-2 text-blue-600 focus:ring-blue-500 flex-shrink-0" /><span className="text-sm text-gray-900 dark:text-gray-100 truncate" title={item.value}>{item.value}</span></div><span className="text-xs text-gray-500 dark:text-gray-400 ml-2 flex-shrink-0">({item.count})</span></label>)}</div>}
  </div>)}</>
}

export default function SessionFilterSidebar({ filterGroups, currentFilters, onFiltersChange, isVisible = true, onToggleVisibility }: Props) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape' && isVisible) onToggleVisibility?.() }
    if (isVisible) document.addEventListener('keydown', close)
    return () => document.removeEventListener('keydown', close)
  }, [isVisible, onToggleVisibility])
  const toggle = (key: string) => setExpanded((current) => { const next = new Set(current); if (next.has(key)) next.delete(key); else next.add(key); return next })
  const active = Boolean(currentFilters.status || Object.keys(currentFilters.metadataFilters).length || Object.keys(currentFilters.environmentFilters).length || Object.keys(currentFilters.tagFilters).length)
  const clear = () => onFiltersChange({ metadataFilters: {}, environmentFilters: {}, tagFilters: {}, status: undefined })
  return <>
    <div className={`fixed inset-y-0 left-0 z-10 w-80 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 transform transition-transform duration-300 ease-in-out overflow-y-auto md:relative md:translate-x-0 md:inset-auto md:w-80 md:h-screen ${isVisible ? 'translate-x-0' : '-translate-x-full'}`}><div className="p-4">
      <div className="flex items-center justify-between mb-4"><h2 className="text-lg font-semibold text-gray-900 dark:text-white">Filters</h2><div className="flex items-center gap-2">{onToggleVisibility && <button onClick={onToggleVisibility} className="md:hidden text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"><svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" /></svg></button>}{active && <button onClick={clear} className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300">Clear All</button>}</div></div>
      <StatusFilters filters={currentFilters} onChange={onFiltersChange} />
      <DynamicGroups groups={filterGroups} filters={currentFilters} expanded={expanded} onToggle={toggle} onChange={onFiltersChange} />
    </div></div>
    {isVisible && onToggleVisibility && <div className="md:hidden fixed inset-0 bg-black bg-opacity-50 z-0" onClick={onToggleVisibility} />}
  </>
}

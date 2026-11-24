import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { useTeamScope } from '../../contexts/TeamScopeContext'

function ScopeIcon({ team }: { team: boolean }) {
  return <svg className={`w-4 h-4 mr-2 ${team ? 'text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={team ? 'M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656-.126-1.283-.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0z' : 'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z'} /></svg>
}

function TeamMenu({ selected, teams, names, choose }: { selected: string | null; teams: string[]; names: Record<string, string>; choose: (team: string | null) => void }) {
  const optionClass = (active: boolean) => `w-full flex items-center px-3 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors ${active ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300' : 'text-gray-900 dark:text-white'}`
  return <div className="absolute right-0 mt-2 w-64 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50"><div className="py-1">
    <div className="px-3 py-2 text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">スコープを選択</div>
    <button onClick={() => choose(null)} className={optionClass(!selected)}><ScopeIcon team={false} /><span>Personal</span>{!selected && <span className="ml-auto text-blue-600">✓</span>}</button>
    <div className="my-1 border-t border-gray-200 dark:border-gray-700" /><div className="px-3 py-2 text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Teams</div>
    {teams.map((team) => <button key={team} onClick={() => choose(team)} className={optionClass(selected === team)}><ScopeIcon team /><span className="truncate">{names[team] || team}</span>{selected === team && <span className="ml-auto text-blue-600">✓</span>}</button>)}
    <div className="my-1 border-t border-gray-200 dark:border-gray-700" /><Link href="/settings/team" onClick={() => choose(selected)} className="flex w-full items-center px-3 py-2 text-sm font-medium text-blue-600 transition-colors hover:bg-gray-100 dark:text-blue-400 dark:hover:bg-gray-700"><span className="mr-3 text-lg leading-none">+</span>新しいチームを作成</Link>
  </div></div>
}

export default function TeamScopeSelector() {
  const [open, setOpen] = useState(false)
  const [names, setNames] = useState<Record<string, string>>({})
  const ref = useRef<HTMLDivElement>(null)
  const { selectedTeam, availableTeams, selectTeam, setAvailableTeams, isLoading } = useTeamScope()
  useEffect(() => { void fetch('/api/user/info').then((response) => response.ok ? response.json() : null).then((info) => { if (!info) return; if (Array.isArray(info.proxy?.teams)) setAvailableTeams(info.proxy.teams); if (Array.isArray(info.proxy?.team_principals)) setNames(Object.fromEntries(info.proxy.team_principals.map((team: { team_id: string; name?: string }) => [team.team_id, team.name || team.team_id]))); if (Array.isArray(info.proxy?.repositories)) sessionStorage.setItem('user_repositories', JSON.stringify(info.proxy.repositories)) }).catch((error) => console.error('Failed to fetch user info for teams:', error)) }, [setAvailableTeams])
  useEffect(() => { const close = (event: MouseEvent) => { if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false) }; if (open) document.addEventListener('mousedown', close); return () => document.removeEventListener('mousedown', close) }, [open])
  const choose = (team: string | null) => { selectTeam(team); setOpen(false) }
  const displayName = isLoading ? '...' : selectedTeam ? names[selectedTeam] || selectedTeam.split('/').pop() : 'Personal'
  return <div className="relative" ref={ref}><button onClick={() => setOpen((value) => !value)} className="inline-flex items-center px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors" title="スコープを選択"><ScopeIcon team={Boolean(selectedTeam)} /><span className="hidden sm:inline max-w-24 truncate">{displayName}</span><span className="w-4 h-4 ml-1">⌄</span></button>{open && <TeamMenu selected={selectedTeam} teams={availableTeams} names={names} choose={choose} />}</div>
}

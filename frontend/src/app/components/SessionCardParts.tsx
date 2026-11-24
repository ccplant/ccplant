import { useState } from 'react'
import Link from 'next/link'
import { CircleDot, GitPullRequest, LoaderCircle, MoreHorizontal } from 'lucide-react'
import { Session } from '../../types/agentapi'
import StatusBadge from './StatusBadge'
import { formatRelativeTime } from '../../utils/timeUtils'
import { truncateText } from '../../utils/textUtils'
import type { SessionCardProps } from './SessionCard'

export function getSessionAnnotations(session: Session) {
  const pick = (key: 'pr_url' | 'issue_url' | 'description' | 'running_task') => { const value = session.annotations?.[key]; return typeof value === 'string' ? value.trim() : '' }
  return { prUrl: pick('pr_url'), issueUrl: pick('issue_url'), description: pick('description'), runningTask: pick('running_task') }
}
type Annotations = ReturnType<typeof getSessionAnnotations>
const urlNumber = (url: string) => url.match(/\/(?:pull|issues)\/(\d+)(?:[/?#]|$)/)?.[1] ?? null

export function SessionSelectButton({ session, selected, selectionMode, onToggle }: { session: Session; selected: boolean; selectionMode: boolean; onToggle?: (id: string) => void }) {
  if (!onToggle) return null
  return <div className="flex-shrink-0 pt-1"><button onClick={(event) => { event.stopPropagation(); onToggle(session.session_id) }} aria-label={`セッション ${session.session_id.substring(0, 8)} を選択`} className={`flex w-4 h-4 rounded border-2 items-center justify-center transition-all duration-150 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-1 ${selected ? 'bg-blue-600 border-blue-600 opacity-100' : selectionMode ? 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 hover:border-blue-400 opacity-100' : 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 hover:border-blue-400 opacity-0 group-hover:opacity-100'}`}>{selected && <svg className="w-2.5 h-2.5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" /></svg>}</button></div>
}

function RunningTask({ task, isMobile }: { task: string; isMobile: boolean }) {
  const [expanded, setExpanded] = useState(false)
  return <div className="mb-2 min-w-0"><button type="button" onClick={(event) => { event.stopPropagation(); setExpanded((value) => !value) }} className="flex max-w-full items-start gap-1.5 rounded text-left text-xs text-gray-500 hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-1 dark:text-gray-400 dark:hover:text-gray-200" aria-expanded={expanded} aria-label={`${expanded ? '進捗を閉じる' : '進捗を全文表示'}: ${task}`}><LoaderCircle className="mt-px h-3 w-3 flex-shrink-0 animate-spin" aria-hidden="true" /><span className={expanded ? 'break-words [overflow-wrap:anywhere]' : 'truncate'}>{expanded ? task : truncateText(task, isMobile ? 32 : 64)}</span></button></div>
}

export function SessionCardBody({ session, annotations, isMobile }: { session: Session; annotations: Annotations; isMobile: boolean }) {
  const title = annotations.description || String(session.tags?.description || session.metadata?.description || `Session ${session.session_id.substring(0, 8)}`)
  const metadata = session.metadata ? Object.entries(session.metadata).filter(([key]) => key !== 'description') : []
  return <div className="flex-1 min-w-0">
    <div className="flex min-w-0 items-start gap-3 mb-2"><h3 className="min-w-0 break-words [overflow-wrap:anywhere] text-sm sm:text-base font-medium text-gray-900 dark:text-white leading-5 sm:leading-6">{truncateText(title, isMobile ? 100 : 120)}</h3><div className="flex-shrink-0"><StatusBadge status={session.status} /></div></div>
    {annotations.runningTask && <RunningTask task={annotations.runningTask} isMobile={isMobile} />}
    <div className="flex flex-col sm:flex-row sm:items-center text-xs text-gray-500 dark:text-gray-400 space-y-1 sm:space-y-0 sm:space-x-4 mb-3"><span>#{session.session_id.substring(0, 8)}</span><div className="flex items-center space-x-2 sm:space-x-4"><span>開始: {formatRelativeTime(session.started_at)}</span>{session.updated_at && <span className="hidden sm:inline">更新: {formatRelativeTime(session.updated_at)}</span>}</div><span className="hidden sm:inline">by {session.user_id}</span>{session.environment?.WORKSPACE_NAME && <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-300 self-start">{session.environment.WORKSPACE_NAME}</span>}</div>
    {metadata.length > 0 && <div className="flex flex-wrap gap-1 sm:gap-2">{metadata.slice(0, isMobile ? 2 : 10).map(([key, value]) => <span key={key} className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 rounded-full"><span className="hidden sm:inline">{key}: </span><span className="sm:hidden">{key.substring(0, 8)}: </span>{String(value).substring(0, isMobile ? 8 : 20)}</span>)}{metadata.length > 2 && isMobile && <span className="inline-flex items-center px-2 py-1 text-xs bg-gray-100 dark:bg-gray-900 text-gray-600 dark:text-gray-400 rounded-full">+{metadata.length - 2}</span>}</div>}
  </div>
}

function AnnotationMenu({ annotations }: { annotations: Annotations }) {
  const [open, setOpen] = useState(false)
  if (!annotations.prUrl && !annotations.issueUrl) return null
  return <div className="relative"><button type="button" onClick={() => setOpen((value) => !value)} className="inline-flex min-h-[44px] items-center justify-center rounded-md border border-gray-300 px-3 py-2 text-gray-600 transition-colors hover:bg-gray-50 hover:text-gray-900 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white sm:min-h-0 sm:px-3 sm:py-1.5" aria-label="関連リンク" title="関連リンク"><MoreHorizontal className="h-4 w-4" /></button>{open && <div className="absolute right-0 top-full z-20 mt-2 min-w-32 overflow-hidden rounded-md border border-gray-200 bg-white py-1 text-xs shadow-lg dark:border-gray-700 dark:bg-gray-900">{annotations.prUrl && <a href={annotations.prUrl} target="_blank" rel="noreferrer" onClick={() => setOpen(false)} className="flex items-center gap-2 px-3 py-2 text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"><GitPullRequest className="h-3.5 w-3.5 text-violet-500" />{urlNumber(annotations.prUrl) ? `PR #${urlNumber(annotations.prUrl)}` : 'PR'}</a>}{annotations.issueUrl && <a href={annotations.issueUrl} target="_blank" rel="noreferrer" onClick={() => setOpen(false)} className="flex items-center gap-2 px-3 py-2 text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"><CircleDot className="h-3.5 w-3.5 text-emerald-500" />{urlNumber(annotations.issueUrl) ? `Issue #${urlNumber(annotations.issueUrl)}` : 'Issue'}</a>}</div>}</div>
}

function DeleteButton({ props }: { props: SessionCardProps }) {
  if (!props.onDelete) return null
  return <button onClick={() => props.onDelete?.(props.session.session_id)} disabled={props.isDeleting} aria-label={`${props.isDeleting ? '削除中' : '削除'}: ${props.session.metadata?.description || `セッション ${props.session.session_id.substring(0, 8)}`}`} className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 border border-red-300 dark:border-red-600 hover:bg-red-50 dark:hover:bg-red-700 text-red-700 dark:text-red-300 text-sm font-medium rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed min-h-[44px] sm:min-h-0"><span className="sm:hidden">×</span><span className="hidden sm:inline">{props.isDeleting ? '削除中...' : '削除'}</span></button>
}

export function SessionCardActions(props: SessionCardProps & { annotations: Annotations }) {
  const href = `/sessions/${props.session.session_id}`
  return <div className="flex flex-row items-center space-x-2 sm:ml-4" onClick={(event) => event.stopPropagation()}>
    {props.session.status === 'active' && <Link href={href} className="inline-flex items-center justify-center px-3 py-2 sm:py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-md min-h-[44px] sm:min-h-0" prefetch><span>チャット</span></Link>}
    <Link href={href} className="inline-flex items-center justify-center px-3 py-2 sm:py-1.5 border border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300 text-sm font-medium rounded-md min-h-[44px] sm:min-h-0"><span>詳細</span></Link>
    <AnnotationMenu annotations={props.annotations} />
    <DeleteButton props={props} />
  </div>
}

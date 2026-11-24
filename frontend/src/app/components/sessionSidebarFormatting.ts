import { Session, SessionStatus } from '../../types/agentapi'

export function getStatusDotClass(status: SessionStatus): string {
  switch (status) {
    case 'active': return 'bg-green-500'
    case 'suspended': return 'bg-violet-500'
    case 'suspending': return 'bg-violet-400 animate-pulse'
    case 'resuming':
    case 'restoring': return 'bg-blue-400 animate-pulse'
    case 'running':
    case 'starting': return 'bg-yellow-400 animate-pulse'
    case 'creating': return 'bg-blue-400 animate-pulse'
    case 'unhealthy':
    case 'error':
    case 'timeout': return 'bg-red-500'
    default: return 'bg-gray-300 dark:bg-gray-600'
  }
}

export function isRunningStatus(status: SessionStatus): boolean {
  return status === 'active' || status === 'running' || status === 'starting' || status === 'creating'
}

export function getSessionAnnotations(session: Session) {
  const pick = (key: 'description' | 'running_task') => {
    const value = session.annotations?.[key]
    return typeof value === 'string' && value.trim() ? value.trim() : ''
  }
  return { description: pick('description'), runningTask: pick('running_task') }
}

export function getSessionTitle(session: Session): string {
  const annotations = getSessionAnnotations(session)
  if (annotations.description) return annotations.description
  if (session.tags?.description?.trim()) return session.tags.description.trim()
  const metadataDescription = session.metadata?.description
  if (typeof metadataDescription === 'string' && metadataDescription.trim()) return metadataDescription.trim()
  if (session.description?.trim()) return session.description.trim()
  return `#${session.session_id.substring(0, 8)}`
}

export function formatRelativeTime(date?: string): string {
  if (!date) return ''
  const minutes = Math.floor((Date.now() - new Date(date).getTime()) / 60000)
  if (minutes < 1) return 'たった今'
  if (minutes < 60) return `${minutes}分前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h前`
  return `${Math.floor(hours / 24)}d前`
}

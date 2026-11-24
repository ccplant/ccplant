import type { Session, AgentStatus } from '../../types/agentapi'
import type { ACPServerSession } from '../../lib/acp-server-client'

export function createAgentStatusFromSession(session: Pick<Session, 'status' | 'updated_at'>): AgentStatus {
  switch (session.status) {
    case 'running':
      return {
        status: 'running',
        last_activity: session.updated_at,
      }
    case 'active':
      return {
        status: 'stable',
        last_activity: session.updated_at,
      }
    default:
      return {
        status: 'error',
        last_activity: session.updated_at,
      }
  }
}

export function createAgentStatusMapFromSessions(sessionList: Session[]): { [sessionId: string]: AgentStatus } {
  return Object.fromEntries(sessionList.map((session) => [session.session_id, createAgentStatusFromSession(session)]))
}

export function mapACPSessionToSession(acpSession: ACPServerSession): Session {
  return {
    session_id: acpSession.sessionId,
    user_id: '',
    status: (acpSession._meta?.status as Session['status']) || 'active',
    started_at: acpSession.updatedAt || new Date().toISOString(),
    updated_at: acpSession.updatedAt,
    metadata: {
      description: acpSession.title || acpSession.cwd,
    },
    tags: acpSession._meta?.tags || {},
    environment: {},
    scope: (acpSession._meta?.scope as Session['scope']) || 'user',
    team_id: acpSession._meta?.teamId,
  }
}

export function getSessionAnnotations(session: Session) {
  const pick = (key: 'pr_url' | 'issue_url' | 'description' | 'running_task') => {
    const directValue = session.annotations?.[key]
    if (typeof directValue === 'string' && directValue.trim()) return directValue.trim()
    return ''
  }

  return {
    prUrl: pick('pr_url'),
    issueUrl: pick('issue_url'),
    description: pick('description'),
    runningTask: pick('running_task'),
  }
}

export function getURLNumber(url: string): string | null {
  const match = url.match(/\/(?:pull|issues)\/(\d+)(?:[/?#]|$)/)
  return match?.[1] ?? null
}

export interface CreatingSession {
  id: string
  message: string
  repository?: string
  status: 'creating' | 'waiting-agent' | 'sending-message' | 'completed' | 'failed'
  startTime: Date
}

export const getStatusText = (status: CreatingSession['status']) => {
  switch (status) {
    case 'creating':
      return 'セッション作成中...'
    case 'waiting-agent':
      return 'エージェント準備中...'
    case 'sending-message':
      return 'メッセージ送信中...'
    case 'completed':
      return '完了'
    case 'failed':
      return '失敗'
    default:
      return '処理中...'
  }
}

export const isNewSession = (startedAt: string) => {
  const started = new Date(startedAt)
  const now = new Date()
  const diffMs = now.getTime() - started.getTime()
  return diffMs < 2 * 60 * 1000 // 2分以内
}

export const isOldSession = (session: Session) => {
  const threeDaysAgo = new Date()
  threeDaysAgo.setDate(threeDaysAgo.getDate() - 3)

  const startedAt = new Date(session.started_at)
  const updatedAt = session.updated_at ? new Date(session.updated_at) : startedAt

  return startedAt < threeDaysAgo && updatedAt < threeDaysAgo
}

export const getSessionDescription = (session: Session) => {
  const annotations = getSessionAnnotations(session)
  if (annotations.description) {
    return annotations.description
  }

  const description = session.metadata?.description

  if (description && description !== 'No description available') {
    return description
  }

  // 新規セッションでメッセージがまだ取得できていない場合
  if (isNewSession(session.started_at)) {
    return '読み込み中...'
  }

  return 'No description available'
}

export const describeSessionStatus = (session: Session, sessionAgentStatus: Record<string, AgentStatus>) => {
  const agentStatus = sessionAgentStatus[session.session_id]
  const isNew = isNewSession(session.started_at)

  // セッションが作成中または起動中の場合は、そのステータスを優先表示
  if (session.status === 'creating') {
    return { status: 'creating' as const, colorClass: 'bg-blue-500 animate-pulse', text: '作成中' }
  }
  if (session.status === 'starting') {
    return { status: 'starting' as const, colorClass: 'bg-indigo-500 animate-pulse', text: '起動中' }
  }
  if (session.status === 'running') {
    return { status: 'running' as const, colorClass: 'bg-yellow-500 animate-pulse', text: 'Running' }
  }
  if (session.status === 'suspended') {
    return { status: 'suspended' as const, colorClass: 'bg-violet-500', text: 'Suspended' }
  }
  if (session.status === 'stopped') {
    return { status: 'stopped' as const, colorClass: 'bg-gray-400', text: 'Stopped' }
  }
  if (session.status === 'resuming' || session.status === 'restoring') {
    return { status: 'starting' as const, colorClass: 'bg-blue-500 animate-pulse', text: '再開中' }
  }
  if (session.status === 'suspending') {
    return { status: 'starting' as const, colorClass: 'bg-violet-500 animate-pulse', text: 'サスペンド中' }
  }
  if (session.status === 'error' || session.status === 'timeout') {
    return {
      status: 'error' as const,
      colorClass: 'bg-red-500',
      text: session.status === 'timeout' ? '起動タイムアウト' : '起動失敗',
    }
  }

  // ステータスが取得できていない場合
  if (!agentStatus) {
    if (isNew) {
      return { status: 'preparing' as const, colorClass: 'bg-blue-500 animate-pulse', text: '準備中' }
    }
    return { status: 'unknown' as const, colorClass: 'bg-gray-400', text: 'Unknown' }
  }

  // 新規セッションでerrorの場合は「準備中」として扱う
  if (agentStatus.status === 'error' && isNew) {
    return { status: 'preparing' as const, colorClass: 'bg-blue-500 animate-pulse', text: '準備中' }
  }

  switch (agentStatus.status) {
    case 'stable':
      return { status: 'stable' as const, colorClass: 'bg-green-500', text: 'Available' }
    case 'running':
      return { status: 'running' as const, colorClass: 'bg-yellow-500 animate-pulse', text: 'Running' }
    case 'error':
      return { status: 'error' as const, colorClass: 'bg-red-500', text: 'Error' }
    default:
      return { status: 'unknown' as const, colorClass: 'bg-gray-400', text: 'Unknown' }
  }
}

export const getStatusColor = (status: CreatingSession['status']) => {
  switch (status) {
    case 'creating':
      return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
    case 'waiting-agent':
      return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200'
    case 'sending-message':
      return 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200'
    case 'completed':
      return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
    case 'failed':
      return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
    default:
      return 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200'
  }
}

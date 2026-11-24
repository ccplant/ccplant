import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '../../../types/agentapi'
import {
  createAgentStatusMapFromSessions,
  describeSessionStatus,
  getSessionAnnotations,
  getSessionDescription,
  getURLNumber,
  isNewSession,
  isOldSession,
  mapACPSessionToSession,
} from '../sessionListPresentation'

const session = (changes: Partial<Session> = {}): Session => ({
  session_id: 'session-1',
  user_id: 'user-1',
  status: 'active',
  started_at: '2026-11-23T11:00:00Z',
  ...changes,
})

describe('session list presentation', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-23T12:00:00Z'))
  })
  afterEach(() => vi.useRealTimers())

  it('uses trimmed annotations before metadata descriptions', () => {
    const value = session({
      annotations: { description: '  current task  ', pr_url: '  https://example.com/pull/42 ' },
      metadata: { description: 'old task' },
    })
    expect(getSessionDescription(value)).toBe('current task')
    expect(getSessionAnnotations(value).prUrl).toBe('https://example.com/pull/42')
    expect(getURLNumber(getSessionAnnotations(value).prUrl)).toBe('42')
    expect(getURLNumber('https://example.com/issues/31?tab=comments')).toBe('31')
    expect(getURLNumber('https://example.com/31')).toBeNull()
  })

  it('keeps startup loading text within the two-minute boundary', () => {
    const fresh = session({ started_at: '2026-11-23T11:59:00Z' })
    expect(isNewSession(fresh.started_at)).toBe(true)
    expect(getSessionDescription(fresh)).toBe('読み込み中...')
    expect(isNewSession('2026-11-23T11:58:00Z')).toBe(false)
    expect(getSessionDescription(session())).toBe('No description available')
  })

  it('requires both start and latest activity to be old', () => {
    expect(isOldSession(session({ started_at: '2026-11-19T12:00:00Z' }))).toBe(true)
    expect(isOldSession(session({ started_at: '2026-11-19T12:00:00Z', updated_at: '2026-11-23T11:00:00Z' }))).toBe(
      false
    )
  })

  it('prioritizes lifecycle state over streamed agent state', () => {
    expect(describeSessionStatus(session({ status: 'suspended' }), { 'session-1': { status: 'running' } }).text).toBe(
      'Suspended'
    )
    expect(describeSessionStatus(session({ status: 'timeout' }), {}).text).toBe('起動タイムアウト')
    expect(describeSessionStatus(session(), { 'session-1': { status: 'stable' } }).text).toBe('Available')
  })

  it('treats a fresh agent error as preparation, but reports old errors', () => {
    const status = { 'session-1': { status: 'error' as const } }
    expect(describeSessionStatus(session({ started_at: '2026-11-23T11:59:00Z' }), status).status).toBe('preparing')
    expect(describeSessionStatus(session(), status).status).toBe('error')
    expect(describeSessionStatus(session(), {}).status).toBe('unknown')
  })

  it('maps ACP session metadata and initializes agent statuses', () => {
    const mapped = mapACPSessionToSession({
      sessionId: 'acp-1',
      cwd: '/repo',
      title: 'ACP task',
      updatedAt: '2026-11-23T11:00:00Z',
      _meta: { status: 'running', scope: 'team', teamId: 'team-1', tags: { repository: 'org/repo' } },
    })
    expect(mapped).toMatchObject({
      session_id: 'acp-1',
      status: 'running',
      scope: 'team',
      team_id: 'team-1',
      metadata: { description: 'ACP task' },
    })
    expect(createAgentStatusMapFromSessions([mapped])['acp-1'].status).toBe('running')
    expect(createAgentStatusMapFromSessions([session()])['session-1'].status).toBe('stable')
  })
})

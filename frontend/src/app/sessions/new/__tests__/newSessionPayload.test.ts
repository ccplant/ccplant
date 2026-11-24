import { describe, expect, it } from 'vitest'
import type { NewSessionDraft } from '../newSessionDraft'
import { buildNewACPSessionRequest, buildNewSessionParams, buildNewSessionRequest } from '../newSessionPayload'

export function sessionDraft(changes: Partial<NewSessionDraft> = {}): NewSessionDraft {
  return {
    initialMessage: ' hello ',
    freeFormRepository: '',
    checkoutTarget: '',
    checkoutBranch: '',
    checkoutPrNumber: '',
    selectedTeam: null,
    selectedAgentType: 'default',
    selectedManagerId: '',
    connectionId: '',
    cycleEnabled: false,
    cycleMessage: '',
    cycleMaxCount: 10,
    dockerEnabled: false,
    dockerRegistries: [],
    sessionTTL: '',
    sessionProfileId: '',
    ...changes,
  }
}

describe('new session payloads', () => {
  it('omits optional settings for a default user session', () => {
    expect(buildNewSessionRequest(sessionDraft())).toEqual({
      environment: {},
      metadata: { description: 'hello' },
      tags: undefined,
      params: { message: 'hello' },
      scope: 'user',
    })
  })
  it('preserves team, profile, checkout, pool, connection and trimmed TTL', () => {
    const request = buildNewSessionRequest(
      sessionDraft({
        freeFormRepository: ' org/repo ',
        checkoutTarget: 'pr',
        checkoutPrNumber: ' 42 ',
        selectedTeam: 'team-1',
        sessionProfileId: 'profile-1',
        selectedAgentType: 'codex-acp',
        selectedManagerId: 'fly-dev',
        connectionId: 'connection-1',
        sessionTTL: ' 2h ',
      })
    )
    expect(request).toMatchObject({
      scope: 'team',
      team_id: 'team-1',
      session_profile_id: 'profile-1',
      tags: { repository: 'org/repo', pr_number: '42' },
      environment: { REPOSITORY: 'org/repo' },
      params: { pool: 'fly-dev', agent_type: 'codex-acp', connection_id: 'connection-1', session_ttl: '2h' },
    })
  })
  it('keeps checkout branch and PR tags mutually exclusive', () => {
    const request = buildNewSessionRequest(
      sessionDraft({
        freeFormRepository: 'org/repo',
        checkoutTarget: 'branch',
        checkoutBranch: ' topic ',
        checkoutPrNumber: '42',
      })
    )
    expect(request.tags).toEqual({ repository: 'org/repo', branch: 'topic' })
  })
  it('prefers registry secrets over inline credentials and omits empty registries', () => {
    const params = buildNewSessionParams(
      sessionDraft({
        dockerEnabled: true,
        dockerRegistries: [
          {
            server: 'registry.example',
            username: 'user',
            password: 'private',
            secretName: 'registry-secret',
            insecure: true,
          },
          { server: '', username: '', password: 'not-a-registry', secretName: '', insecure: false },
        ],
      })
    )
    expect(params.docker).toEqual({
      enabled: true,
      registries: [{ server: 'registry.example', secret_name: 'registry-secret', insecure: true }],
    })
  })
  it('only enables cycles with a nonempty message and positive count', () => {
    expect(buildNewSessionParams(sessionDraft({ cycleEnabled: true, cycleMessage: '  ' }))).not.toHaveProperty(
      'cycle_message'
    )
    expect(
      buildNewSessionParams(sessionDraft({ cycleEnabled: true, cycleMessage: ' retry ', cycleMaxCount: 0 }))
    ).toEqual({ message: 'hello', cycle_message: 'retry' })
  })
  it('preserves ACP workspace and team tag behavior', () => {
    expect(buildNewACPSessionRequest(sessionDraft({ freeFormRepository: 'org/repo', selectedTeam: 'team-1' }))).toEqual(
      {
        cwd: '/home/user/workdir/repo',
        message: 'hello',
        agentType: undefined,
        pool: undefined,
        tags: { repository: 'org/repo', team: 'team-1' },
      }
    )
    expect(buildNewACPSessionRequest(sessionDraft()).cwd).toBe('/home/user')
  })
})

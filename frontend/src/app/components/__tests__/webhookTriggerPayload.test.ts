import { describe, expect, it } from 'vitest'
import { buildWebhookTrigger } from '../webhookTriggerPayload'
import { emptyTrigger } from '../webhookFormTypes'

describe('buildWebhookTrigger', () => {
  it('preserves explicit false runtime flags when saving a trigger', () => {
    expect(buildWebhookTrigger({ ...emptyTrigger, name: ' trigger ' }, 'github')).toEqual({
      id: undefined,
      name: 'trigger',
      enabled: true,
      conditions: { github: {} },
      session_config: { reuse_session: false, mount_payload: false, params: { oneshot: false } },
    })
  })

  it('saves GitHub matching rules and all configured session overrides', () => {
    const result = buildWebhookTrigger({
      ...emptyTrigger,
      id: 'trigger-1', name: ' PR ', enabled: false,
      events: ['pull_request'], actions: ['opened'], baseBranches: ['main'],
      goTemplate: ' {{ .valid }} ', initialMessageTemplate: ' review ', reuseMessageTemplate: ' update ',
      reuseSession: true, mountPayload: true, oneshot: true,
      sessionTTL: ' 1h ', model: ' sonnet ', credentialSource: 'triggered_user',
      sessionProfileId: ' profile-1 ', environment: { MODE: 'review' }, tags: { purpose: 'review' },
    }, 'github')
    expect(result).toEqual({
      id: 'trigger-1', name: 'PR', enabled: false,
      conditions: { github: { events: ['pull_request'], actions: ['opened'], base_branches: ['main'] }, go_template: '{{ .valid }}' },
      session_config: {
        initial_message_template: 'review', reuse_message_template: 'update', reuse_session: true, mount_payload: true,
        environment: { MODE: 'review' }, tags: { purpose: 'review' }, session_profile_id: 'profile-1',
        params: { oneshot: true, session_ttl: '1h', model: 'sonnet', credential_source: 'triggered_user' },
      },
    })
  })

  it('omits GitHub rules for custom webhooks while retaining the template condition', () => {
    const result = buildWebhookTrigger({ ...emptyTrigger, name: 'custom', events: ['push'], goTemplate: ' {{ .accepted }} ' }, 'custom')
    expect(result.conditions).toEqual({ go_template: '{{ .accepted }}' })
  })
})

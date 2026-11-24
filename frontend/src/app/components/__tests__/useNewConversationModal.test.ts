import { describe, expect, it } from 'vitest'

import { buildNewConversationSessionData } from '../useNewConversationModal'

describe('buildNewConversationSessionData', () => {
  it('normalizes form values and promotes filter metadata to tags', () => {
    expect(buildNewConversationSessionData(
      ' investigate failure ',
      ' owner/repository ',
      [{ key: 'NODE_ENV', value: 'test' }, { key: '', value: 'ignored' }],
      [{ key: 'priority', value: 'high' }, { key: 'ticket', value: '123' }],
    )).toEqual({
      environment: { NODE_ENV: 'test' },
      metadata: { description: 'investigate failure', ticket: '123' },
      tags: { repository: 'owner/repository', priority: 'high' },
    })
  })

  it('omits empty environment and tag collections', () => {
    expect(buildNewConversationSessionData('task', '', [{ key: '', value: '' }], [])).toEqual({
      environment: undefined,
      metadata: { description: 'task' },
      tags: undefined,
    })
  })
})

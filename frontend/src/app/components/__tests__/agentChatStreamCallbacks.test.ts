import type { SetStateAction } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { SessionMessage } from '../../../types/agentapi'
import { createAgentChatStreamCallbacks } from '../agentChatStreamCallbacks'

function createStream() {
  let messages: SessionMessage[] = []
  const context = {
    setMessages(update: SetStateAction<SessionMessage[]>) {
      messages = typeof update === 'function' ? update(messages) : update
    },
    applyACPConfigOptions: vi.fn(),
    setACPPendingPermission: vi.fn(),
    setPendingAction: vi.fn(),
    setShowQuestionModal: vi.fn(),
    setMessageSSEConnectionStatus: vi.fn(),
  }
  return { context, callbacks: createAgentChatStreamCallbacks(context), messages: () => messages }
}

describe('agent chat stream updates', () => {
  it('appends text and thoughts to the matching message only', () => {
    const stream = createStream()
    stream.callbacks.onMessage({ id: 1, role: 'agent', content: 'hello', time: 'now' })
    stream.callbacks.onMessage({ id: 2, role: 'agent', content: 'other', time: 'now' })
    stream.callbacks.onChunk(1, ' world')
    stream.callbacks.onThoughtChunk(1, 'thinking')
    expect(stream.messages().map(message => [message.content, message.thought])).toEqual([
      ['hello world', 'thinking'], ['other', undefined],
    ])
  })

  it('updates tool input without dropping existing tool metadata', () => {
    const stream = createStream()
    stream.callbacks.onMessage({ id: 1, role: 'agent', content: '{"name":"Read"}', time: 'now', toolUseId: 'tool' })
    stream.callbacks.onToolInputUpdate?.('tool', { path: '/file' }, 'Read file')
    expect(JSON.parse(stream.messages()[0].content)).toEqual({ name: 'Read', input: { path: '/file' }, title: 'Read file' })
  })

  it('keeps malformed tool messages intact and tracks reconnect status', () => {
    const stream = createStream()
    stream.callbacks.onMessage({ id: 1, role: 'agent', content: 'not json', time: 'now', toolUseId: 'tool' })
    stream.callbacks.onToolInputUpdate?.('tool', { path: '/file' })
    expect(stream.messages()[0].content).toBe('not json')
    stream.callbacks.onConnectionOpen?.()
    stream.callbacks.onConnectionClosed?.()
    expect(stream.context.setMessageSSEConnectionStatus.mock.calls).toEqual([['connected'], ['connecting']])
  })
})

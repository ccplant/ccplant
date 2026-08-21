import { describe, expect, it } from 'vitest'
import {
  mergeRefreshedMessageHistory,
  replaceRebuiltMessageHistory,
} from '../messageHistory'

type Message = { id: string | number; content: string }

describe('mergeRefreshedMessageHistory', () => {
  it('keeps older messages outside a refreshed tail window', () => {
    const current: Message[] = [
      { id: 1, content: 'older' },
      { id: 2, content: 'old value' },
    ]
    const refreshed: Message[] = [
      { id: 2, content: 'updated value' },
      { id: 3, content: 'new' },
    ]

    expect(mergeRefreshedMessageHistory(current, refreshed)).toEqual([
      { id: 1, content: 'older' },
      { id: 2, content: 'updated value' },
      { id: 3, content: 'new' },
    ])
  })

  it('does not erase visible history for a transient empty refresh', () => {
    const current: Message[] = [{ id: 1, content: 'visible' }]

    expect(mergeRefreshedMessageHistory(current, [])).toBe(current)
  })

  it('distinguishes numeric and string message IDs', () => {
    const current: Message[] = [{ id: 1, content: 'numeric' }]
    const refreshed: Message[] = [{ id: '1', content: 'string' }]

    expect(mergeRefreshedMessageHistory(current, refreshed)).toEqual([
      { id: 1, content: 'numeric' },
      { id: '1', content: 'string' },
    ])
  })
})

describe('replaceRebuiltMessageHistory', () => {
  it('removes live copies when rebuilt ACP history uses different local IDs', () => {
    const current: Message[] = [
      { id: 1, content: 'previous agent output' },
      { id: 2, content: 'latest user prompt' },
    ]
    const rebuilt: Message[] = [
      { id: -100001, content: 'previous agent output' },
      { id: 1, content: 'latest user prompt' },
    ]

    expect(replaceRebuiltMessageHistory(current, rebuilt)).toEqual(rebuilt)
  })

  it('keeps visible history when a complete rebuild is transiently empty', () => {
    const current: Message[] = [{ id: 1, content: 'visible' }]

    expect(replaceRebuiltMessageHistory(current, [])).toBe(current)
  })
})

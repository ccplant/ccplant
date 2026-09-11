import { beforeEach, describe, expect, it } from 'vitest'
import { Window } from 'happy-dom'

import { getEnterKeyBehavior, loadFullGlobalSettings, setEnterKeyBehavior } from '../settings'

const testWindow = new Window()
Object.defineProperty(globalThis, 'window', { value: testWindow, configurable: true })
Object.defineProperty(globalThis, 'localStorage', { value: testWindow.localStorage, configurable: true })

describe('Enter key behavior settings', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('defaults to newline when no settings have been saved', () => {
    expect(getEnterKeyBehavior()).toBe('newline')
  })

  it('defaults to newline for existing settings without an Enter key preference', () => {
    localStorage.setItem('agentapi-full-global-settings', JSON.stringify({
      agentApiProxy: {},
      mcpServers: [],
      repositoryHistory: [],
      messageTemplates: [],
      created_at: '2026-01-01T00:00:00.000Z',
      updated_at: '2026-01-01T00:00:00.000Z',
    }))

    expect(getEnterKeyBehavior()).toBe('newline')
  })

  it('preserves an explicitly selected send preference', () => {
    setEnterKeyBehavior('send')

    expect(getEnterKeyBehavior()).toBe('send')
  })

  it('migrates the removed /api/proxy endpoint to /api/v1', () => {
    localStorage.setItem('agentapi-full-global-settings', JSON.stringify({
      agentApiProxy: {
        endpoint: 'https://app.example.com/api/proxy',
        enabled: true,
        timeout: 30000,
        apiKey: '',
      },
    }))

    expect(loadFullGlobalSettings().agentApiProxy.endpoint).toBe('https://app.example.com/api/v1')
  })
})

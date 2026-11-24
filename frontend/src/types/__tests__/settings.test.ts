import { beforeEach, describe, expect, it } from 'vitest'
import { Window } from 'happy-dom'

import { getEnterKeyBehavior, loadFullGlobalSettings, prepareSettingsForSave, setEnterKeyBehavior } from '../settings'

describe('settings API payload preparation', () => {
  it('normalizes nested values and omits empty secrets', () => {
    expect(prepareSettingsForSave({
      bedrock: {
        enabled: false,
        model: ' model-id ',
        access_key_id: ' ',
      },
      mcp_servers: {
        ' server ': {
          type: 'http',
          url: ' https://example.test/mcp ',
          headers: { ' Authorization ': ' Bearer token ', empty: ' ' },
          env: { ' TOKEN ': ' value ', empty: '' },
        },
        ' ': { type: 'stdio', command: 'ignored' },
      },
    })).toEqual({
      bedrock: { enabled: false, model: 'model-id' },
      mcp_servers: {
        server: {
          type: 'http',
          url: 'https://example.test/mcp',
          headers: { Authorization: 'Bearer token' },
          env: { TOKEN: 'value' },
        },
      },
    })
  })

  it('preserves empty environment values as deletion instructions', () => {
    expect(prepareSettingsForSave({
      env_vars: { ' KEEP ': ' value ', ' REMOVE ': '', ' ': 'ignored' },
    })).toEqual({
      env_vars: { KEEP: 'value', REMOVE: '' },
    })
  })

  it('preserves explicit empty collections and removes response-only fields', () => {
    expect(prepareSettingsForSave({
      marketplaces: {},
      enabled_plugins: [],
      skills: [' owner/skill ', 'owner/skill', ''],
      codex_connection: { mode: 'oauth', has_api_key: true },
      external_session_managers: [{
        id: 'manager-1',
        name: 'Manager',
        labels: { region: 'local' },
        has_connection_token: true,
        version: '1.0.0',
      }],
    })).toEqual({
      marketplaces: {},
      enabled_plugins: [],
      skills: ['owner/skill'],
      codex_connection: { mode: 'oauth' },
      external_session_managers: [{
        id: 'manager-1',
        instance_id: undefined,
        name: 'Manager',
        labels: { region: 'local' },
      }],
    })
  })
})

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
})

describe('AgentAPI proxy endpoint migration', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('migrates a saved legacy proxy route to the public API route', () => {
    localStorage.setItem('agentapi-full-global-settings', JSON.stringify({
      agentApiProxy: {
        endpoint: 'https://myinstance.com/api/proxy',
        enabled: true,
        timeout: 30000,
        apiKey: '',
      },
    }))

    expect(loadFullGlobalSettings().agentApiProxy.endpoint).toBe('https://myinstance.com/api/v1')
  })
})

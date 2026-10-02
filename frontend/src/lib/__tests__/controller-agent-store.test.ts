import { beforeEach, describe, expect, it } from 'vitest'
import {
  buildControllerMessage,
  CONTROLLER_AGENTS_STORAGE_KEY,
  createControllerAgent,
  loadControllerAgents,
  saveControllerAgents,
} from '../controller-agent-store'

describe('controller agent PoC store', () => {
  beforeEach(() => localStorage.clear())

  it('persists an agent in local storage', () => {
    const agent = createControllerAgent({
      name: 'Backend Lead',
      description: 'Coordinates backend work',
      instructions: 'Delegate independent work.',
      scope: 'user',
    })

    saveControllerAgents([agent])

    expect(localStorage.getItem(CONTROLLER_AGENTS_STORAGE_KEY)).toContain('Backend Lead')
    expect(loadControllerAgents()).toEqual([agent])
  })

  it('adds identity and instructions only to the first command', () => {
    const agent = createControllerAgent({
      name: 'Lead',
      description: 'Coordinates work',
      instructions: 'Report progress.',
      scope: 'user',
    })

    expect(buildControllerMessage(agent, 'Fix issue 42', true)).toContain('常設指示:\nReport progress.')
    expect(buildControllerMessage(agent, 'Continue', false)).toBe('Continue')
  })
})

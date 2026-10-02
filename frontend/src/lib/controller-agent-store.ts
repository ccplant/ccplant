import type { ControllerAgent, CreateControllerAgentInput } from '../types/controller_agent'

export const CONTROLLER_AGENTS_STORAGE_KEY = 'ccplant_controller_agents_poc_v1'

const createId = () => {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export function loadControllerAgents(): ControllerAgent[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = window.localStorage.getItem(CONTROLLER_AGENTS_STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export function saveControllerAgents(agents: ControllerAgent[]): void {
  window.localStorage.setItem(CONTROLLER_AGENTS_STORAGE_KEY, JSON.stringify(agents))
}

export function createControllerAgent(input: CreateControllerAgentInput): ControllerAgent {
  const now = new Date().toISOString()
  return {
    ...input,
    id: createId(),
    status: 'idle',
    runs: [],
    created_at: now,
    updated_at: now,
  }
}

export function replaceControllerAgent(agents: ControllerAgent[], updated: ControllerAgent): ControllerAgent[] {
  return agents.map((agent) => agent.id === updated.id ? updated : agent)
}

export function buildControllerMessage(agent: ControllerAgent, command: string, firstRun: boolean): string {
  if (!firstRun) return command.trim()
  return [
    `あなたは「${agent.name}」という司令塔Agentです。`,
    agent.description ? `役割: ${agent.description}` : '',
    `常設指示:\n${agent.instructions.trim()}`,
    `今回の指令:\n${command.trim()}`,
  ].filter(Boolean).join('\n\n')
}

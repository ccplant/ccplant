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
    return Array.isArray(parsed)
      ? parsed.map((agent) => ({ max_child_sessions: 4, ...agent }))
      : []
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
    max_child_sessions: input.max_child_sessions || 4,
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

export function buildControllerBootstrapMessage(agent: ControllerAgent): string {
  return [
    `あなたは「${agent.name}」という常駐の司令塔Agentです。`,
    agent.description ? `役割: ${agent.description}` : '',
    `常設指示:\n${agent.instructions.trim()}`,
    'ccplant_sessions MCPのcreate_session、send_message、get_session_status、get_messages、delete_sessionを使えます。',
    `必要に応じて最大${agent.max_child_sessions}個までWorker Sessionを作り、仕事を並列化してください。`,
    'Workerを作るときはcreate_sessionのmessageに具体的な担当作業を渡してください。完了を監視し、成果を回収して統合してください。',
    'いまは初期化だけを行い、ユーザーから指令が来るまで待機してください。',
  ].filter(Boolean).join('\n\n')
}

import type { ResourceScope } from './agentapi'

export type ControllerAgentStatus = 'idle' | 'starting' | 'working' | 'error'

export interface ControllerAgentRun {
  id: string
  command: string
  status: 'running' | 'submitted' | 'failed'
  created_at: string
  error?: string
}

export interface ControllerAgent {
  id: string
  name: string
  description: string
  instructions: string
  session_profile_id?: string
  controller_session_id?: string
  scope: ResourceScope
  team_id?: string
  status: ControllerAgentStatus
  runs: ControllerAgentRun[]
  created_at: string
  updated_at: string
}

export interface CreateControllerAgentInput {
  name: string
  description: string
  instructions: string
  session_profile_id?: string
  scope: ResourceScope
  team_id?: string
}

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
  /** Generated profile that equips the controller session with orchestration tools. */
  session_profile_id?: string
  /** Optional user-selected profile inherited by the generated controller profile. */
  source_session_profile_id?: string
  controller_session_id?: string
  max_child_sessions: number
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
  source_session_profile_id?: string
  max_child_sessions?: number
  scope: ResourceScope
  team_id?: string
}

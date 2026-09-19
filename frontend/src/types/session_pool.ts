export interface ClusterSessionManager {
  id: string
  name: string
  enabled: boolean
  draining?: boolean
  last_heartbeat_at?: string
}

export interface LogicalSessionPool {
  name: string
  labels?: Record<string, string>
  enabled: boolean
}

export interface SessionPoolSupplier {
  pool: string
  manager_id: string
  labels?: Record<string, string>
  min_idle?: number
  max_runners?: number
  enabled: boolean
  draining?: boolean
  idle_runners?: number
  total_runners?: number
}

export interface SessionPoolBinding {
  id: string
  pool: string
  subject_type: 'user' | 'team' | 'all'
  subject_id: string
  role: 'use' | 'manage' | 'manage_and_use'
  enabled: boolean
  priority?: number
  max_concurrent?: number
}

export interface SessionPoolManagerRuntimeStatus {
  status?: string
  version?: string
  uptime_seconds?: number
  active_sessions?: number
  running_runners?: number
  used_runners?: number
  running_runner_ids?: string[]
  used_runner_ids?: string[]
  capabilities?: string[]
}

export interface SessionPoolManagerStatus {
  manager: ClusterSessionManager
  pools: string[]
  online: boolean
  status?: SessionPoolManagerRuntimeStatus
  error?: string
}

export interface SessionPoolStatusResponse {
  session_pools: LogicalSessionPool[]
  session_managers: SessionPoolManagerStatus[]
}

export interface SessionPoolLogs {
  lines: string[]
  source?: string
}

export interface AdminSessionRunner {
  id: string
  manager_id: string
  manager_name?: string
  pool?: string
  from_pool: boolean
  status: 'idle' | 'claiming' | 'running' | 'offline' | 'draining'
  session_id?: string
  online: boolean
  created_at?: string
  updated_at?: string
  last_seen?: string
}

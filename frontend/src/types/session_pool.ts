export interface ClusterSessionManager {
  id: string
  name: string
  enabled: boolean
  draining?: boolean
  last_heartbeat_at?: string
}

export interface SessionPool {
  name: string
  manager_id: string
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
  subject_type: 'user' | 'team'
  subject_id: string
  enabled: boolean
}

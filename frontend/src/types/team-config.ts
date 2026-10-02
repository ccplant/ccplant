export interface TeamConfig {
  team_id: string
  principal_id: string
  name: string
  github_teams: string[]
}

export interface TeamMember {
  principal_id: string
  login: string
  sources: Array<{ connection_id: string; github_user_id: number }>
}

export interface TeamMembershipState {
  members: TeamMember[]
  unlinked_external_member_count: number
  sync: {
    status: 'never' | 'running' | 'succeeded' | 'failed'
    synced_at?: string
    synced_by?: string
    reason?: 'manual' | 'identity_created' | 'identity_linked'
    next_sync_at?: string
    operation_id?: string
  }
}

export interface TeamMembershipSyncResult {
  operation_id: string
  synced_at: string
  next_sync_at: string
  member_count: number
  unlinked_external_member_count: number
  added_count: number
  removed_count: number
}

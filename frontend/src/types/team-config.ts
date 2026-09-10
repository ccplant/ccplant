export interface ExternalTeamBinding {
  connection_id: string
  organization: string
  team_slug: string
  managed_by?: 'discovery' | 'api'
}

export interface TeamConfig {
  team_id: string
  principal_id: string
  external_teams: ExternalTeamBinding[]
}

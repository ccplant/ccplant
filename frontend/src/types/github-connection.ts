export type GitHubSecretSource = 'encrypted' | 'environment'

export interface GitHubConnection {
  id: string
  name: string
  base_url: string
  api_url?: string
  oauth_client_id?: string
  oauth_scope?: string
  secret_source?: GitHubSecretSource
  secret_environment?: string
  secret_configured?: boolean
  callback_url?: string
  linked_identities?: number
  enabled?: boolean
  created_at?: string
  updated_at?: string
}

export interface GitHubConnectionInput {
  name: string
  base_url: string
  api_url: string
  oauth_client_id: string
  oauth_scope: string
  enabled?: boolean
  oauth_client_secret?: {
    source: GitHubSecretSource
    value?: string
    environment?: string
  }
}

export interface GitHubIdentity {
  id: string
  principal_id: string
  connection_id: string
  connection_name: string
  base_url: string
  github_user_id: number
  login: string
  avatar_url?: string
  created_at: string
}

export interface GitHubIdentitiesResponse {
  principal_id: string | null
  identities: GitHubIdentity[]
}

export type GoogleSecretSource = 'encrypted' | 'environment'

export interface GoogleConnection {
  id: string
  name: string
  oauth_client_id?: string
  secret_source?: GoogleSecretSource
  secret_environment?: string
  secret_configured?: boolean
  callback_url?: string
  linked_identities?: number
  enabled?: boolean
  allow_login?: boolean
  show_on_login?: boolean
  allow_user_creation?: boolean
  hosted_domains?: string[]
  email_domains?: string[]
  created_at?: string
  updated_at?: string
}

export interface GoogleConnectionInput {
  name: string
  oauth_client_id: string
  enabled?: boolean
  allow_login?: boolean
  show_on_login?: boolean
  allow_user_creation?: boolean
  hosted_domains?: string[]
  email_domains?: string[]
  oauth_client_secret?: {
    source: GoogleSecretSource
    value?: string
    environment?: string
  }
}

export interface GoogleIdentity {
  id: string
  principal_id: string
  connection_id: string
  connection_name: string
  subject: string
  email: string
  email_verified: boolean
  name?: string
  avatar_url?: string
  hosted_domain?: string
  created_at: string
}

export interface GoogleIdentitiesResponse {
  principal_id: string | null
  identities: GoogleIdentity[]
}

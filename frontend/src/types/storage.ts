import type { ResourceScope } from './agentapi'

export interface StorageAttachment {
  storage_id: string
  mount_path: string
  read_only?: boolean
}

export interface Storage {
  id: string
  name: string
  description?: string
  scope: ResourceScope
  team_id?: string
  kind: 'object_snapshot'
  state: string
  revision: number
  file_count: number
  updated_at: string
}

export interface CreateStorageRequest {
  name: string
  description?: string
  scope: ResourceScope
  team_id?: string
  kind: 'object_snapshot'
}

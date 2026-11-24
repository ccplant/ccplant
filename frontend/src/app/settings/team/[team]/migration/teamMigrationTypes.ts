import { TransferableResourceType } from '@/lib/agentapi-proxy-client'
import type { SandboxPolicy } from '@/types/sandbox_policy'
import type { Schedule } from '@/types/schedule'
import type { SessionProfile } from '@/types/session_profile'
import type { SlackBot } from '@/types/slackbot'
import type { Webhook } from '@/types/webhook'

export type MigrationResourceType = TransferableResourceType | 'schedule'
export type MigratablePayload = Webhook | SlackBot | Schedule | SessionProfile | SandboxPolicy
export type MigrationResource = { id: string; name: string; description?: string; type: MigrationResourceType; payload?: MigratablePayload }
export type MigrationSourceMode = 'current' | 'external'

export interface ExternalSourceResponse {
  webhooks: Webhook[] | { webhooks?: Webhook[] }
  slackbots: SlackBot[] | { slackbots?: SlackBot[] }
  schedules: Schedule[] | { schedules?: Schedule[] }
  profiles: SessionProfile[] | { session_profiles?: SessionProfile[] }
  policies: SandboxPolicy[] | { sandbox_policies?: SandboxPolicy[] }
}

export const migrationGroups: { type: MigrationResourceType; label: string }[] = [
  { type: 'session_profile', label: 'セッションプロファイル' },
  { type: 'schedule', label: 'スケジュール' },
  { type: 'sandbox_policy', label: 'サンドボックスポリシー' },
  { type: 'webhook', label: 'Webhook' },
  { type: 'slackbot', label: 'SlackBot' },
]

export function migrationKey(resource: Pick<MigrationResource, 'type' | 'id'>) {
  return `${resource.type}:${resource.id}`
}

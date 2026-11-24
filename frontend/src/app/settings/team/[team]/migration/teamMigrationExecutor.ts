import { AgentAPIProxyClient, AgentAPIProxyError, createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { SandboxPolicy } from '@/types/sandbox_policy'
import type { Schedule } from '@/types/schedule'
import type { SessionProfile } from '@/types/session_profile'
import type { SlackBot } from '@/types/slackbot'
import type { Webhook } from '@/types/webhook'
import { MigrationResource, MigrationSourceMode } from './teamMigrationTypes'

interface MigrationInput {
  chosen: MigrationResource[]; sourceMode: MigrationSourceMode; scopeId: string
  setMigrating: (value: boolean) => void; setCompleted: React.Dispatch<React.SetStateAction<number>>
  setError: (value: string | null) => void; setStep: (value: 1 | 2 | 3) => void
}

export async function executeTeamMigration(input: MigrationInput) {
  input.setMigrating(true); input.setCompleted(0); input.setError(null)
  try {
    const context: MigrationContext = { client: createAgentAPIProxyClientFromStorage(), sourceMode: input.sourceMode, scopeId: input.scopeId, idMap: new Map() }
    for (const resource of input.chosen) {
      await migrateResource(context, resource)
      input.setCompleted((value) => value + 1)
    }
    input.setStep(3)
  } catch (reason) {
    input.setError(reason instanceof AgentAPIProxyError ? reason.message : '移行中にエラーが発生しました。完了済みのリソースは移行先に残っています。')
  } finally { input.setMigrating(false) }
}

interface MigrationContext { client: AgentAPIProxyClient; sourceMode: MigrationSourceMode; scopeId: string; idMap: Map<string, string> }

async function migrateResource(context: MigrationContext, resource: MigrationResource) {
  if (context.sourceMode === 'current' && resource.type !== 'schedule') {
    const request = { resource_type: resource.type, resource_id: resource.id, target_scope: 'team' as const, target_team_id: context.scopeId }
    await context.client.transferResource({ ...request, dry_run: true })
    await context.client.transferResource(request)
    return
  }
  if (!resource.payload) return
  const handlers: Partial<Record<MigrationResource['type'], () => Promise<string>>> = {
    sandbox_policy: () => migratePolicy(context, resource.payload as SandboxPolicy),
    session_profile: () => migrateProfile(context, resource.payload as SessionProfile),
    schedule: () => migrateSchedule(context, resource.payload as Schedule),
    webhook: () => migrateWebhook(context, resource.payload as Webhook),
    slackbot: () => migrateSlackBot(context, resource.payload as SlackBot),
  }
  const id = await handlers[resource.type]?.()
  if (id) context.idMap.set(resource.id, id)
}

async function migratePolicy(context: MigrationContext, value: SandboxPolicy) {
  const created = await context.client.createSandboxPolicy({
    name: value.name, description: value.description, allowed_domains: value.allowed_domains,
    denied_domains: value.denied_domains, count_mode: value.count_mode, scope: 'team', team_id: context.scopeId,
  })
  return created.id
}

async function migrateProfile(context: MigrationContext, value: SessionProfile) {
  const config = value.config ? structuredClone(value.config) : undefined
  if (config?.sandbox_policy_id) config.sandbox_policy_id = context.idMap.get(config.sandbox_policy_id)
  if (config?.source_session_profile_id) config.source_session_profile_id = context.idMap.get(config.source_session_profile_id)
  if (config?.settings_team_id) config.settings_team_id = context.scopeId
  const created = await context.client.createSessionProfile({
    name: value.name, description: value.description, is_default: value.is_default,
    selector_tags: value.selector_tags, config, scope: 'team', team_id: context.scopeId,
  })
  return created.id
}

async function migrateSchedule(context: MigrationContext, value: Schedule) {
  const sessionConfig = mappedSessionConfig(context, value.session_config)
  const created = await context.client.createSchedule({
    name: value.name, scheduled_at: value.scheduled_at, cron_expr: value.cron_expr,
    timezone: value.timezone, session_config: sessionConfig, scope: 'team', team_id: context.scopeId,
  })
  if (value.status !== 'active') await context.client.updateSchedule(created.id, { status: value.status })
  return created.id
}

async function migrateWebhook(context: MigrationContext, value: Webhook) {
  const triggers = structuredClone(value.triggers || [])
  for (const trigger of triggers) {
    const profileId = trigger.session_config?.session_profile_id
    if (profileId) trigger.session_config!.session_profile_id = context.idMap.get(profileId)
  }
  const created = await context.client.createWebhook({
    name: value.name, type: value.type, github: value.github, signature_header: value.signature_header,
    signature_type: value.signature_type, signature_prefix: value.signature_prefix, triggers,
    session_config: mappedSessionConfig(context, value.session_config), scope: 'team', team_id: context.scopeId,
  })
  return created.id
}

async function migrateSlackBot(context: MigrationContext, value: SlackBot) {
  const created = await context.client.createSlackBot({
    name: value.name, allowed_event_types: value.allowed_event_types, allowed_channel_names: value.allowed_channel_names,
    allowed_user_ids: value.allowed_user_ids, session_config: mappedSessionConfig(context, value.session_config),
    max_sessions: value.max_sessions, notify_on_session_created: value.notify_on_session_created,
    allow_bot_messages: value.allow_bot_messages, scope: 'team', team_id: context.scopeId,
  })
  return created.id
}

function mappedSessionConfig<T extends { session_profile_id?: string }>(context: MigrationContext, value: T | undefined): T | undefined {
  if (!value) return undefined
  const config = structuredClone(value)
  if (context.sourceMode === 'external' && config.session_profile_id) config.session_profile_id = context.idMap.get(config.session_profile_id)
  return config
}

import type { WebhookTrigger, WebhookSessionConfig, WebhookType } from '../../types/webhook'
import type { TriggerFormData } from './webhookFormTypes'

function buildTriggerConditions(t: TriggerFormData, webhookType: WebhookType): WebhookTrigger['conditions'] {
  const conditions: WebhookTrigger['conditions'] = {}

  // GitHub conditions
  if (webhookType === 'github') {
    conditions.github = {}
    if (t.events.length > 0) {
      conditions.github.events = t.events
    }
    if (t.actions.length > 0) {
      conditions.github.actions = t.actions
    }
    if (t.baseBranches.length > 0) {
      conditions.github.base_branches = t.baseBranches
    }
    // Go template for GitHub webhooks
    if (t.goTemplate?.trim()) {
      conditions.go_template = t.goTemplate.trim()
    }
  }

  // Custom webhook conditions
  if (webhookType === 'custom') {
    if (t.goTemplate?.trim()) {
      conditions.go_template = t.goTemplate.trim()
    }
  }

  return conditions
}

function buildTriggerSessionConfig(t: TriggerFormData): WebhookSessionConfig {
  // Build session_config
  const session_config: WebhookSessionConfig = {}
  if (t.initialMessageTemplate.trim()) {
    session_config.initial_message_template = t.initialMessageTemplate.trim()
  }
  if (t.reuseMessageTemplate?.trim()) {
    session_config.reuse_message_template = t.reuseMessageTemplate.trim()
  }
  // Always set reuse_session explicitly (true or false)
  session_config.reuse_session = t.reuseSession
  // Always set mount_payload explicitly (true or false)
  session_config.mount_payload = t.mountPayload

  // Add environment variables if present
  if (Object.keys(t.environment).length > 0) {
    session_config.environment = t.environment
  }

  // Add tags if present
  if (Object.keys(t.tags).length > 0) {
    session_config.tags = t.tags
  }

  // Add session profile if present
  if (t.sessionProfileId.trim()) {
    session_config.session_profile_id = t.sessionProfileId.trim()
  }

  // Build params
  if (!session_config.params) {
    session_config.params = {}
  }
  // Always set oneshot explicitly (true or false)
  session_config.params.oneshot = t.oneshot
  if (t.sessionTTL.trim()) {
    session_config.params.session_ttl = t.sessionTTL.trim()
  }
  if (t.model.trim()) {
    session_config.params.model = t.model.trim()
  }
  if (t.credentialSource) {
    session_config.params.credential_source = t.credentialSource
  }

  return session_config
}

export function buildWebhookTrigger(t: TriggerFormData, webhookType: WebhookType): WebhookTrigger {
  return {
    id: t.id,
    name: t.name.trim(),
    enabled: t.enabled,
    conditions: buildTriggerConditions(t, webhookType),
    session_config: buildTriggerSessionConfig(t),
  }
}

import { ResourceScope } from '../../types/agentapi'
import { AgentAPIProxyError, createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { CreateWebhookRequest, Webhook, WebhookSignatureType, WebhookType } from '../../types/webhook'

import { buildWebhookTrigger } from './webhookTriggerPayload'
import { TriggerFormData } from './webhookFormTypes'

export interface WebhookSubmitInput {
  allowedEvents: string[]
  allowedRepositories: string
  editingWebhook?: Webhook | null
  getScopeParams: () => { scope: ResourceScope; team_id?: string }
  name: string
  secret: string
  signatureHeader: string
  signaturePrefix: string
  signatureType: WebhookSignatureType
  triggers: TriggerFormData[]
  webhookType: WebhookType
}

export function validateWebhookSubmit(input: WebhookSubmitInput): string | null {
  if (!input.name.trim()) return 'Webhook名を入力してください'
  if (!input.triggers.some((trigger) => trigger.name.trim())) {
    return '少なくとも1つのトリガーを設定してください'
  }
  return null
}

function buildWebhookRequest(input: WebhookSubmitInput): CreateWebhookRequest {
  const repositories = input.allowedRepositories
    .split(',')
    .map((repository) => repository.trim())
    .filter(Boolean)
  const validTriggers = input.triggers.filter((trigger) => trigger.name.trim())

  return {
    name: input.name.trim(),
    type: input.webhookType,
    secret: input.secret.trim() || undefined,
    signature_header: input.signatureHeader.trim() || undefined,
    signature_type: input.signatureType,
    signature_prefix: input.signaturePrefix.trim() || undefined,
    github: input.webhookType === 'github' ? {
      allowed_events: input.allowedEvents.length ? input.allowedEvents : undefined,
      allowed_repositories: repositories.length ? repositories : undefined,
    } : undefined,
    triggers: validTriggers.map((trigger) => buildWebhookTrigger(trigger, input.webhookType)),
    ...input.getScopeParams(),
  }
}

export async function saveWebhook(input: WebhookSubmitInput): Promise<void> {
  const client = createAgentAPIProxyClientFromStorage()
  const request = buildWebhookRequest(input)
  if (input.editingWebhook) {
    await client.updateWebhook(input.editingWebhook.id, request)
    return
  }
  await client.createWebhook(request)
}

export function webhookSubmitError(error: unknown): string {
  if (error instanceof AgentAPIProxyError) {
    if (error.status === 400) return `入力エラー: ${error.message}`
    if (error.status === 403) return `権限エラー: ${error.message}`
    if (error.status === 500) return `サーバーエラー: ${error.message}`
    return error.message
  }
  if (error instanceof Error) return error.message
  return 'Webhookの保存に失敗しました'
}

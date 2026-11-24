'use client'

import { Dispatch, SetStateAction, useEffect } from 'react'

import { Webhook, WebhookSignatureType, WebhookType } from '../../types/webhook'
import { emptyTrigger, TriggerFormData } from './webhookFormTypes'

interface WebhookFormInitializationInput {
  editingWebhook?: Webhook | null
  isOpen: boolean
  resetForm: () => void
  setAllowedEvents: Dispatch<SetStateAction<string[]>>
  setAllowedRepositories: Dispatch<SetStateAction<string>>
  setName: Dispatch<SetStateAction<string>>
  setSecret: Dispatch<SetStateAction<string>>
  setSignatureHeader: Dispatch<SetStateAction<string>>
  setSignaturePrefix: Dispatch<SetStateAction<string>>
  setSignatureType: Dispatch<SetStateAction<WebhookSignatureType>>
  setTriggers: Dispatch<SetStateAction<TriggerFormData[]>>
  setWebhookType: Dispatch<SetStateAction<WebhookType>>
}

function webhookTriggers(webhook: Webhook): TriggerFormData[] {
  if (!webhook.triggers?.length) return [{ ...emptyTrigger }]
  return webhook.triggers.map((trigger) => ({
    id: trigger.id,
    name: trigger.name,
    enabled: trigger.enabled ?? true,
    events: trigger.conditions.github?.events || [],
    actions: trigger.conditions.github?.actions || [],
    baseBranches: trigger.conditions.github?.base_branches || [],
    initialMessageTemplate: trigger.session_config?.initial_message_template || '',
    reuseMessageTemplate: trigger.session_config?.reuse_message_template || '',
    reuseSession: trigger.session_config?.reuse_session ?? false,
    mountPayload: trigger.session_config?.mount_payload ?? false,
    oneshot: trigger.session_config?.params?.oneshot ?? false,
    sessionTTL: trigger.session_config?.params?.session_ttl || '',
    model: trigger.session_config?.params?.model || '',
    credentialSource: trigger.session_config?.params?.credential_source || '',
    goTemplate: trigger.conditions.go_template || '',
    environment: trigger.session_config?.environment || {},
    tags: trigger.session_config?.tags || {},
    showAdvanced: false,
    sessionProfileId: trigger.session_config?.session_profile_id || '',
  }))
}

export function useWebhookFormInitialization(input: WebhookFormInitializationInput) {
  useEffect(() => {
    const webhook = input.editingWebhook
    if (!webhook) return input.resetForm()
    input.setName(webhook.name)
    input.setWebhookType(webhook.type)
    input.setSecret('')
    input.setSignatureHeader(webhook.signature_header || 'X-Signature')
    input.setSignatureType(webhook.signature_type || 'hmac')
    input.setSignaturePrefix(webhook.signature_prefix || '')
    input.setAllowedEvents(webhook.github?.allowed_events || [])
    input.setAllowedRepositories(webhook.github?.allowed_repositories?.join(', ') || '')
    input.setTriggers(webhookTriggers(webhook))
  }, [input.editingWebhook, input.isOpen, input.resetForm]) // eslint-disable-line react-hooks/exhaustive-deps
}

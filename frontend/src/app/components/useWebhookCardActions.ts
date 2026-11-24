'use client'

import { useState } from 'react'
import { Webhook } from '../../types/webhook'

interface WebhookCardCallbacks {
  onDelete: (webhookId: string) => void
  onRegenerateSecret: (webhookId: string) => void
}

export function useWebhookCardActions(webhook: Webhook, callbacks: WebhookCardCallbacks) {
  const [isDeleting, setIsDeleting] = useState(false)
  const [isRegenerating, setIsRegenerating] = useState(false)
  const [copiedUrl, setCopiedUrl] = useState(false)
  const [copiedSecret, setCopiedSecret] = useState(false)
  const deleteWebhook = async () => {
    if (!confirm(`Webhook「${webhook.name}」を削除してもよろしいですか？`)) return
    setIsDeleting(true)
    try { await callbacks.onDelete(webhook.id) } finally { setIsDeleting(false) }
  }
  const regenerateSecret = async () => {
    if (!confirm('シークレットを再生成すると、既存のシークレットは無効になります。続行しますか？')) return
    setIsRegenerating(true)
    try { await callbacks.onRegenerateSecret(webhook.id) } finally { setIsRegenerating(false) }
  }
  const copyUrl = () => copyWebhookValue(webhook.webhook_url, setCopiedUrl, 'webhook URL')
  const copySecret = () => copyWebhookValue(webhook.secret, setCopiedSecret, 'secret')
  return { isDeleting, isRegenerating, copiedUrl, copiedSecret, deleteWebhook, regenerateSecret, copyUrl, copySecret }
}

async function copyWebhookValue(value: string | undefined, setCopied: (value: boolean) => void, label: string) {
  if (!value) return
  try {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  } catch (reason) { console.error(`Failed to copy ${label}:`, reason) }
}

export type WebhookCardActionState = ReturnType<typeof useWebhookCardActions>

'use client'

import { Webhook } from '../../types/webhook'
import { WebhookCardActions } from './WebhookCardActions'
import { WebhookCardDetails } from './WebhookCardDetails'
import { useWebhookCardActions } from './useWebhookCardActions'

interface WebhookCardProps {
  webhook: Webhook
  onEdit: (webhook: Webhook) => void
  onDelete: (webhookId: string) => void
  onTogglePause: (webhookId: string, currentStatus: Webhook['status']) => void
  onRegenerateSecret: (webhookId: string) => void
}

export default function WebhookCard({ webhook, onEdit, onDelete, onTogglePause, onRegenerateSecret }: WebhookCardProps) {
  const actions = useWebhookCardActions(webhook, { onDelete, onRegenerateSecret })
  return <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4 hover:shadow-md transition-shadow">
    <WebhookCardDetails webhook={webhook} actions={actions} />
    <WebhookCardActions webhook={webhook} actions={actions} onEdit={onEdit} onTogglePause={onTogglePause} />
  </div>
}

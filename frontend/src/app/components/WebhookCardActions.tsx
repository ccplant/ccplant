import Link from 'next/link'
import { Webhook } from '../../types/webhook'
import { WebhookCardActionState } from './useWebhookCardActions'

interface WebhookCardActionsProps {
  webhook: Webhook
  actions: WebhookCardActionState
  onEdit: (webhook: Webhook) => void
  onTogglePause: (webhookId: string, currentStatus: Webhook['status']) => void
}

export function WebhookCardActions({ webhook, actions, onEdit, onTogglePause }: WebhookCardActionsProps) {
  const paused = webhook.status === 'paused'
  return <div className="flex items-center gap-2 mt-4 pt-3 border-t border-gray-100 dark:border-gray-700 flex-wrap">
    <ActionLink href={`/chats?tag=webhook_id:${webhook.id}`} label="セッション一覧" />
    <ActionLink href={`/webhooks/${webhook.id}`} label="テスト" />
    <ActionButton onClick={() => onEdit(webhook)} label="編集" />
    <ActionButton onClick={() => onTogglePause(webhook.id, webhook.status)} label={paused ? '再開' : '一時停止'} tone={paused ? 'green' : 'yellow'} />
    <ActionButton onClick={actions.regenerateSecret} label={actions.isRegenerating ? '再生成中...' : 'Secret再生成'} tone="purple" disabled={actions.isRegenerating} spinning={actions.isRegenerating} />
    <ActionButton onClick={actions.deleteWebhook} label={actions.isDeleting ? '削除中...' : '削除'} tone="red" disabled={actions.isDeleting} spinning={actions.isDeleting} trailing />
  </div>
}

function ActionLink({ href, label }: { href: string; label: string }) {
  return <Link href={href} className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"><ActionIcon spinning={false} />{label}</Link>
}

function ActionButton({ onClick, label, tone = 'gray', disabled = false, spinning = false, trailing = false }: { onClick: () => void; label: string; tone?: Tone; disabled?: boolean; spinning?: boolean; trailing?: boolean }) {
  const tones: Record<Tone, string> = {
    gray: 'text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20',
    green: 'text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/20', yellow: 'text-yellow-600 dark:text-yellow-400 hover:bg-yellow-50 dark:hover:bg-yellow-900/20',
    purple: 'text-purple-600 dark:text-purple-400 hover:bg-purple-50 dark:hover:bg-purple-900/20', red: 'text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20',
  }
  return <button onClick={onClick} disabled={disabled} className={`flex items-center gap-1 px-3 py-1.5 text-sm rounded transition-colors disabled:opacity-50 ${tones[tone]} ${trailing ? 'ml-auto' : ''}`}><ActionIcon spinning={spinning} />{label}</button>
}

type Tone = 'gray' | 'green' | 'yellow' | 'purple' | 'red'

function ActionIcon({ spinning }: { spinning: boolean }) {
  if (spinning) return <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24"><circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" /><path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" /></svg>
  return <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8s-9-3.582-9-8 4.03-8 9-8 9 3.582 9 8z" /></svg>
}

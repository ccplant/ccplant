import { Webhook } from '../../types/webhook'
import CopyableResourceId from './CopyableResourceId'
import WebhookStatusBadge from './WebhookStatusBadge'
import { WebhookCardActionState } from './useWebhookCardActions'

export function WebhookCardDetails({ webhook, actions }: { webhook: Webhook; actions: WebhookCardActionState }) {
  return <>
    <div className="flex items-start justify-between mb-3"><div className="flex-1 min-w-0">
      <h3 className="text-lg font-medium text-gray-900 dark:text-white truncate">{webhook.name}</h3>
      <CopyableResourceId id={webhook.id} className="mt-1" />
      <div className="flex items-center gap-2 mt-1"><WebhookStatusBadge status={webhook.status} /><span className="text-xs px-2 py-0.5 bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-400 rounded-full">{webhook.type === 'github' ? 'GitHub' : 'Custom'}</span></div>
    </div></div>
    <div className="space-y-2 text-sm">
      <CopyButtons webhook={webhook} actions={actions} />
      <ValueTags values={webhook.github?.allowed_events} icon="clipboard" />
      <ValueTags values={webhook.github?.allowed_repositories} icon="folder" mono />
      {!!webhook.triggers?.length && <Metric icon="bolt">トリガー: {webhook.triggers.length}件</Metric>}
      {!!webhook.delivery_count && <Metric icon="chart">配信回数: {webhook.delivery_count}</Metric>}
      {webhook.last_delivery && <LastDelivery webhook={webhook} />}
    </div>
  </>
}

function CopyButtons({ webhook, actions }: { webhook: Webhook; actions: WebhookCardActionState }) {
  return <div className="flex items-center gap-2">
    {webhook.webhook_url && <CopyButton copied={actions.copiedUrl} onClick={actions.copyUrl} label="URL をコピー" />}
    {webhook.secret && <CopyButton copied={actions.copiedSecret} onClick={actions.copySecret} label="Secretをコピー" />}
  </div>
}

function CopyButton({ copied, onClick, label }: { copied: boolean; onClick: () => void; label: string }) {
  const style = copied ? 'text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-900/20' : 'text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20'
  return <button onClick={onClick} className={`flex items-center gap-1 px-3 py-1.5 text-sm rounded transition-colors ${style}`} title={label}><SmallIcon name={copied ? 'check' : 'copy'} />{copied ? 'コピー済み' : label}</button>
}

function ValueTags({ values, icon, mono = false }: { values?: string[]; icon: IconName; mono?: boolean }) {
  if (!values?.length) return null
  return <div className="flex items-start gap-2 text-gray-600 dark:text-gray-400"><SmallIcon name={icon} /><div className="flex flex-wrap gap-1">{values.map((value) => <span key={value} className={`px-1.5 py-0.5 bg-gray-100 dark:bg-gray-700 text-xs rounded ${mono ? 'font-mono' : ''}`}>{value}</span>)}</div></div>
}

function Metric({ icon, children }: { icon: IconName; children: React.ReactNode }) {
  return <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400"><SmallIcon name={icon} /><span>{children}</span></div>
}

function LastDelivery({ webhook }: { webhook: Webhook }) {
  const delivery = webhook.last_delivery!
  const style = delivery.status === 'processed' ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400' : delivery.status === 'skipped' ? 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-400' : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  return <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400"><SmallIcon name="clock" /><span>最終配信: {formatDateTime(delivery.received_at)}</span><span className={`text-xs px-1.5 py-0.5 rounded ${style}`}>{delivery.status}</span></div>
}

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('ja-JP', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

type IconName = 'check' | 'copy' | 'clipboard' | 'folder' | 'bolt' | 'chart' | 'clock'
const iconPaths: Record<IconName, string> = {
  check: 'M5 13l4 4L19 7', copy: 'M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3',
  clipboard: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2', folder: 'M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z',
  bolt: 'M13 10V3L4 14h7v7l9-11h-7z', chart: 'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6m6 0V9a2 2 0 012-2h2a2 2 0 012 2v10m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14', clock: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z',
}

function SmallIcon({ name }: { name: IconName }) {
  return <svg className="w-4 h-4 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={iconPaths[name]} /></svg>
}

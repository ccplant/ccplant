export type UsagePreset = {
  id: string
  label: string
  description: string
  chart: 'bar' | 'line' | 'area'
  sql: string
}

export const usagePresets: UsagePreset[] = [
  {
    id: 'hourly-tokens',
    label: '時間別トークン',
    description: 'Input・Cache・Outputを時間単位で集計',
    chart: 'bar',
    sql: `SELECT
  date_trunc('hour', occurred_at) AS bucket,
  SUM(input_tokens) AS input_tokens,
  SUM(cached_input_tokens + cache_creation_tokens) AS cache_tokens,
  SUM(output_tokens) AS output_tokens
FROM usage_events
GROUP BY bucket
ORDER BY bucket`,
  },
  {
    id: 'daily-tokens',
    label: '日別トークン',
    description: '長期間の利用量を日単位で表示',
    chart: 'area',
    sql: `SELECT
  date_trunc('day', occurred_at) AS bucket,
  SUM(input_tokens) AS input_tokens,
  SUM(cached_input_tokens + cache_creation_tokens) AS cache_tokens,
  SUM(output_tokens) AS output_tokens
FROM usage_events
GROUP BY bucket
ORDER BY bucket`,
  },
  {
    id: 'models',
    label: 'モデル別',
    description: 'モデルごとのトークン利用量',
    chart: 'bar',
    sql: `SELECT
  model,
  SUM(input_tokens) AS input_tokens,
  SUM(cached_input_tokens + cache_creation_tokens) AS cache_tokens,
  SUM(output_tokens) AS output_tokens
FROM usage_events
GROUP BY model
ORDER BY input_tokens + cache_tokens + output_tokens DESC`,
  },
  {
    id: 'triggers',
    label: '起動元別',
    description: '手動・Webhook・Scheduleなどの利用量',
    chart: 'bar',
    sql: `SELECT
  trigger_type,
  COUNT(DISTINCT session_id) AS sessions,
  SUM(input_tokens + cached_input_tokens + cache_creation_tokens + output_tokens) AS tokens
FROM usage_events
GROUP BY trigger_type
ORDER BY tokens DESC`,
  },
  {
    id: 'sessions',
    label: '時間別セッション数',
    description: 'usageが発生したユニークセッション数',
    chart: 'line',
    sql: `SELECT
  date_trunc('hour', occurred_at) AS bucket,
  COUNT(DISTINCT session_id) AS sessions,
  COUNT(*) AS responses
FROM usage_events
GROUP BY bucket
ORDER BY bucket`,
  },
]

export interface UsageBreakdown {
  key: string
  events: number
  input_tokens: number
  output_tokens: number
  cached_input_tokens: number
  cache_creation_tokens: number
  reasoning_tokens: number
}

export interface UsageSummary {
  events: number
  input_tokens: number
  output_tokens: number
  cached_input_tokens: number
  cache_creation_tokens: number
  reasoning_tokens: number
  by_day: UsageBreakdown[]
  by_model: UsageBreakdown[]
  by_session: UsageBreakdown[]
}

export interface UsageQuery {
  team_id?: string
  from?: string
  to?: string
}

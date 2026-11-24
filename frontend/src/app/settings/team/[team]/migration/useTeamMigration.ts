'use client'

import { useEffect, useMemo, useState } from 'react'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { SandboxPolicy } from '@/types/sandbox_policy'
import type { Schedule } from '@/types/schedule'
import type { SessionProfile } from '@/types/session_profile'
import type { SlackBot } from '@/types/slackbot'
import type { Webhook } from '@/types/webhook'
import { useSettingsScope } from '../../../SettingsScopeContext'
import { executeTeamMigration } from './teamMigrationExecutor'
import { ExternalSourceResponse, MigrationResource, MigrationSourceMode, migrationKey } from './teamMigrationTypes'

export function useTeamMigration() {
  const { scopeId, userTeams, userTeamNames } = useSettingsScope()
  const [sourceMode, setSourceMode] = useState<MigrationSourceMode>('current')
  const [sourceTeam, setSourceTeam] = useState('')
  const [apiUrl, setApiUrl] = useState('')
  const [apiToken, setApiToken] = useState('')
  const [resources, setResources] = useState<MigrationResource[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [step, setStep] = useState<1 | 2 | 3>(1)
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [migrating, setMigrating] = useState(false)
  const [completed, setCompleted] = useState(0)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    setSourceTeam(''); setApiToken(''); setResources([]); setSelected(new Set()); setStep(1)
  }, [scopeId])
  const chosen = useMemo(() => resources.filter((item) => selected.has(migrationKey(item))), [resources, selected])
  const visible = useMemo(() => filterResources(resources, query), [resources, query])
  const loadResources = async () => runResourceLoad({ sourceMode, sourceTeam, apiUrl, apiToken, setLoading, setError, setResources, setSelected, setApiToken, setStep })
  const migrate = async () => executeTeamMigration({ chosen, sourceMode, scopeId, setMigrating, setCompleted, setError, setStep })
  const reset = () => { setSourceTeam(''); setApiToken(''); setResources([]); setSelected(new Set()); setStep(1) }
  const toggle = (key: string) => setSelected((current) => toggleKey(current, key))
  return {
    scopeId, sourceMode, setSourceMode, sourceTeam, setSourceTeam, apiUrl, setApiUrl, apiToken, setApiToken,
    resources, selected, setSelected, step, setStep, query, setQuery, loading, migrating, completed, error, setError,
    sourceTeams: userTeams.filter((team) => team !== scopeId), teamName: (id: string) => userTeamNames[id] || id,
    chosen, visible, loadResources, migrate, reset, toggle,
  }
}

interface ResourceLoadInput {
  sourceMode: MigrationSourceMode; sourceTeam: string; apiUrl: string; apiToken: string
  setLoading: (value: boolean) => void; setError: (value: string | null) => void
  setResources: (value: MigrationResource[]) => void; setSelected: (value: Set<string>) => void
  setApiToken: (value: string) => void; setStep: (value: 1 | 2 | 3) => void
}

async function runResourceLoad(input: ResourceLoadInput) {
  if (!input.sourceTeam || input.sourceMode === 'external' && (!input.apiUrl.trim() || !input.apiToken.trim())) return
  input.setLoading(true); input.setError(null)
  try {
    const payload = input.sourceMode === 'external' ? await loadExternalResources(input) : await loadCurrentResources(input.sourceTeam)
    const resources = buildResources(payload)
    input.setResources(resources); input.setSelected(new Set(resources.map(migrationKey)))
    if (input.sourceMode === 'external') input.setApiToken('')
    input.setStep(2)
  } catch (reason) { input.setError(reason instanceof Error ? reason.message : 'リソースを読み込めませんでした') }
  finally { input.setLoading(false) }
}

type ResourcePayload = { webhooks: Webhook[]; slackbots: SlackBot[]; schedules: Schedule[]; profiles: SessionProfile[]; policies: SandboxPolicy[] }

async function loadExternalResources(input: ResourceLoadInput): Promise<ResourcePayload> {
  const response = await fetch('/api/team-migration/source', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ api_url: input.apiUrl, token: input.apiToken, team_id: input.sourceTeam }) })
  const data = await response.json() as ExternalSourceResponse & { message?: string }
  if (!response.ok) throw new Error(data.message || '移行元へ接続できませんでした')
  return {
    webhooks: Array.isArray(data.webhooks) ? data.webhooks : data.webhooks.webhooks || [],
    slackbots: Array.isArray(data.slackbots) ? data.slackbots : data.slackbots.slackbots || [],
    schedules: Array.isArray(data.schedules) ? data.schedules : data.schedules.schedules || [],
    profiles: Array.isArray(data.profiles) ? data.profiles : data.profiles.session_profiles || [],
    policies: Array.isArray(data.policies) ? data.policies : data.policies.sandbox_policies || [],
  }
}

async function loadCurrentResources(teamId: string): Promise<ResourcePayload> {
  const client = createAgentAPIProxyClientFromStorage()
  const [webhooks, slackbots, schedules, profiles, policies] = await Promise.all([
    client.getWebhooks({ scope: 'team', team_id: teamId, limit: 100 }), client.getSlackBots({ scope: 'team', team_id: teamId, limit: 100 }),
    client.getSchedules({ scope: 'team', team_id: teamId, limit: 100 }), client.getSessionProfiles({ scope: 'team', team_id: teamId }),
    client.getSandboxPolicies({ scope: 'team', team_id: teamId }),
  ])
  return { webhooks: webhooks.webhooks, slackbots: slackbots.slackbots, schedules: schedules.schedules, profiles: profiles.session_profiles, policies: policies.sandbox_policies }
}

function buildResources(payload: ResourcePayload): MigrationResource[] {
  return [
    ...payload.policies.map((item) => ({ id: item.id, name: item.name, description: item.description, type: 'sandbox_policy' as const, payload: item })),
    ...payload.profiles.map((item) => ({ id: item.id, name: item.name, description: item.description, type: 'session_profile' as const, payload: item })),
    ...payload.schedules.map((item) => ({ id: item.id, name: item.name, description: item.cron_expr || item.scheduled_at, type: 'schedule' as const, payload: item })),
    ...payload.webhooks.map((item) => ({ id: item.id, name: item.name, description: `${item.type} · ${item.status}`, type: 'webhook' as const, payload: item })),
    ...payload.slackbots.map((item) => ({ id: item.id, name: item.name, description: item.status, type: 'slackbot' as const, payload: item })),
  ]
}

function filterResources(resources: MigrationResource[], query: string) {
  const needle = query.trim().toLowerCase()
  return needle ? resources.filter((item) => `${item.name} ${item.description || ''}`.toLowerCase().includes(needle)) : resources
}

function toggleKey(current: Set<string>, key: string) { const next = new Set(current); if (next.has(key)) next.delete(key); else next.add(key); return next }

export type TeamMigrationState = ReturnType<typeof useTeamMigration>

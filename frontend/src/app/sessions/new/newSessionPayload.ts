import type { DockerRegistryDraft, NewSessionDraft } from './newSessionDraft'
import { buildRepositoryTags } from './newSessionPresentation'

function buildDockerRegistry(registry: DockerRegistryDraft) {
  return {
    ...(registry.server ? { server: registry.server } : {}),
    ...(registry.secretName ? { secret_name: registry.secretName } : {}),
    ...(registry.username && !registry.secretName ? { username: registry.username, password: registry.password } : {}),
    ...(registry.insecure ? { insecure: true } : {}),
  }
}

export function buildNewSessionParams(draft: NewSessionDraft): Record<string, unknown> {
  const params: Record<string, unknown> = { message: draft.initialMessage.trim() }
  if (draft.connectionId) params.connection_id = draft.connectionId
  if (draft.selectedAgentType !== 'default') params.agent_type = draft.selectedAgentType
  if (draft.selectedManagerId) params.pool = draft.selectedManagerId
  if (draft.cycleEnabled && draft.cycleMessage.trim()) {
    params.cycle_message = draft.cycleMessage.trim()
    if (draft.cycleMaxCount > 0) params.cycle_max_count = draft.cycleMaxCount
  }
  if (draft.dockerEnabled) {
    const registries = draft.dockerRegistries
      .filter((r) => r.server || r.username || r.secretName)
      .map(buildDockerRegistry)
    params.docker = { enabled: true, ...(registries.length > 0 ? { registries } : {}) }
  }
  if (draft.sessionTTL.trim()) params.session_ttl = draft.sessionTTL.trim()
  return params
}

export function buildNewSessionRequest(draft: NewSessionDraft) {
  const repository = draft.freeFormRepository.trim()
  const tags = buildRepositoryTags(repository, draft.checkoutTarget, draft.checkoutBranch, draft.checkoutPrNumber)
  const environment: Record<string, string> = repository ? { REPOSITORY: repository } : {}
  const scope = draft.selectedTeam
    ? { scope: 'team' as const, team_id: draft.selectedTeam }
    : { scope: 'user' as const }
  return {
    environment,
    metadata: { description: draft.initialMessage.trim() },
    tags: Object.keys(tags).length > 0 ? tags : undefined,
    params: buildNewSessionParams(draft),
    ...(draft.sessionProfileId ? { session_profile_id: draft.sessionProfileId } : {}),
    ...scope,
  }
}

export function buildNewACPSessionRequest(draft: NewSessionDraft) {
  const repository = draft.freeFormRepository.trim()
  const tags = buildRepositoryTags(repository, draft.checkoutTarget, draft.checkoutBranch, draft.checkoutPrNumber)
  if (draft.selectedTeam) tags.team = draft.selectedTeam
  const name = repository ? repository.split('/').pop() : ''
  return {
    cwd: name ? `/home/user/workdir/${name}` : '/home/user',
    message: draft.initialMessage.trim(),
    agentType: draft.selectedAgentType !== 'default' ? draft.selectedAgentType : undefined,
    pool: draft.selectedManagerId || undefined,
    tags,
  }
}

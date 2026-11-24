import type { SessionProfile, SessionProfileParams } from '../../types/session_profile'
import type { ProfileEditorState } from './profileEditorState'

type DefaultModelTarget =
  | 'codex:all'
  | 'codex:auth_json'
  | 'codex:openai_compatible'
  | 'claude:all'
  | 'claude:oauth'
  | 'claude:bedrock'
  | 'claude:anthropic_compatible'

const DEFAULT_MODEL_TARGETS = [
  { value: 'codex:all', label: 'Codex / 全認証方式' },
  { value: 'codex:auth_json', label: 'Codex / auth.json' },
  { value: 'codex:openai_compatible', label: 'Codex / OpenAI 互換 API' },
  { value: 'claude:all', label: 'Claude / 全認証方式' },
  { value: 'claude:oauth', label: 'Claude / OAuth' },
  { value: 'claude:bedrock', label: 'Claude / Bedrock' },
  { value: 'claude:anthropic_compatible', label: 'Claude / Anthropic 互換 API' },
] as const satisfies ReadonlyArray<{ value: DefaultModelTarget; label: string }>

const SUPPORTED_AGENT_TYPES = new Set([
  'auto',
  'claude-legacy',
  'claude-acp',
  'codex-acp',
  'pi',
  'cursor',
  'antigravity',
])

const normalizeAgentType = (value?: string): string => {
  return value && SUPPORTED_AGENT_TYPES.has(value) ? value : ''
}

export function initializeProfileBasic(state: ProfileEditorState, profile: SessionProfile) {
  const { setName, setDescription, setIsDefault, setRepositorySelector, setOtherSelectorTags } = state
  setName(profile.name)
  setDescription(profile.description ?? '')
  setIsDefault(profile.is_default ?? false)
  const selectorEntries = Object.entries(profile.selector_tags ?? {})
  setRepositorySelector(
    (profile.selector_repositories?.length
      ? profile.selector_repositories
      : [selectorEntries.find(([key]) => key === 'repository' || key === 'repo')?.[1]].filter(Boolean)
    ).join('\n')
  )
  setOtherSelectorTags(Object.fromEntries(selectorEntries.filter(([key]) => key !== 'repository' && key !== 'repo')))
}

export function initializeProfileDocker(state: ProfileEditorState, cfg: SessionProfile['config']) {
  const { setDockerEnabled, setDockerRegistries } = state
  // Initialize docker from profile params
  const docker = cfg?.params?.docker
  if (docker) {
    setDockerEnabled(docker.enabled)
    setDockerRegistries(
      (docker.registries ?? []).map((r) => ({
        server: r.server ?? '',
        username: r.username ?? '',
        password: r.password ?? '',
        secretName: r.secret_name ?? '',
        insecure: r.insecure ?? false,
      }))
    )
  } else {
    setDockerEnabled(false)
    setDockerRegistries([])
  }
}

export function initializeProfileEnvironment(state: ProfileEditorState, cfg: SessionProfile['config']) {
  const {
    setMcpServers,
    setSourceProfileId,
    setSelectedSecretIds,
    setSkills,
    setPiPackages,
    setCommandWrapperTemplate,
    setSystemPrompt,
    setSystemPromptMode,
    setGitHubRepositories,
    setConnectionId,
  } = state
  setMcpServers(cfg?.mcp_servers ?? {})
  setSourceProfileId(cfg?.source_session_profile_id ?? '')
  setSelectedSecretIds(cfg?.secret_ids ?? [])
  setSkills(cfg?.skills ?? [])
  setPiPackages((cfg?.pi_packages ?? []).join('\n'))
  setCommandWrapperTemplate(cfg?.command_wrapper_template ?? '')
  setSystemPrompt(cfg?.system_prompt ?? '')
  setSystemPromptMode(cfg?.system_prompt_mode ?? 'override')
  setGitHubRepositories((cfg?.github_repositories ?? []).join('\n'))
  setConnectionId(cfg?.params?.connection_id ?? '')
}

export function initializeProfileKeyValues(state: ProfileEditorState, cfg: SessionProfile['config']) {
  const { setEnvPairs, setTagPairs } = state
  if (cfg?.environment && Object.keys(cfg.environment).length > 0) {
    const generalEnvironment = Object.entries(cfg.environment).filter(
      ([key]) => key !== 'CODEX_MODEL' && key !== 'ANTHROPIC_MODEL'
    )
    setEnvPairs(
      generalEnvironment.length > 0
        ? generalEnvironment.map(([key, value]) => ({ key, value }))
        : [{ key: '', value: '' }]
    )
  } else {
    setEnvPairs([{ key: '', value: '' }])
  }

  if (cfg?.tags && Object.keys(cfg.tags).length > 0) {
    setTagPairs(Object.entries(cfg.tags).map(([key, value]) => ({ key, value })))
  } else {
    setTagPairs([{ key: '', value: '' }])
  }
}

export function initializeProfileModels(state: ProfileEditorState, cfg: SessionProfile['config']) {
  const {
    setPool,
    setAgentType,
    setCodexModel,
    setClaudeModel,
    setCodexDefaultModels,
    setClaudeDefaultModels,
    setDefaultModelTarget,
    setModelOptions,
  } = state
  setPool(cfg?.pool || cfg?.params?.pool || '')
  setAgentType(normalizeAgentType(cfg?.params?.agent_type))
  setCodexModel(cfg?.environment?.CODEX_MODEL ?? '')
  setClaudeModel(cfg?.environment?.ANTHROPIC_MODEL ?? '')
  setCodexDefaultModels(cfg?.params?.codex_default_models ?? {})
  setClaudeDefaultModels(cfg?.params?.claude_default_models ?? {})
  const configuredTarget = DEFAULT_MODEL_TARGETS.find(({ value }) => {
    const [agent, mode] = value.split(':')
    if (mode === 'all')
      return agent === 'codex' ? Boolean(cfg?.environment?.CODEX_MODEL) : Boolean(cfg?.environment?.ANTHROPIC_MODEL)
    return agent === 'codex'
      ? Boolean(
          cfg?.params?.codex_default_models?.[mode as keyof NonNullable<SessionProfileParams['codex_default_models']>]
        )
      : Boolean(
          cfg?.params?.claude_default_models?.[mode as keyof NonNullable<SessionProfileParams['claude_default_models']>]
        )
  })
  setDefaultModelTarget(configuredTarget?.value ?? 'codex:all')
  setModelOptions((cfg?.params?.model_options ?? []).join('\n'))
}

export function initializeProfileRuntime(state: ProfileEditorState, cfg: SessionProfile['config']) {
  const {
    setProfileFiles,
    setProfileRepositories,
    setStorages,
    setSessionState,
    setSandboxPolicyId,
    setSessionTTL,
    setUnsyncedFilePaths,
    setSettingsTeamId,
    setCodexConnection,
    setClaudeConnection,
    setPiConnection,
    setCodexAuthMode,
    setClaudeAuthMode,
  } = state
  // Initialize network sandbox from profile params and legacy top-level policy ID
  const sandbox = cfg?.params?.sandbox
  const policyId = sandbox?.policy_id ?? cfg?.sandbox_policy_id ?? ''
  setSandboxPolicyId(policyId)

  // Initialize session_ttl from profile config
  const ttl = cfg?.session_ttl ?? cfg?.params?.session_ttl ?? ''
  setSessionTTL(ttl)

  const paths = cfg?.unsynced_file_paths ?? cfg?.params?.unsynced_file_paths ?? []
  setUnsyncedFilePaths(paths.join('\n'))
  setProfileFiles(cfg?.files ?? [])
  setProfileRepositories(cfg?.repositories ?? [])
  setStorages(cfg?.params?.storages ?? [])
  setSessionState(cfg?.session_state ?? { enabled: false, components: ['agent_state'], agents: ['claude'] })

  setSettingsTeamId(cfg?.settings_team_id ?? '')
  setCodexConnection(cfg?.codex_connection ?? null)
  setClaudeConnection(cfg?.claude_connection ?? null)
  setPiConnection(cfg?.pi_connection ?? null)
  setCodexAuthMode(cfg?.params?.codex_auth_mode ?? '')
  setClaudeAuthMode(cfg?.params?.claude_auth_mode ?? '')
}

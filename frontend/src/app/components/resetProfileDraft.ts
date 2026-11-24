import type { ProfileEditorState } from './profileEditorState'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

export function resetProfileDraft(state: ProfileEditorState, createScope: SessionProfileEditorProps['createScope']) {
  const {
    setName,
    setDescription,
    setIsDefault,
    setRepositorySelector,
    setOtherSelectorTags,
    setEnvPairs,
    setTagPairs,
    setPool,
    setAgentType,
    setCodexModel,
    setClaudeModel,
    setCodexDefaultModels,
    setClaudeDefaultModels,
    setDefaultModelTarget,
    setModelOptions,
    setProfileFiles,
    setProfileRepositories,
    setStorages,
    setSessionState,
    setMcpServers,
    setSourceProfileId,
    setAvailableProfiles,
    setSelectedSecretIds,
    setSkills,
    setPiPackages,
    setCommandWrapperTemplate,
    setSystemPrompt,
    setSystemPromptMode,
    setConnectionId,
    setDockerEnabled,
    setDockerRegistries,
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
  // Reset form
  setName('')
  setDescription('')
  setIsDefault(false)
  setRepositorySelector('')
  setOtherSelectorTags({})
  setEnvPairs([{ key: '', value: '' }])
  setTagPairs([{ key: '', value: '' }])
  setPool('')
  setAgentType('')
  setCodexModel('')
  setClaudeModel('')
  setCodexDefaultModels({})
  setClaudeDefaultModels({})
  setDefaultModelTarget('codex:all')
  setModelOptions('')
  setMcpServers({})
  setSourceProfileId('')
  setSelectedSecretIds([])
  setSkills([])
  setPiPackages('')
  setCommandWrapperTemplate('')
  setSystemPrompt('')
  setSystemPromptMode('override')
  setAvailableProfiles([])
  setDockerEnabled(false)
  setDockerRegistries([])
  setSandboxPolicyId('')
  setSessionTTL('')
  setUnsyncedFilePaths('')
  setProfileFiles([])
  setProfileRepositories([])
  setStorages([])
  setSessionState({ enabled: false, components: ['agent_state'], agents: ['claude'] })
  setSettingsTeamId(createScope?.scope === 'team' ? (createScope.team_id ?? '') : '')
  setCodexConnection(null)
  setClaudeConnection(null)
  setPiConnection(null)
  setCodexAuthMode('')
  setClaudeAuthMode('')
  setConnectionId('')
}

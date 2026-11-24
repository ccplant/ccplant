import type { ProfileEditorState } from './profileEditorState'
import type { SessionProfileEditorProps } from './SessionProfileEditor'
import type { ModelConnection } from '../../types/settings'
import { createProfileDraftActions } from './profileDraftActions'
import { buildProfileParams } from './profileParamsPayload'

export function buildProfileConfig(
  state: ProfileEditorState,
  editingProfile: SessionProfileEditorProps['editingProfile']
) {
  const {
    envPairs,
    tagPairs,
    pool,
    codexModel,
    claudeModel,
    profileFiles,
    profileRepositories,
    sessionState,
    mcpServers,
    sourceProfileId,
    selectedSecretIds,
    skills,
    piPackages,
    systemPrompt,
    systemPromptMode,
    githubRepositories,
    sandboxPolicyId,
    sessionTTL,
    unsyncedFilePaths,
    settingsTeamId,
    codexConnection,
    claudeConnection,
    piConnection,
  } = state
  const wrapper = state.commandWrapperTemplate.trim()
  const { pairsToRecord } = createProfileDraftActions(state)
  const environment = {
    ...pairsToRecord(envPairs),
    ...(codexModel.trim() ? { CODEX_MODEL: codexModel.trim() } : {}),
    ...(claudeModel.trim() ? { ANTHROPIC_MODEL: claudeModel.trim() } : {}),
  }
  const tags = pairsToRecord(tagPairs)
  const parsedUnsyncedFilePaths = unsyncedFilePaths
    .split('\n')
    .map((path) => path.trim())
    .filter(Boolean)
  const parsedProfileFiles = profileFiles
    .map((file) => ({ ...file, path: file.path.trim(), permissions: file.permissions?.trim() }))
    .filter((file) => file.path)
  const parsedProfileRepositories = profileRepositories
    .map((repository) => ({
      repository: repository.repository.trim(),
      path: repository.path.trim(),
      branch: repository.branch?.trim(),
    }))
    .filter((repository) => repository.repository || repository.path)
  if (parsedProfileRepositories.some((repository) => !/^[^/\s]+\/[^/\s]+$/.test(repository.repository)))
    throw new Error('事前配置リポジトリは owner/repository 形式で入力してください')
  if (parsedProfileRepositories.some((repository) => !repository.path.startsWith('/')))
    throw new Error('事前配置リポジトリの配置先は絶対パスで入力してください')

  const params = buildProfileParams(state, editingProfile)

  const connectionPayload = (connection: ModelConnection | null) => {
    if (!connection) return null
    const payload = { ...connection }
    delete payload.has_api_key
    if (!payload.api_key) delete payload.api_key
    return payload
  }
  const extraConfig = { ...editingProfile?.config }
  const parsedPiPackages = piPackages
    .split('\n')
    .map((value) => value.trim())
    .filter(Boolean)
  for (const key of [
    'settings_team_id',
    'codex_connection',
    'claude_connection',
    'pi_connection',
    'environment',
    'tags',
    'pool',
    'mcp_servers',
    'params',
    'sandbox_policy_id',
    'session_ttl',
    'unsynced_file_paths',
    'source_session_profile_id',
    'secret_ids',
    'github_repositories',
    'skills',
    'pi_packages',
    'files',
    'repositories',
    'command_wrapper_template',
    'system_prompt',
    'system_prompt_mode',
    'session_state',
  ] as const)
    delete extraConfig[key]
  const parsedGitHubRepositories = githubRepositories
    .split('\n')
    .map((value) => value.trim())
    .filter(Boolean)
  const config = {
    ...extraConfig,
    ...(settingsTeamId ? { settings_team_id: settingsTeamId } : {}),
    codex_connection: connectionPayload(codexConnection),
    claude_connection: connectionPayload(claudeConnection),
    pi_connection: connectionPayload(piConnection),
    ...(Object.keys(environment).length > 0 ? { environment } : {}),
    ...(Object.keys(tags).length > 0 ? { tags } : {}),
    ...(pool.trim() ? { pool: pool.trim() } : {}),
    ...(Object.keys(mcpServers).length > 0 ? { mcp_servers: mcpServers } : {}),
    params,
    ...(sandboxPolicyId ? { sandbox_policy_id: sandboxPolicyId } : {}),
    ...(sessionTTL.trim() ? { session_ttl: sessionTTL.trim() } : {}),
    ...(parsedUnsyncedFilePaths.length > 0 ? { unsynced_file_paths: parsedUnsyncedFilePaths } : {}),
    ...(sourceProfileId.trim() ? { source_session_profile_id: sourceProfileId.trim() } : {}),
    ...(selectedSecretIds.length > 0 ? { secret_ids: selectedSecretIds } : {}),
    ...(parsedGitHubRepositories.length > 0 ? { github_repositories: parsedGitHubRepositories } : {}),
    ...(skills.length > 0 ? { skills } : {}),
    ...(parsedPiPackages.length > 0 ? { pi_packages: parsedPiPackages } : {}),
    ...(wrapper ? { command_wrapper_template: wrapper } : {}),
    ...(systemPrompt ? { system_prompt: systemPrompt, system_prompt_mode: systemPromptMode } : {}),
    files: parsedProfileFiles,
    repositories: parsedProfileRepositories,
    session_state: sessionState,
  }

  return config
}

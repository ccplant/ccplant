import type { ProfileEditorState } from './profileEditorState'
import type { SessionProfileEditorProps } from './SessionProfileEditor'
import { buildProfileDocker } from './profileDockerPayload'
import { validateProfileStorage } from './profileStorageValidation'

export function buildProfileParams(
  state: ProfileEditorState,
  editingProfile: SessionProfileEditorProps['editingProfile']
) {
  const {
    agentType,
    codexDefaultModels,
    claudeDefaultModels,
    modelOptions,
    storages,
    connectionId,
    sandboxPolicyId,
    codexAuthMode,
    claudeAuthMode,
  } = state
  const dockerConfig = buildProfileDocker(state)

  const sandboxConfig = {
    enabled: true,
    ...(sandboxPolicyId ? { policy_id: sandboxPolicyId } : {}),
    ...(!sandboxPolicyId ? { count_mode: true } : {}),
  }

  // Build params if any param is set
  // Preserve fields managed through the API that are not exposed in this editor.
  const extraParams = { ...editingProfile?.config?.params }
  delete extraParams.storages
  validateProfileStorage(state)
  for (const key of [
    'pool',
    'agent_type',
    'model',
    'model_options',
    'sandbox',
    'docker',
    'connection_id',
    'codex_auth_mode',
    'claude_auth_mode',
    'codex_default_models',
    'claude_default_models',
    'session_ttl',
    'unsynced_file_paths',
  ] as const)
    delete extraParams[key]
  const parsedModelOptions = modelOptions
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
  const trimmedModels = <T extends Record<string, string | undefined>>(models: T) =>
    Object.fromEntries(
      Object.entries(models)
        .map(([mode, model]) => [mode, model?.trim()])
        .filter((entry): entry is [string, string] => Boolean(entry[1]))
    )
  const parsedCodexDefaultModels = trimmedModels(codexDefaultModels)
  const parsedClaudeDefaultModels = trimmedModels(claudeDefaultModels)
  const params = {
    ...extraParams,
    storages: storages.map((storage) => ({ ...storage, mount_path: storage.mount_path.trim() })),
    ...(agentType.trim() ? { agent_type: agentType.trim() } : {}),
    ...(connectionId ? { connection_id: connectionId } : {}),
    ...(parsedModelOptions.length > 0 ? { model_options: parsedModelOptions } : {}),
    sandbox: sandboxConfig,
    ...(dockerConfig ? { docker: dockerConfig } : {}),
    ...(codexAuthMode ? { codex_auth_mode: codexAuthMode } : {}),
    ...(claudeAuthMode ? { claude_auth_mode: claudeAuthMode } : {}),
    ...(Object.keys(parsedCodexDefaultModels).length > 0 ? { codex_default_models: parsedCodexDefaultModels } : {}),
    ...(Object.keys(parsedClaudeDefaultModels).length > 0 ? { claude_default_models: parsedClaudeDefaultModels } : {}),
  }

  return params
}

import { validateStorageAttachments } from './StorageAttachmentEditor'
import type { ProfileEditorState } from './profileEditorState'

export function validateProfileStorage(state: ProfileEditorState) {
  const { agentType, storages, sessionState } = state
  const storageError = validateStorageAttachments(storages)
  if (storageError) throw new Error(storageError)
  if (sessionState.enabled) {
    if (!sessionState.storage_id) throw new Error('セッション情報の保存先Storageを選択してください')
    if (!sessionState.components?.length) throw new Error('保持するセッション情報を選択してください')
    if (sessionState.components.includes('agent_state') && !sessionState.agents?.length)
      throw new Error('対象エージェントを選択してください')
    const fixedAgent = agentType.includes('codex') ? 'codex' : agentType.includes('claude') ? 'claude' : ''
    if (sessionState.components.includes('agent_state') && sessionState.agents?.includes('codex'))
      throw new Error('Codexのセッション情報はStorageへ保存できません')
    if (
      fixedAgent === 'claude' &&
      sessionState.components.includes('agent_state') &&
      !sessionState.agents?.includes('claude')
    )
      throw new Error('Profileのエージェントを保持対象に選択してください')
  }
}

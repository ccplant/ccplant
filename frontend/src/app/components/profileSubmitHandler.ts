'use client'

import { CreateSessionProfileRequest, SessionProfile, UpdateSessionProfileRequest } from '../../types/session_profile'

import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'

import { buildProfileConfig } from './profileConfigPayload'
import type { ProfileEditorState } from './profileEditorState'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Context = Pick<SessionProfileEditorProps, 'editingProfile' | 'createScope' | 'onSuccess'> & {
  getScopeParams: ReturnType<typeof useTeamScope>['getScopeParams']
  dirtyRef: React.RefObject<boolean>
}
export function createProfileSubmitHandler(
  state: ProfileEditorState,
  { editingProfile, createScope, onSuccess, getScopeParams, dirtyRef }: Context
) {
  const {
    name,
    description,
    isDefault,
    repositorySelector,
    otherSelectorTags,
    commandWrapperTemplate,
    setIsSubmitting,
    setError,
    setDirty,
  } = state
  const isEditing = !!editingProfile
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    if (!name.trim()) {
      setError('基本情報で名前を入力してください')
      return
    }

    const selectorRepositories = repositorySelector
      .split('\n')
      .map((value) => value.trim())
      .filter(Boolean)
    if (selectorRepositories.some((repository) => !/^[^/\s]+\/[^/\s]+$/.test(repository))) {
      setError('リポジトリ自動選択は org/repo 形式で入力してください')
      return
    }

    const wrapper = commandWrapperTemplate.trim()
    if (wrapper && (wrapper.match(/{{\s*\.Command\s*}}/g)?.length ?? 0) !== 1) {
      setError('コマンドラッパーには {{ .Command }} を1回だけ指定してください')
      return
    }

    setIsSubmitting(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()

      const selectorTags = { ...otherSelectorTags }
      const config = buildProfileConfig(state, editingProfile)

      let savedProfile: SessionProfile
      if (isEditing && editingProfile) {
        const updateData: UpdateSessionProfileRequest = {
          name: name.trim(),
          description: description.trim(),
          is_default: isDefault,
          selector_tags: selectorTags,
          selector_repositories: selectorRepositories,
          config: Object.keys(config).length > 0 ? config : undefined,
        }
        savedProfile = await client.updateSessionProfile(editingProfile.id, updateData)
      } else {
        const scopeParams = createScope ?? getScopeParams()
        const createData: CreateSessionProfileRequest = {
          name: name.trim(),
          description: description.trim(),
          is_default: isDefault,
          ...(Object.keys(selectorTags).length > 0 ? { selector_tags: selectorTags } : {}),
          ...(selectorRepositories.length > 0 ? { selector_repositories: selectorRepositories } : {}),
          ...(Object.keys(config).length > 0 ? { config } : {}),
          ...scopeParams,
        }
        savedProfile = await client.createSessionProfile(createData)
      }

      setDirty(false)
      dirtyRef.current = false
      onSuccess(savedProfile)
    } catch (err) {
      console.error('Failed to save session profile:', err)
      setError(err instanceof Error ? err.message : 'セッションプロファイルの保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }
  return handleSubmit
}

'use client'

import { useEffect, useRef } from 'react'
import type { ProfileEditorState } from './profileEditorState'
import type { SessionProfileEditorProps } from './SessionProfileEditor'
import { resetProfileDraft } from './resetProfileDraft'
import {
  initializeProfileBasic,
  initializeProfileModels,
  initializeProfileEnvironment,
  initializeProfileKeyValues,
  initializeProfileDocker,
  initializeProfileRuntime,
} from './profileInitialization'

export function useProfileInitialization(
  state: ProfileEditorState,
  editingProfile: SessionProfileEditorProps['editingProfile'],
  createScope: SessionProfileEditorProps['createScope']
) {
  const stateRef = useRef(state)
  stateRef.current = state
  const { resetKey } = state
  const scope = createScope?.scope
  const teamId = createScope?.team_id
  useEffect(() => {
    const current = stateRef.current
    if (editingProfile) {
      initializeProfileBasic(current, editingProfile)
      initializeProfileModels(current, editingProfile.config)
      initializeProfileEnvironment(current, editingProfile.config)
      initializeProfileKeyValues(current, editingProfile.config)
      initializeProfileDocker(current, editingProfile.config)
      initializeProfileRuntime(current, editingProfile.config)
    } else {
      resetProfileDraft(current, scope ? { scope, team_id: teamId } : undefined)
    }
    current.setError(null)
    current.setDirty(false)
  }, [editingProfile, resetKey, scope, teamId])
}

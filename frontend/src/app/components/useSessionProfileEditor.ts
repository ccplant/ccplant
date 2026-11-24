'use client'

import { useEffect, useRef } from 'react'
import { usePathname, useSearchParams } from 'next/navigation'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import type { SessionProfileEditorProps } from './SessionProfileEditor'
import { useProfileBasicState } from './useProfileBasicState'
import { useProfileConfigurationState } from './useProfileConfigurationState'
import { useProfileRuntimeState } from './useProfileRuntimeState'
import { useProfileInterfaceState } from './useProfileInterfaceState'
import { useProfileInitialization } from './useProfileInitialization'
import { useProfileOptions } from './useProfileOptions'
import { createProfileDraftActions } from './profileDraftActions'
import { createProfileSubmitHandler } from './profileSubmitHandler'

export function useSessionProfileEditor({
  onSuccess,
  editingProfile,
  section = 'basic',
  createScope,
}: SessionProfileEditorProps) {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const { getScopeParams, availableTeams = [] } = useTeamScope()
  const basic = useProfileBasicState()
  const configuration = useProfileConfigurationState()
  const runtime = useProfileRuntimeState()
  const interfaceState = useProfileInterfaceState()
  const state = { ...basic, ...configuration, ...runtime, ...interfaceState }
  const { dirty, setDrawerOpen } = interfaceState
  const dirtyRef = useRef(false)
  dirtyRef.current = dirty
  const scope = editingProfile?.scope ?? createScope?.scope
  const teamId = editingProfile?.team_id ?? createScope?.team_id
  useProfileInitialization(state, editingProfile, createScope)
  useProfileOptions(state, scope, teamId, getScopeParams)
  useEffect(() => {
    setDrawerOpen(false)
  }, [section, setDrawerOpen])
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (dirtyRef.current) {
        event.preventDefault()
        event.returnValue = ''
      }
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [])
  const actions = createProfileDraftActions(state)
  const handleSubmit = createProfileSubmitHandler(state, {
    editingProfile,
    createScope,
    onSuccess,
    getScopeParams,
    dirtyRef,
  })
  return {
    ...state,
    ...actions,
    pathname,
    searchParams,
    getScopeParams,
    availableTeams,
    isEditing: !!editingProfile,
    scope,
    teamId,
    handleSubmit,
  }
}

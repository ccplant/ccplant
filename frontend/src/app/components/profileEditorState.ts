import type { useProfileBasicState } from './useProfileBasicState'
import type { useProfileConfigurationState } from './useProfileConfigurationState'
import type { useProfileInterfaceState } from './useProfileInterfaceState'
import type { useProfileRuntimeState } from './useProfileRuntimeState'

export type ProfileEditorState = ReturnType<typeof useProfileBasicState> &
  ReturnType<typeof useProfileConfigurationState> &
  ReturnType<typeof useProfileRuntimeState> &
  ReturnType<typeof useProfileInterfaceState>

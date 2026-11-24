'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import { ProfileConnectionField, ProfileFileList, ProfileRepositoryList, UnsyncedPathsField } from './ProfileFileEditors'

export type ProfileFilesSectionProps = Pick<ReturnType<typeof useSessionProfileEditor>, 'profileFiles' | 'profileRepositories' | 'linkedConnections' | 'connectionId' | 'setConnectionId' | 'unsyncedFilePaths' | 'setUnsyncedFilePaths' | 'setDirty' | 'updateProfileFile' | 'addProfileFile' | 'removeProfileFile' | 'updateProfileRepository' | 'addProfileRepository' | 'removeProfileRepository'>

export function ProfileFilesSection(props: ProfileFilesSectionProps) {
  return <div className="space-y-5">
    <ProfileRepositoryList {...props} />
    <ProfileConnectionField {...props} />
    <ProfileFileList {...props} />
    <UnsyncedPathsField {...props} />
  </div>
}

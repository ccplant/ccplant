'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'
import StorageAttachmentEditor from './StorageAttachmentEditor'
import SessionStateStorageEditor from './SessionStateStorageEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  | 'getScopeParams'
  | 'agentType'
  | 'storages'
  | 'setStorages'
  | 'sessionState'
  | 'setSessionState'
  | 'setDirty'
  | 'scope'
  | 'teamId'
> &
  Pick<SessionProfileEditorProps, 'editingProfile' | 'createScope'>

export function ProfileStoragesSection({
  getScopeParams,
  agentType,
  storages,
  setStorages,
  sessionState,
  setSessionState,
  setDirty,
  editingProfile,
  createScope,
}: Props) {
  return (
    <div className="space-y-6">
      <SessionStateStorageEditor
        value={sessionState}
        onChange={(value) => {
          setSessionState(value)
          setDirty(true)
        }}
        scope={(editingProfile ?? createScope ?? getScopeParams()).scope ?? 'user'}
        teamId={(editingProfile ?? createScope ?? getScopeParams()).team_id}
        agentType={agentType}
      />
      <div>
        <h3 className="mb-1 font-medium">追加のStorageマウント</h3>
        <p className="mb-4 text-sm text-gray-500 dark:text-gray-400">
          データセットや共有ファイルを任意のパスへ配置します。
        </p>
        <StorageAttachmentEditor
          value={storages}
          onChange={(value) => {
            setStorages(value)
            setDirty(true)
          }}
          scope={(editingProfile ?? createScope ?? getScopeParams()).scope ?? 'user'}
          teamId={(editingProfile ?? createScope ?? getScopeParams()).team_id}
        />
      </div>
    </div>
  )
}

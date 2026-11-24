'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  | 'getScopeParams'
  | 'availableTeams'
  | 'name'
  | 'sourceProfileId'
  | 'setSourceProfileId'
  | 'availableProfiles'
  | 'settingsTeamId'
  | 'setSettingsTeamId'
  | 'setDirty'
  | 'scope'
> &
  Pick<SessionProfileEditorProps, 'editingProfile' | 'createScope'>

export function ProfileInheritanceSection({
  getScopeParams,
  availableTeams,
  sourceProfileId,
  setSourceProfileId,
  availableProfiles,
  settingsTeamId,
  setSettingsTeamId,
  setDirty,
  editingProfile,
  createScope,
}: Props) {
  return (
    <div className="space-y-5">
      <div>
        <label htmlFor="profile-settings-team" className="block text-sm font-medium mb-1">
          ベースにする設定
        </label>
        <select
          id="profile-settings-team"
          value={settingsTeamId}
          onChange={(e) => setSettingsTeamId(e.target.value)}
          className="w-full px-3 py-2 text-sm border rounded-lg dark:bg-gray-700 dark:text-white"
        >
          <option value="">既定の個人・チーム設定</option>
          {[...new Set([...availableTeams, ...(settingsTeamId ? [settingsTeamId] : [])])]
            .filter((team) => {
              const scope = editingProfile ?? createScope ?? getScopeParams()
              return scope.scope !== 'team' || scope.team_id === team
            })
            .map((team) => (
              <option key={team} value={team}>
                チーム: {team}
              </option>
            ))}
        </select>
        <p className="text-xs text-gray-500 mt-2">
          選んだチームの設定をベースにします。認証方法・モデル・環境変数・MCP
          は、このプロファイルで指定した項目だけ上書きします。チームの設定自体は変更されません。
        </p>
      </div>

      <div>
        <label htmlFor="profile-source" className="block text-sm font-medium mb-1">
          設定ソースプロファイル
        </label>
        <select
          id="profile-source"
          value={sourceProfileId}
          onChange={(e) => {
            setSourceProfileId(e.target.value)
            setDirty(true)
          }}
          className="w-full px-3 py-2 text-sm border rounded-lg dark:bg-gray-700 dark:text-white"
        >
          <option value="">プロファイルを参照しない</option>
          {availableProfiles
            .filter((profile) => profile.id !== editingProfile?.id)
            .map((profile) => (
              <option key={profile.id} value={profile.id}>
                {profile.name}
              </option>
            ))}
        </select>
        <p className="text-xs text-gray-500 mt-2">
          参照先プロファイルの環境変数と MCP サーバーを取り込みます。このプロファイルの同名設定が優先されます。
        </p>
      </div>
    </div>
  )
}

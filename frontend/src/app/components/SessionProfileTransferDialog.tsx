import { SessionProfile } from '../../types/session_profile'
import type { ProxyUserInfo } from '../../types/user'

interface Props {
  profile: SessionProfile
  teamId: string
  transferring: boolean
  currentUser: ProxyUserInfo | null
  onTeamIdChange: (teamId: string) => void
  onCancel: () => void
  onTransfer: () => void
}

export default function SessionProfileTransferDialog({ profile, teamId, transferring, currentUser, onTeamIdChange, onCancel, onTransfer }: Props) {
  const teams = currentUser?.team_principals?.length
    ? currentUser.team_principals.map((team) => ({ id: team.team_id, name: team.name || team.team_id }))
    : (currentUser?.teams || []).map((team) => ({ id: team, name: team }))
  const disabled = transferring || (profile.scope !== 'team' && !teamId) || (profile.scope === 'team' && !currentUser?.is_admin)
  return <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-modal="true" aria-labelledby="transfer-profile-title">
    <div className="w-full max-w-md rounded-xl bg-white p-6 shadow-xl dark:bg-gray-800">
      <h2 id="transfer-profile-title" className="text-lg font-semibold text-gray-900 dark:text-white">セッションプロファイルを移管</h2>
      <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">「{profile.name}」を{profile.scope === 'team' ? '個人' : 'チーム'}スコープへ移管します。IDは維持され、移管元の一覧からは削除されます。</p>
      {profile.scope === 'team' ? <p className="mt-4 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">チームから個人への移管は管理者のみ実行できます。</p> : <label className="mt-4 block text-sm font-medium text-gray-700 dark:text-gray-200">移管先チーム<select value={teamId} onChange={(event) => onTeamIdChange(event.target.value)} className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-gray-600 dark:bg-gray-900"><option value="">チームを選択してください</option>{teams.map((team) => <option key={team.id} value={team.id}>{team.name} ({team.id})</option>)}</select></label>}
      <div className="mt-6 flex justify-end gap-2">
        <button type="button" onClick={onCancel} disabled={transferring} className="rounded-lg px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-700">キャンセル</button>
        <button type="button" onClick={onTransfer} disabled={disabled} className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{transferring ? '移管中…' : '移管する'}</button>
      </div>
    </div>
  </div>
}

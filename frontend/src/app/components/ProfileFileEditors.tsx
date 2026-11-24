import type { ProfileFilesSectionProps } from './ProfileFilesSection'

function RepositoryItem({ index, props }: { index: number; props: ProfileFilesSectionProps }) {
  const repository = props.profileRepositories[index]
  const inputClass = 'w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 font-mono'
  return <div className="rounded border border-gray-200 dark:border-gray-600 p-3 space-y-2 bg-gray-50 dark:bg-gray-900/40">
    <div className="flex items-end gap-2"><div className="min-w-0 flex-1"><label htmlFor={`profile-repository-${index}-name`} className="block text-xs text-gray-500 dark:text-gray-400 mb-1">リポジトリ</label><input id={`profile-repository-${index}-name`} value={repository.repository} onChange={(event) => props.updateProfileRepository(index, 'repository', event.target.value)} placeholder="owner/repository" className={inputClass} /></div><button type="button" aria-label={`リポジトリ ${index + 1} を削除`} onClick={() => props.removeProfileRepository(index)} className="p-1 text-gray-400 hover:text-red-500">×</button></div>
    <div><label htmlFor={`profile-repository-${index}-path`} className="block text-xs text-gray-500 dark:text-gray-400 mb-1">配置先</label><input id={`profile-repository-${index}-path`} value={repository.path} onChange={(event) => props.updateProfileRepository(index, 'path', event.target.value)} placeholder="/home/agentapi/workdir/shared" className={inputClass} /></div>
    <div><label htmlFor={`profile-repository-${index}-branch`} className="block text-xs text-gray-500 dark:text-gray-400 mb-1">ブランチ（任意）</label><input id={`profile-repository-${index}-branch`} value={repository.branch || ''} onChange={(event) => props.updateProfileRepository(index, 'branch', event.target.value)} placeholder="main" className={inputClass} /></div>
  </div>
}

export function ProfileRepositoryList(props: ProfileFilesSectionProps) {
  return <div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">事前配置リポジトリ</label><p className="text-xs text-gray-500 dark:text-gray-400 mb-2">エージェントの起動前に clone するリポジトリと配置先を指定します。メインの作業リポジトリは変更しません。</p>{!props.profileRepositories.length && <p className="text-xs text-gray-400 dark:text-gray-500">登録済みリポジトリはありません。</p>}<div className="space-y-3">{props.profileRepositories.map((_, index) => <RepositoryItem key={index} index={index} props={props} />)}</div><button type="button" onClick={props.addProfileRepository} className="mt-2 text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800">リポジトリを追加</button></div>
}

export function ProfileConnectionField(props: ProfileFilesSectionProps) {
  const change = (value: string) => { props.setConnectionId(value); props.setDirty(true) }
  return <div><label htmlFor="profile-repository-connection" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">トークンコネクション</label><select id="profile-repository-connection" value={props.connectionId} onChange={(event) => change(event.target.value)} className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700"><option value="">リポジトリ owner から自動選択</option>{props.linkedConnections.map((identity) => <option key={identity.id} value={identity.connection_id}>{identity.connection_name} ({identity.login})</option>)}</select><p className="mt-1 text-xs text-gray-500">この connection の token をメインリポジトリと事前配置リポジトリの clone に使用します。</p></div>
}

function FileItem({ index, props }: { index: number; props: ProfileFilesSectionProps }) {
  const file = props.profileFiles[index]
  const inputClass = 'w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500'
  return <div className="rounded border border-gray-200 dark:border-gray-600 p-3 space-y-2 bg-gray-50 dark:bg-gray-900/40">
    <div className="flex items-end gap-2"><div className="min-w-0 flex-1"><label htmlFor={`profile-file-${index}-name`} className="block text-xs text-gray-500 dark:text-gray-400 mb-1">表示名</label><input type="text" id={`profile-file-${index}-name`} value={file.name || ''} onChange={(event) => props.updateProfileFile(index, 'name', event.target.value)} placeholder="SSH 鍵" className={inputClass} /></div><button type="button" aria-label={`ファイル ${index + 1} を削除`} onClick={() => props.removeProfileFile(index)} className="p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400">×</button></div>
    <div><label htmlFor={`profile-file-${index}-path`} className="block text-xs text-gray-500 dark:text-gray-400 mb-1">配置パス</label><input type="text" id={`profile-file-${index}-path`} value={file.path} onChange={(event) => props.updateProfileFile(index, 'path', event.target.value)} placeholder="/home/agentapi/.ssh/id_rsa" className={inputClass} /></div>
    <div><label htmlFor={`profile-file-${index}-content`} className="block text-xs text-gray-500 dark:text-gray-400 mb-1">内容</label><textarea id={`profile-file-${index}-content`} value={file.content || ''} onChange={(event) => props.updateProfileFile(index, 'content', event.target.value)} rows={4} className={`${inputClass} resize-y`} /></div>
    <div><label htmlFor={`profile-file-${index}-permissions`} className="block text-xs text-gray-500 dark:text-gray-400 mb-1">パーミッション</label><input type="text" id={`profile-file-${index}-permissions`} value={file.permissions || ''} onChange={(event) => props.updateProfileFile(index, 'permissions', event.target.value)} placeholder="0600" className={`w-24 ${inputClass}`} /></div>
  </div>
}

export function ProfileFileList(props: ProfileFilesSectionProps) {
  return <div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">登録ファイル</label><p className="text-xs text-gray-500 dark:text-gray-400 mb-2">このプロファイルでセッション起動時に配置するファイルを登録します。パスは絶対パスで指定してください。</p>{!props.profileFiles.length && <p className="text-xs text-gray-400 dark:text-gray-500">登録済みファイルはありません。</p>}<div className="space-y-3">{props.profileFiles.map((_, index) => <FileItem key={index} index={index} props={props} />)}</div><button type="button" onClick={props.addProfileFile} className="mt-2 text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300">ファイルを追加</button></div>
}

export function UnsyncedPathsField(props: ProfileFilesSectionProps) {
  return <div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">同期しないファイルパス</label><p className="text-xs text-gray-500 dark:text-gray-400 mb-2">変更を保存先へ同期しない管理ファイルのパスを 1 行に 1 つ指定します。</p><textarea value={props.unsyncedFilePaths} onChange={(event) => props.setUnsyncedFilePaths(event.target.value)} placeholder="/home/agentapi/.codex/auth.json" rows={3} className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white font-mono resize-y" /></div>
}

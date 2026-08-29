'use client'

import { FormEvent, useCallback, useEffect, useState } from 'react'
import { Copy, Github, Plus, Trash2, X } from 'lucide-react'
import { SettingsPageHeader } from '@/components/settings'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import { GitHubConnection, GitHubConnectionInput, GitHubSecretSource } from '@/types/github-connection'
import { useToast } from '@/contexts/ToastContext'

const emptyForm: GitHubConnectionInput = {
  name: '', base_url: 'https://github.com', api_url: 'https://api.github.com', oauth_client_id: '', enabled: true,
  oauth_client_secret: { source: 'encrypted', value: '' },
}

export default function GitHubConnectionsAdminPage() {
  const [connections, setConnections] = useState<GitHubConnection[]>([])
  const [form, setForm] = useState<GitHubConnectionInput>(emptyForm)
  const [editing, setEditing] = useState<GitHubConnection | null>(null)
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const { showToast } = useToast()

  const load = useCallback(async () => {
    setLoading(true)
    try { setConnections(await createCurrentDeploymentAgentAPIProxyClient().listGitHubConnections(true)) }
    catch { showToast('GitHub Connectionsを読み込めませんでした', 'error') }
    finally { setLoading(false) }
  }, [showToast])

  useEffect(() => { void load() }, [load])

  const startCreate = () => { setEditing(null); setForm(emptyForm); setOpen(true) }
  const startEdit = (item: GitHubConnection) => {
    setEditing(item)
    setForm({ name: item.name, base_url: item.base_url, api_url: item.api_url || '', oauth_client_id: item.oauth_client_id || '', enabled: item.enabled,
      oauth_client_secret: { source: item.secret_source || 'encrypted', environment: item.secret_environment || '', value: '' } })
    setOpen(true)
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault(); setSaving(true)
    const client = createCurrentDeploymentAgentAPIProxyClient()
    try {
      if (!editing) {
        await client.createGitHubConnection(form)
      } else {
        await client.updateGitHubConnection(editing.id, { name: form.name, base_url: form.base_url, api_url: form.api_url, oauth_client_id: form.oauth_client_id, enabled: form.enabled })
        const secret = form.oauth_client_secret
        if (secret && ((secret.source === 'encrypted' && secret.value) || (secret.source === 'environment' && secret.environment !== editing.secret_environment))) {
          await client.updateGitHubConnectionSecret(editing.id, secret)
        }
      }
      showToast(editing ? 'Connectionを更新しました' : 'Connectionを追加しました', 'success')
      setOpen(false); await load()
    } catch { showToast('Connectionを保存できませんでした', 'error') }
    finally { setSaving(false) }
  }

  const remove = async (item: GitHubConnection) => {
    if (!confirm(`${item.name}を削除しますか？ 連携済みidentityがある場合は削除できません。`)) return
    try { await createCurrentDeploymentAgentAPIProxyClient().deleteGitHubConnection(item.id); showToast('Connectionを削除しました', 'success'); await load() }
    catch { showToast('Connectionを削除できませんでした。先に無効化するか、連携状況を確認してください。', 'error') }
  }

  const testConnection = async (item: GitHubConnection) => {
    try {
      const result = await createCurrentDeploymentAgentAPIProxyClient().testGitHubConnection(item.id)
      showToast(result.api_reachable && result.secret_resolvable ? 'API接続とSecret解決を確認しました' : `確認結果: API ${result.api_reachable ? 'OK' : 'NG'} / Secret ${result.secret_resolvable ? 'OK' : 'NG'}`, result.api_reachable && result.secret_resolvable ? 'success' : 'error')
    } catch { showToast('接続テストを実行できませんでした', 'error') }
  }

  const setSecretSource = (source: GitHubSecretSource) => setForm({ ...form, oauth_client_secret: { source, value: '', environment: '' } })
  const input = 'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900 dark:text-white'

  return <>
    <SettingsPageHeader title="GitHub Connections" description="GitHub.comとGHESのOAuth接続を管理します。" action={<button onClick={startCreate} className="inline-flex items-center gap-2 rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700"><Plus className="h-4 w-4" />追加</button>} />
    {loading ? <p className="py-8 text-sm text-gray-500">読み込み中...</p> : <div className="space-y-3">
      {connections.map((item) => <div key={item.id} className="rounded-lg border border-gray-200 bg-white p-5 dark:border-gray-700 dark:bg-gray-800">
        <div className="flex items-start justify-between gap-4"><div className="flex min-w-0 gap-3"><Github className="mt-0.5 h-5 w-5 shrink-0" /><div><h2 className="font-semibold dark:text-white">{item.name}</h2><p className="mt-1 break-all text-sm text-gray-500">{item.base_url}</p></div></div>
          <div className="flex gap-2"><button onClick={() => void testConnection(item)} className="rounded-md border px-3 py-1.5 text-sm dark:border-gray-600">接続テスト</button><button onClick={() => startEdit(item)} className="rounded-md border px-3 py-1.5 text-sm dark:border-gray-600">編集</button><button onClick={() => void remove(item)} aria-label="削除" className="rounded-md border border-red-200 p-2 text-red-600 dark:border-red-800"><Trash2 className="h-4 w-4" /></button></div></div>
        <div className="mt-4 flex flex-wrap gap-2 text-xs"><span className={`rounded-full px-2 py-1 ${item.enabled ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-600'}`}>{item.enabled ? 'Enabled' : 'Disabled'}</span><span className="rounded-full bg-gray-100 px-2 py-1 text-gray-600">{item.secret_source} / {item.secret_configured ? '設定済み' : '未設定'}</span><span className="rounded-full bg-gray-100 px-2 py-1 text-gray-600">{item.linked_identities || 0} identities</span></div>
        <button onClick={() => { const callback = `${window.location.origin}/api/proxy/auth/github-connections/callback`; void navigator.clipboard.writeText(callback); showToast('Callback URLをコピーしました', 'success') }} className="mt-4 inline-flex items-center gap-1 text-xs text-blue-600"><Copy className="h-3.5 w-3.5" />Callback URLをコピー</button>
      </div>)}
      {!connections.length && <div className="rounded-lg border border-dashed p-8 text-center text-sm text-gray-500">Connectionはまだありません。</div>}
    </div>}

    {open && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"><form onSubmit={submit} className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-xl bg-white p-6 shadow-xl dark:bg-gray-800">
      <div className="mb-5 flex items-center justify-between"><h2 className="text-lg font-semibold dark:text-white">{editing ? 'Connectionを編集' : 'Connectionを追加'}</h2><button type="button" onClick={() => setOpen(false)}><X className="h-5 w-5" /></button></div>
      <div className="space-y-4">
        <label className="block text-sm dark:text-white">名前<input required className={`${input} mt-1`} value={form.name} onChange={e => setForm({...form, name:e.target.value})} /></label>
        <label className="block text-sm dark:text-white">Base URL<input required type="url" className={`${input} mt-1`} value={form.base_url} onChange={e => setForm({...form, base_url:e.target.value})} /></label>
        <label className="block text-sm dark:text-white">API URL<input required type="url" className={`${input} mt-1`} value={form.api_url} onChange={e => setForm({...form, api_url:e.target.value})} /></label>
        <label className="block text-sm dark:text-white">OAuth Client ID<input required className={`${input} mt-1`} value={form.oauth_client_id} onChange={e => setForm({...form, oauth_client_id:e.target.value})} /></label>
        <label className="block text-sm dark:text-white">Secret保存方式<select className={`${input} mt-1`} value={form.oauth_client_secret?.source} onChange={e => setSecretSource(e.target.value as GitHubSecretSource)}><option value="encrypted">暗号化して保存</option><option value="environment">環境変数を参照</option></select></label>
        {form.oauth_client_secret?.source === 'encrypted' ? <label className="block text-sm dark:text-white">OAuth Client Secret<input required={!editing || !editing.secret_configured} type="password" autoComplete="new-password" placeholder={editing?.secret_configured ? '変更する場合のみ入力' : ''} className={`${input} mt-1`} value={form.oauth_client_secret.value || ''} onChange={e => setForm({...form, oauth_client_secret:{source:'encrypted', value:e.target.value}})} /></label> : <label className="block text-sm dark:text-white">環境変数名<input required className={`${input} mt-1`} placeholder="GITHUB_OAUTH_CORP_CLIENT_SECRET" value={form.oauth_client_secret?.environment || ''} onChange={e => setForm({...form, oauth_client_secret:{source:'environment', environment:e.target.value}})} /></label>}
        <label className="flex items-center gap-2 text-sm dark:text-white"><input type="checkbox" checked={form.enabled} onChange={e => setForm({...form, enabled:e.target.checked})} />有効</label>
      </div>
      <div className="mt-6 flex justify-end gap-3"><button type="button" onClick={() => setOpen(false)} className="rounded-md border px-4 py-2 text-sm dark:border-gray-600">キャンセル</button><button disabled={saving} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">{saving ? '保存中...' : '保存'}</button></div>
    </form></div>}
  </>
}

'use client'

import { FormEvent, useCallback, useEffect, useState } from 'react'
import { Copy, Plus, Search, Trash2, X } from 'lucide-react'
import { SettingsPageHeader } from '@/components/settings'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import { GoogleConnection, GoogleConnectionInput, GoogleSecretSource } from '@/types/google-connection'
import { useToast } from '@/contexts/ToastContext'

const emptyForm: GoogleConnectionInput = { name: '', oauth_client_id: '', enabled: true, allow_login: true, show_on_login: true, allow_user_creation: false, hosted_domains: [], email_domains: [], oauth_client_secret: { source: 'encrypted', value: '' } }
const domains = (value: string) => value.split(',').map(item => item.trim().toLowerCase()).filter(Boolean)

export default function GoogleConnectionsAdminPage() {
  const [connections, setConnections] = useState<GoogleConnection[]>([])
  const [form, setForm] = useState<GoogleConnectionInput>(emptyForm)
  const [editing, setEditing] = useState<GoogleConnection | null>(null)
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const { showToast } = useToast()
  const load = useCallback(async () => { try { setConnections(await createCurrentDeploymentAgentAPIProxyClient().listGoogleConnections(true)) } catch { showToast('Google Connectionsを読み込めませんでした', 'error') } }, [showToast])
  useEffect(() => { void load() }, [load])
  const startEdit = (item: GoogleConnection) => { setEditing(item); setForm({ name: item.name, oauth_client_id: item.oauth_client_id || '', enabled: item.enabled, allow_login: item.allow_login !== false, show_on_login: item.show_on_login !== false, allow_user_creation: item.allow_user_creation === true, hosted_domains: item.hosted_domains || [], email_domains: item.email_domains || [], oauth_client_secret: { source: item.secret_source || 'encrypted', environment: item.secret_environment || '', value: '' } }); setOpen(true) }
  const submit = async (event: FormEvent) => {
    event.preventDefault(); setSaving(true)
    const client = createCurrentDeploymentAgentAPIProxyClient()
    try {
      if (editing) {
        await client.updateGoogleConnection(editing.id, { name: form.name, oauth_client_id: form.oauth_client_id, enabled: form.enabled, allow_login: form.allow_login, show_on_login: form.show_on_login, allow_user_creation: form.allow_user_creation, hosted_domains: form.hosted_domains, email_domains: form.email_domains })
        const secret = form.oauth_client_secret
        if (secret && ((secret.source === 'encrypted' && secret.value) || (secret.source === 'environment' && secret.environment !== editing.secret_environment))) await client.updateGoogleConnectionSecret(editing.id, secret)
      } else await client.createGoogleConnection(form)
      showToast(editing ? 'Google Connectionを更新しました' : 'Google Connectionを追加しました', 'success'); setOpen(false); await load()
    } catch { showToast('Google Connectionを保存できませんでした', 'error') } finally { setSaving(false) }
  }
  const input = 'mt-1 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900 dark:text-white'
  return <>
    <SettingsPageHeader title="Google Connections" description="Google OIDCログインとアカウント連携を管理します。" action={<button onClick={() => { setEditing(null); setForm(emptyForm); setOpen(true) }} className="inline-flex items-center gap-2 rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white"><Plus className="h-4 w-4" />追加</button>} />
    <div className="space-y-3">{connections.map(item => <div key={item.id} className="rounded-lg border bg-white p-5 dark:border-gray-700 dark:bg-gray-800"><div className="flex justify-between gap-4"><div className="flex gap-3"><Search className="h-5 w-5" /><div><h2 className="font-semibold dark:text-white">{item.name}</h2><p className="text-sm text-gray-500">{item.oauth_client_id}</p></div></div><div className="flex gap-2"><button onClick={async () => { try { const result = await createCurrentDeploymentAgentAPIProxyClient().testGoogleConnection(item.id); showToast(result.discovery_reachable && result.secret_resolvable ? 'Google OIDC設定を確認しました' : 'OIDC設定を確認できませんでした', result.discovery_reachable && result.secret_resolvable ? 'success' : 'error') } catch { showToast('接続テストに失敗しました', 'error') } }} className="rounded-md border px-3 py-1.5 text-sm">接続テスト</button><button onClick={() => startEdit(item)} className="rounded-md border px-3 py-1.5 text-sm">編集</button><button aria-label="削除" onClick={async () => { if (confirm(`${item.name}を削除しますか？`)) { try { await createCurrentDeploymentAgentAPIProxyClient().deleteGoogleConnection(item.id); await load() } catch { showToast('連携済みidentityがあるConnectionは削除できません', 'error') } } }} className="rounded-md border border-red-200 p-2 text-red-600"><Trash2 className="h-4 w-4" /></button></div></div><div className="mt-4 flex flex-wrap gap-2 text-xs"><span>{item.enabled ? 'Enabled' : 'Disabled'}</span><span>{item.allow_login !== false ? 'ログイン可' : '連携のみ'}</span><span>{item.allow_user_creation ? 'ユーザー作成可' : '既存ユーザーのみ'}</span><span>{item.linked_identities || 0} identities</span></div><button onClick={() => { void navigator.clipboard.writeText(`${window.location.origin}/api/v1/auth/google-connections/callback`); showToast('Callback URLをコピーしました', 'success') }} className="mt-4 inline-flex items-center gap-1 text-xs text-blue-600"><Copy className="h-3.5 w-3.5" />Callback URLをコピー</button></div>)}</div>
    {open && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"><form onSubmit={submit} className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-xl bg-white p-6 dark:bg-gray-800"><div className="mb-5 flex justify-between"><h2 className="font-semibold dark:text-white">{editing ? 'Connectionを編集' : 'Connectionを追加'}</h2><button type="button" onClick={() => setOpen(false)}><X className="h-5 w-5" /></button></div><div className="space-y-4">
      <label className="block text-sm dark:text-white">名前<input required className={input} value={form.name} onChange={e => setForm({...form, name:e.target.value})} /></label>
      <label className="block text-sm dark:text-white">OAuth Client ID<input required className={input} value={form.oauth_client_id} onChange={e => setForm({...form, oauth_client_id:e.target.value})} /></label>
      <label className="block text-sm dark:text-white">Hosted domains<input className={input} placeholder="example.com, subsidiary.example.com" value={(form.hosted_domains || []).join(', ')} onChange={e => setForm({...form, hosted_domains:domains(e.target.value)})} /></label>
      <label className="block text-sm dark:text-white">Email domains<input className={input} placeholder="example.com" value={(form.email_domains || []).join(', ')} onChange={e => setForm({...form, email_domains:domains(e.target.value)})} /></label>
      <label className="block text-sm dark:text-white">Secret保存方式<select className={input} value={form.oauth_client_secret?.source} onChange={e => setForm({...form, oauth_client_secret:{source:e.target.value as GoogleSecretSource}})}><option value="encrypted">暗号化して保存</option><option value="environment">環境変数を参照</option></select></label>
      {form.oauth_client_secret?.source === 'encrypted' ? <label className="block text-sm dark:text-white">OAuth Client Secret<input required={!editing || !editing.secret_configured} type="password" className={input} placeholder={editing?.secret_configured ? '変更時のみ入力' : ''} value={form.oauth_client_secret.value || ''} onChange={e => setForm({...form, oauth_client_secret:{source:'encrypted', value:e.target.value}})} /></label> : <label className="block text-sm dark:text-white">環境変数名<input required className={input} placeholder="GOOGLE_OAUTH_CORP_CLIENT_SECRET" value={form.oauth_client_secret?.environment || ''} onChange={e => setForm({...form, oauth_client_secret:{source:'environment', environment:e.target.value}})} /></label>}
      <label className="flex gap-2 text-sm dark:text-white"><input type="checkbox" checked={form.enabled} onChange={e => setForm({...form, enabled:e.target.checked, show_on_login:e.target.checked ? form.show_on_login : false})} />有効</label>
      <label className="flex gap-2 text-sm dark:text-white"><input type="checkbox" checked={form.allow_login} onChange={e => setForm({...form, allow_login:e.target.checked, show_on_login:e.target.checked ? form.show_on_login : false})} />ccplantへのログインを許可</label>
      <label className="flex gap-2 text-sm dark:text-white"><input type="checkbox" checked={form.show_on_login} disabled={!form.enabled || !form.allow_login} onChange={e => setForm({...form, show_on_login:e.target.checked})} />ログイン画面に表示</label>
      <label className="flex gap-2 text-sm dark:text-white"><input type="checkbox" checked={form.allow_user_creation} onChange={e => setForm({...form, allow_user_creation:e.target.checked})} />初回ログインでユーザーを作成</label>
    </div><div className="mt-6 flex justify-end gap-3"><button type="button" onClick={() => setOpen(false)} className="rounded-md border px-4 py-2 text-sm">キャンセル</button><button disabled={saving} className="rounded-md bg-blue-600 px-4 py-2 text-sm text-white">{saving ? '保存中...' : '保存'}</button></div></form></div>}
  </>
}

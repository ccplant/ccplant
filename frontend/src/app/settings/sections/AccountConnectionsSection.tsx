'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { CheckCircle, Github, Link2, Unlink } from 'lucide-react'
import { ImmediateSaveNotice, SettingsPageHeader } from '@/components/settings'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import { GitHubConnection, GitHubIdentity } from '@/types/github-connection'
import { useToast } from '@/contexts/ToastContext'

export function AccountConnectionsSection() {
  const [connections, setConnections] = useState<GitHubConnection[]>([])
  const [identities, setIdentities] = useState<GitHubIdentity[]>([])
  const [principalId, setPrincipalId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)
  const { showToast } = useToast()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const client = createCurrentDeploymentAgentAPIProxyClient()
      const [available, linked] = await Promise.all([client.listGitHubConnections(), client.listGitHubIdentities()])
      setConnections(available); setIdentities(linked.identities); setPrincipalId(linked.principal_id)
    } catch { showToast('アカウント連携を読み込めませんでした', 'error') }
    finally { setLoading(false) }
  }, [showToast])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const status = params.get('github_link')
    if (status) {
      showToast(status === 'success' ? 'GitHubアカウントを連携しました' : 'GitHubアカウントを連携できませんでした', status === 'success' ? 'success' : 'error')
      window.history.replaceState(null, '', window.location.pathname)
    }
  }, [showToast])

  const byConnection = useMemo(() => new Map(identities.map(identity => [identity.connection_id, identity])), [identities])
  const link = async (connection: GitHubConnection) => {
    setBusy(connection.id)
    try {
      const response = await createCurrentDeploymentAgentAPIProxyClient().startGitHubIdentityLink(
        connection.id,
        window.location.pathname,
        `${window.location.origin}/api/proxy/auth/github-connections/callback`,
      )
      window.location.assign(response.authorization_url)
    } catch { showToast('OAuth連携を開始できませんでした', 'error'); setBusy(null) }
  }
  const unlink = async (identity: GitHubIdentity) => {
    if (!confirm(`${identity.login}の連携を解除しますか？`)) return
    setBusy(identity.connection_id)
    try { await createCurrentDeploymentAgentAPIProxyClient().unlinkGitHubIdentity(identity.id); showToast('連携を解除しました', 'success'); await load() }
    catch { showToast('連携を解除できませんでした', 'error') }
    finally { setBusy(null) }
  }

  return <>
    <SettingsPageHeader title="アカウント連携" description="GitHub.comや社内GHESのアカウントを、同じユーザーとして連携します。" />
    <ImmediateSaveNotice />
    {principalId && <p className="mb-4 break-all text-xs text-gray-500">Principal ID: {principalId}</p>}
    {loading ? <p className="py-8 text-sm text-gray-500">読み込み中...</p> : <div className="space-y-3">
      {connections.map(connection => {
        const identity = byConnection.get(connection.id)
        return <div key={connection.id} className="flex flex-col gap-4 rounded-lg border border-gray-200 bg-white p-5 dark:border-gray-700 dark:bg-gray-800 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 gap-3"><Github className="mt-0.5 h-5 w-5 shrink-0 dark:text-white" /><div className="min-w-0"><h2 className="font-semibold dark:text-white">{connection.name}</h2><p className="truncate text-sm text-gray-500">{connection.base_url}</p>{identity && <p className="mt-2 flex items-center gap-1 text-sm text-green-700 dark:text-green-400"><CheckCircle className="h-4 w-4" />{identity.login}</p>}</div></div>
          {identity ? <button disabled={busy === connection.id} onClick={() => void unlink(identity)} className="inline-flex items-center justify-center gap-2 rounded-md border border-red-200 px-3 py-2 text-sm text-red-600 disabled:opacity-50 dark:border-red-800"><Unlink className="h-4 w-4" />連携解除</button> : <button disabled={busy === connection.id} onClick={() => void link(connection)} className="inline-flex items-center justify-center gap-2 rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"><Link2 className="h-4 w-4" />GitHubと連携</button>}
        </div>
      })}
      {!connections.length && <div className="rounded-lg border border-dashed p-8 text-center text-sm text-gray-500">利用できるGitHub Connectionがありません。管理者にお問い合わせください。</div>}
    </div>}
  </>
}

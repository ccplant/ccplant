'use client'

import { useEffect, useState } from 'react'
import { FieldGroup, FieldRow, ImmediateSaveNotice, SettingsPageHeader, TextField, ToggleSwitch } from '@/components/settings'

interface RouteResponse {
  route: {
    subdomain: string
    apiUrl: string
    enabled: boolean
  } | null
}

export function ApiRoutingSection() {
  const [subdomain, setSubdomain] = useState('')
  const [apiUrl, setApiUrl] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void fetch('/api/settings/subdomain-route', { cache: 'no-store' })
      .then(async (response) => {
        if (!response.ok) throw new Error((await response.json()).error || '設定を読み込めませんでした')
        return response.json() as Promise<RouteResponse>
      })
      .then(({ route }) => {
        if (route) {
          setSubdomain(route.subdomain)
          setApiUrl(route.apiUrl)
          setEnabled(route.enabled)
        }
      })
      .catch((loadError) => setError(loadError instanceof Error ? loadError.message : '設定を読み込めませんでした'))
      .finally(() => setLoading(false))
  }, [])

  const save = async () => {
    setSaving(true)
    setError(null)
    setMessage(null)
    try {
      const response = await fetch('/api/settings/subdomain-route', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ subdomain, apiUrl, enabled }),
      })
      const result = await response.json()
      if (!response.ok) throw new Error(result.error || '保存できませんでした')
      setSubdomain(result.route.subdomain)
      setApiUrl(result.route.apiUrl)
      setMessage('APIルーティング設定を保存しました。')
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : '保存できませんでした')
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!window.confirm('APIルーティング設定を削除しますか？')) return
    setSaving(true)
    setError(null)
    try {
      const response = await fetch('/api/settings/subdomain-route', { method: 'DELETE' })
      if (!response.ok) throw new Error('削除できませんでした')
      setSubdomain('')
      setApiUrl('')
      setEnabled(true)
      setMessage('APIルーティング設定を削除しました。')
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : '削除できませんでした')
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <SettingsPageHeader
        title="API ルーティング"
        description="このUIのサブドメインから接続するAgentAPI Proxyを指定します。設定は即時反映されます。"
      />
      <ImmediateSaveNotice />
      <FieldGroup>
        <FieldRow label="サブドメイン" htmlFor="api-route-subdomain" description="例: team-a（team-a.ui.example.com の先頭部分）">
          <TextField id="api-route-subdomain" value={subdomain} onChange={setSubdomain} placeholder="team-a" disabled={loading || saving} />
        </FieldRow>
        <FieldRow label="AgentAPI Proxy URL" htmlFor="api-route-url" description="公開HTTPS URLを指定してください。">
          <TextField id="api-route-url" value={apiUrl} onChange={setApiUrl} placeholder="https://api.example.com" disabled={loading || saving} />
        </FieldRow>
        <FieldRow label="有効" description="無効にすると管理者設定のAPIへフォールバックします。" control={
          <ToggleSwitch checked={enabled} onChange={setEnabled} disabled={loading || saving} />
        } />
      </FieldGroup>
      {error && <p className="mt-4 text-sm text-red-600 dark:text-red-400">{error}</p>}
      {message && <p className="mt-4 text-sm text-green-600 dark:text-green-400">{message}</p>}
      <div className="mt-6 flex gap-3">
        <button type="button" onClick={save} disabled={loading || saving || !subdomain || !apiUrl} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">
          {saving ? '保存中…' : '保存'}
        </button>
        <button type="button" onClick={remove} disabled={loading || saving || !subdomain} className="rounded-md border border-red-300 px-4 py-2 text-sm font-medium text-red-600 disabled:opacity-50 dark:border-red-700 dark:text-red-400">
          削除
        </button>
      </div>
    </>
  )
}


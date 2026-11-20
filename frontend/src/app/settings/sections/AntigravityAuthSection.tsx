'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { CheckCircle2, ExternalLink, FileJson2, Loader2 } from 'lucide-react'
import { ImmediateSaveNotice, SettingsPageHeader, SettingsSubsection } from '@/components/settings'
import { AntigravityAuthAttempt, createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { useToast } from '@/contexts/ToastContext'
import { useSettingsScope } from '../SettingsScopeContext'

const terminal = new Set(['authorized', 'denied', 'failed', 'cancelled'])

export function AntigravityAuthSection() {
  const { scopeKind, scopeId, credentialsMetadata, reloadCredentials } = useSettingsScope()
  const { showToast } = useToast()
  const [attempt, setAttempt] = useState<AntigravityAuthAttempt | null>(null)
  const [configured, setConfigured] = useState<boolean | null>(null)
  const [starting, setStarting] = useState(false)
  const [input, setInput] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [adcText, setAdcText] = useState('')
  const [adcError, setAdcError] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const hasSSO = !!credentialsMetadata?.files?.find(file => file.type === 'antigravity_auth' && file.has_data)
  const hasADC = !!credentialsMetadata?.files?.find(file => file.type === 'google_adc' && file.has_data)
  const target = scopeKind === 'team' ? { scope: 'team' as const, team_id: scopeId, replace: true } : { scope: 'user' as const, replace: true }

  useEffect(() => {
    const configTarget = scopeKind === 'team' ? { scope: 'team' as const, team_id: scopeId } : { scope: 'user' as const }
    createAgentAPIProxyClientFromStorage().getAntigravityAuthConfig(configTarget).then(value => setConfigured(value.configured)).catch(() => setConfigured(true))
    return () => { if (timer.current) clearTimeout(timer.current) }
  }, [scopeId, scopeKind])

  const poll = useCallback(async (attemptId: string) => {
    try {
      const next = await createAgentAPIProxyClientFromStorage().getAntigravityAuthAttempt(attemptId)
      setAttempt(next)
      if (next.status === 'authorized') {
        await reloadCredentials()
        showToast('Gemini Enterprise の認証情報を保存しました', 'success')
      }
      if (!terminal.has(next.status)) timer.current = setTimeout(() => poll(attemptId), next.poll_after_ms || 1500)
    } catch (error) {
      showToast(error instanceof Error ? error.message : '認証状態を取得できませんでした', 'error')
    }
  }, [reloadCredentials, showToast])

  const startSSO = async () => {
    setStarting(true); setAttempt(null)
    try {
      const next = await createAgentAPIProxyClientFromStorage().startAntigravitySSO(target)
      setAttempt(next); timer.current = setTimeout(() => poll(next.attempt_id), next.poll_after_ms || 1000)
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'Business SSOを開始できませんでした', 'error')
    } finally { setStarting(false) }
  }

  const submitInput = async () => {
    if (!attempt || !input.trim()) return
    setSubmitting(true)
    try {
      await createAgentAPIProxyClientFromStorage().submitAntigravityAuthInput(attempt.attempt_id, input.trim())
      setInput('')
    } catch (error) {
      showToast(error instanceof Error ? error.message : '認証コードを送信できませんでした', 'error')
    } finally { setSubmitting(false) }
  }

  const submitAction = async (action: 'up' | 'down' | 'enter') => {
    if (!attempt) return
    setSubmitting(true)
    try {
      await createAgentAPIProxyClientFromStorage().submitAntigravityAuthInput(attempt.attempt_id, undefined, action)
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'サブスクリプションを選択できませんでした', 'error')
    } finally { setSubmitting(false) }
  }

  const selectingSubscription = attempt?.transcript?.includes('Select available subscription') || attempt?.transcript?.includes("Can't find your subscription")

  const readADC = (file: File) => {
    const reader = new FileReader()
    reader.onload = () => validateADC(String(reader.result || ''))
    reader.readAsText(file)
  }

  const validateADC = (text: string) => {
    setAdcText(text)
    try {
      const value = JSON.parse(text) as Record<string, unknown>
      if (!value.type) throw new Error('type がありません')
      setAdcError(null)
    } catch { setAdcError('有効な Application Default Credentials JSON を選択してください') }
  }

  const uploadADC = async () => {
    if (!adcText || adcError) return
    setUploading(true)
    try {
      await createAgentAPIProxyClientFromStorage().uploadCredentials(scopeId, JSON.parse(adcText), 'google_adc')
      setAdcText(''); await reloadCredentials(); showToast('ADC JSONを保存しました', 'success')
    } catch (error) {
      showToast(error instanceof Error ? error.message : 'ADC JSONを保存できませんでした', 'error')
    } finally { setUploading(false) }
  }

  return <>
    <SettingsPageHeader title="Antigravity 認証" description="Gemini Enterprise のBusiness SSO、またはGoogle Cloud ADCでAntigravityを認証します。認証情報はセッション開始時に自動配置されます。" />
    <ImmediateSaveNotice />

    <SettingsSubsection title="Gemini Enterprise Business SSO" description="組織アカウントでログインし、割り当てられたEnterpriseライセンスを使用します。">
      <div className="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800/40">
        <div className="flex items-start justify-between gap-4">
          <p className="text-sm text-gray-600 dark:text-gray-300">ブラウザ認証後に表示されるコード、またはリダイレクト先URLをここへ貼り付けます。</p>
          {hasSSO && <span className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-green-700 dark:text-green-400"><CheckCircle2 className="h-4 w-4" />設定済み</span>}
        </div>
        {!attempt && <button type="button" onClick={startSSO} disabled={starting || configured === false} className="mt-4 inline-flex items-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">
          {starting && <Loader2 className="h-4 w-4 animate-spin" />}{hasSSO ? 'Business SSOをやり直す' : 'Business SSOでログイン'}
        </button>}
        {attempt && !terminal.has(attempt.status) && <div className="mt-4 space-y-3 border-t border-gray-200 pt-4 dark:border-gray-700">
          {attempt.verification_uri && <a href={attempt.verification_uri} target="_blank" rel="noreferrer" className="inline-flex items-center gap-2 text-sm font-medium text-blue-600 hover:underline dark:text-blue-400">Googleの認証画面を開く<ExternalLink className="h-4 w-4" /></a>}
          {attempt.transcript && <pre className="max-h-44 overflow-auto whitespace-pre-wrap rounded-md bg-gray-950 p-3 text-xs leading-5 text-gray-200">{attempt.transcript}</pre>}
          {selectingSubscription && <div className="flex flex-wrap items-center gap-2">
            <span className="mr-1 text-sm text-gray-600 dark:text-gray-300">利用するサブスクリプションを選択:</span>
            <button type="button" onClick={() => void submitAction('up')} disabled={submitting} className="rounded-md border border-gray-300 px-3 py-2 text-sm dark:border-gray-600">↑</button>
            <button type="button" onClick={() => void submitAction('down')} disabled={submitting} className="rounded-md border border-gray-300 px-3 py-2 text-sm dark:border-gray-600">↓</button>
            <button type="button" onClick={() => void submitAction('enter')} disabled={submitting} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">選択する</button>
          </div>}
          <div className="flex gap-2">
            <input aria-label="認証コードまたは応答" value={input} onChange={event => setInput(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') void submitInput() }} placeholder="認証コード、リダイレクトURL、または選択肢" className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" />
            <button type="button" onClick={submitInput} disabled={!input.trim() || submitting} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">送信</button>
          </div>
        </div>}
        {attempt && terminal.has(attempt.status) && <div className={`mt-4 rounded-md px-3 py-2 text-sm ${attempt.status === 'authorized' ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300' : 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'}`}>{attempt.status === 'authorized' ? 'Business SSOが完了しました。' : <><p>認証を完了できませんでした。もう一度お試しください。</p>{attempt.error_message && <pre className="mt-2 whitespace-pre-wrap text-xs">{attempt.error_message}</pre>}{attempt.error_code && <p className="mt-1 text-xs opacity-75">エラーコード: {attempt.error_code}</p>}</>}</div>}
      </div>
    </SettingsSubsection>

    <SettingsSubsection title="Application Default Credentials" description="ヘッドレス実行用の application_default_credentials.json を登録します。AntigravityセッションではADCモードが自動で有効になります。">
      <div className="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800/40">
        <div className="flex items-center justify-between gap-4"><p className="text-sm text-gray-600 dark:text-gray-300">Enterpriseライセンスに紐づくGoogle Cloudプロジェクトで生成したADCを使用してください。</p>{hasADC && <span className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-green-700 dark:text-green-400"><CheckCircle2 className="h-4 w-4" />設定済み</span>}</div>
        <label className="mt-4 flex cursor-pointer items-center gap-2 rounded-md border border-dashed border-gray-300 px-3 py-3 text-sm text-gray-600 hover:border-blue-400 dark:border-gray-600 dark:text-gray-300"><FileJson2 className="h-5 w-5" /><span>ADC JSONを選択</span><input type="file" accept="application/json,.json" className="sr-only" onChange={event => { const file = event.target.files?.[0]; if (file) readADC(file); event.target.value = '' }} /></label>
        {adcError && <p className="mt-2 text-xs text-red-600 dark:text-red-400">{adcError}</p>}
        {adcText && !adcError && <button type="button" onClick={uploadADC} disabled={uploading} className="mt-3 inline-flex items-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">{uploading && <Loader2 className="h-4 w-4 animate-spin" />}ADC JSONを保存</button>}
      </div>
    </SettingsSubsection>
  </>
}

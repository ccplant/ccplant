'use client'

import { useState } from 'react'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { TriggerWebhookRequest, TriggerWebhookResponse, Webhook } from '../../types/webhook'

interface WebhookTriggerTestProps { webhook: Webhook }

function useWebhookTriggerTest(webhook: Webhook) {
  const [payloadJson, setPayloadJson] = useState('')
  const [eventType, setEventType] = useState('')
  const [dryRun, setDryRun] = useState(true)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [result, setResult] = useState<TriggerWebhookResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setIsSubmitting(true); setError(null); setResult(null)
    try {
      const request: TriggerWebhookRequest = { payload: parsePayload(payloadJson), dry_run: dryRun }
      if (webhook.type === 'github') {
        if (!eventType) throw new Error('Event type is required for GitHub webhooks')
        request.event = eventType
      }
      setResult(await createAgentAPIProxyClientFromStorage().triggerWebhook(webhook.id, request))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Unknown error occurred')
    } finally { setIsSubmitting(false) }
  }
  const loadExample = () => {
    if (webhook.type === 'github') setEventType('pull_request')
    setPayloadJson(JSON.stringify(webhook.type === 'github' ? githubExample : customExample, null, 2))
  }
  return { payloadJson, setPayloadJson, eventType, setEventType, dryRun, setDryRun, isSubmitting, result, error, submit, loadExample }
}

function parsePayload(value: string): Record<string, unknown> {
  try { return JSON.parse(value || '{}') }
  catch (reason) { throw new Error('Invalid JSON payload: ' + (reason instanceof Error ? reason.message : 'Unknown error')) }
}

const githubExample = {
  action: 'opened', number: 1,
  pull_request: { number: 1, title: 'Test PR', state: 'open', draft: false, base: { ref: 'main' }, head: { ref: 'feature-branch' } },
  repository: { full_name: 'owner/repo' }, sender: { login: 'testuser' },
}
const customExample = { event: { type: 'deploy' }, data: { status: 'success', environment: 'production' } }

export default function WebhookTriggerTest({ webhook }: WebhookTriggerTestProps) {
  const state = useWebhookTriggerTest(webhook)
  return <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-6">
    <div className="flex items-center justify-between mb-4">
      <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Webhook Trigger Test</h2>
      <button type="button" onClick={state.loadExample} className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300">Load Example</button>
    </div>
    <TriggerForm webhook={webhook} state={state} />
    {state.error && <ErrorResult error={state.error} />}
    {state.result && <TriggerResult result={state.result} />}
  </div>
}

type TriggerState = ReturnType<typeof useWebhookTriggerTest>

function TriggerForm({ webhook, state }: { webhook: Webhook; state: TriggerState }) {
  return <form onSubmit={state.submit} className="space-y-4">
    {webhook.type === 'github' && <EventTypeField value={state.eventType} onChange={state.setEventType} />}
    <div>
      <label htmlFor="payloadJson" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Payload JSON <span className="text-red-500">*</span></label>
      <textarea id="payloadJson" value={state.payloadJson} onChange={(event) => state.setPayloadJson(event.target.value)} placeholder='{"action": "opened", "repository": {"full_name": "owner/repo"}}' rows={10} className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" required />
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">JSON payload to test with</p>
    </div>
    <div className="flex items-center">
      <input type="checkbox" id="dryRun" checked={state.dryRun} onChange={(event) => state.setDryRun(event.target.checked)} className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded" />
      <label htmlFor="dryRun" className="ml-2 text-sm text-gray-700 dark:text-gray-300">Dry Run (test trigger matching without creating a session)</label>
    </div>
    <button type="submit" disabled={state.isSubmitting} className="px-6 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 text-white font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2">{state.isSubmitting ? <LoadingLabel /> : 'Test Trigger'}</button>
  </form>
}

function EventTypeField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return <div>
    <label htmlFor="eventType" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Event Type <span className="text-red-500">*</span></label>
    <input type="text" id="eventType" value={value} onChange={(event) => onChange(event.target.value)} placeholder="e.g., pull_request, push, issues" className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500" required />
    <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">GitHub event type (e.g., pull_request, push, issues)</p>
  </div>
}

function LoadingLabel() {
  return <span className="flex items-center"><svg className="animate-spin -ml-1 mr-2 h-4 w-4 text-white" fill="none" viewBox="0 0 24 24"><circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" /><path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" /></svg>Testing...</span>
}

function ErrorResult({ error }: { error: string }) {
  return <div className="mt-4 p-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg"><div className="flex">
    <StatusIcon kind="error" />
    <div className="ml-3"><h3 className="text-sm font-medium text-red-800 dark:text-red-200">Error</h3><p className="mt-1 text-sm text-red-700 dark:text-red-300">{error}</p></div>
  </div></div>
}

function TriggerResult({ result }: { result: TriggerWebhookResponse }) {
  const colors = result.matched ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' : 'bg-yellow-50 dark:bg-yellow-900/20 border-yellow-200 dark:border-yellow-800'
  return <div className={`mt-4 p-4 border rounded-lg ${colors}`}><div className="flex items-start">
    <StatusIcon kind={result.matched ? 'success' : 'warning'} />
    <div className="ml-3 flex-1">
      <h3 className={`text-sm font-medium ${result.matched ? 'text-green-800 dark:text-green-200' : 'text-yellow-800 dark:text-yellow-200'}`}>{result.matched ? 'Trigger Matched!' : 'No Trigger Matched'}</h3>
      {result.error && <p className="mt-2 text-sm text-red-700 dark:text-red-300">{result.error}</p>}
      {result.matched && result.matched_trigger && <MatchedDetails result={result} />}
    </div>
  </div></div>
}

function StatusIcon({ kind }: { kind: 'error' | 'success' | 'warning' }) {
  const styles = { error: 'text-red-600 dark:text-red-400', success: 'text-green-600 dark:text-green-400 mt-0.5', warning: 'text-yellow-600 dark:text-yellow-400 mt-0.5' }
  const paths = {
    error: 'M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z',
    success: 'M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z',
    warning: 'M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z',
  }
  return <svg className={`h-5 w-5 ${styles[kind]}`} fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d={paths[kind]} clipRule="evenodd" /></svg>
}

function MatchedDetails({ result }: { result: TriggerWebhookResponse }) {
  return <div className="mt-2 space-y-2">
    <div><p className="text-sm font-medium text-gray-700 dark:text-gray-300">Matched Trigger:</p><p className="text-sm text-gray-600 dark:text-gray-400">{result.matched_trigger!.name} (ID: {result.matched_trigger!.id})</p></div>
    {result.session_id && <div><p className="text-sm font-medium text-gray-700 dark:text-gray-300">Session Created:</p><a href={`/agentapi?session=${result.session_id}`} className="text-sm text-blue-600 dark:text-blue-400 hover:underline">{result.session_id}</a>{result.session_reused && <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">(reused)</span>}</div>}
    {result.initial_message && <LabeledCode label="Initial Message" value={result.initial_message} />}
    {result.tags && Object.keys(result.tags).length > 0 && <TagList tags={result.tags} />}
    {result.environment && Object.keys(result.environment).length > 0 && <LabeledCode label="Environment" value={JSON.stringify(result.environment, null, 2)} />}
  </div>
}

function LabeledCode({ label, value }: { label: string; value: string }) {
  return <div><p className="text-sm font-medium text-gray-700 dark:text-gray-300">{label}:</p><pre className="mt-1 text-sm text-gray-600 dark:text-gray-400 bg-gray-100 dark:bg-gray-700 p-2 rounded overflow-x-auto">{value}</pre></div>
}

function TagList({ tags }: { tags: Record<string, string> }) {
  return <div><p className="text-sm font-medium text-gray-700 dark:text-gray-300">Tags:</p><div className="mt-1 flex flex-wrap gap-2">{Object.entries(tags).map(([key, value]) => <span key={key} className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-800 text-blue-800 dark:text-blue-200 rounded">{key}: {value}</span>)}</div></div>
}

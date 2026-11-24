import { useCallback, useEffect, useRef, useState } from 'react'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'

export type AuthPhase = 'idle' | 'polling' | 'authorized' | 'denied' | 'error'
export interface DeviceAuthState { userCode: string; verificationUri: string }
interface Options { scope: 'user' | 'team'; teamId?: string; onAuthComplete?: () => void }
const POLL_INTERVAL_MS = 3000

export function useCodexDeviceAuth({ scope, teamId, onAuthComplete }: Options) {
  const [phase, setPhase] = useState<AuthPhase>('idle')
  const [starting, setStarting] = useState(false)
  const [deviceAuth, setDeviceAuth] = useState<DeviceAuthState | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [configured, setConfigured] = useState<boolean | null>(null)
  const [copied, setCopied] = useState(false)
  const pollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const attemptIdRef = useRef<string | null>(null)

  useEffect(() => { createAgentAPIProxyClientFromStorage().getCodexDeviceAuthConfig().then((config) => setConfigured(config.configured)).catch(() => setConfigured(true)) }, [])
  const clearPollTimer = () => { if (pollTimerRef.current) { clearTimeout(pollTimerRef.current); pollTimerRef.current = null } }
  const poll = useCallback(async () => {
    try {
      const attemptId = attemptIdRef.current
      if (!attemptId) return
      const result = await createAgentAPIProxyClientFromStorage().getCodexDeviceAuthAttempt(attemptId)
      if (result.status === 'waiting_for_user' && result.user_code && result.verification_uri) setDeviceAuth({ userCode: result.user_code, verificationUri: result.verification_uri })
      if (result.status === 'authorized') { setPhase('authorized'); setDeviceAuth(null); onAuthComplete?.(); return }
      if (['denied', 'failed', 'cancelled'].includes(result.status)) { setPhase('denied'); setDeviceAuth(null); return }
      pollTimerRef.current = setTimeout(poll, POLL_INTERVAL_MS)
    } catch (err) { setError(err instanceof Error ? err.message : 'ポーリングエラーが発生しました'); setPhase('error') }
  }, [onAuthComplete])
  useEffect(() => () => clearPollTimer(), [])

  const start = async () => {
    setStarting(true); setError(null); setDeviceAuth(null)
    try {
      const result = await createAgentAPIProxyClientFromStorage().startCodexDeviceAuth(scope === 'team' ? { scope, team_id: teamId, replace: true } : { scope, replace: true })
      attemptIdRef.current = result.attempt_id
      if (result.user_code && result.verification_uri) setDeviceAuth({ userCode: result.user_code, verificationUri: result.verification_uri })
      setPhase('polling'); pollTimerRef.current = setTimeout(poll, POLL_INTERVAL_MS)
    } catch (err) { setError(err instanceof Error ? err.message : '認証フローの開始に失敗しました'); setPhase('error') }
    finally { setStarting(false) }
  }
  const cancel = async () => {
    clearPollTimer(); const attemptId = attemptIdRef.current; attemptIdRef.current = null
    if (attemptId) try { await createAgentAPIProxyClientFromStorage().cancelCodexDeviceAuth(attemptId) } catch (err) { console.debug('Best-effort Codex device auth cancellation failed:', err) }
    setPhase('idle'); setDeviceAuth(null); setError(null)
  }
  const copyCode = async () => {
    if (!deviceAuth) return
    try { await navigator.clipboard.writeText(deviceAuth.userCode); setCopied(true); setTimeout(() => setCopied(false), 2000) }
    catch (err) { console.warn('Clipboard is unavailable for the Codex device code:', err) }
  }
  return { phase, starting, deviceAuth, error, configured, copied, start, cancel, copyCode }
}

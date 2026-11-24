import { useCallback, useEffect, useState } from 'react'
import { ApiTokenPermission, ApiTokenScope, CreateApiTokenRequest, CreateApiTokenResponse } from '../../types/api_token'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'

interface Options { isOpen: boolean; scope: ApiTokenScope; teamId?: string; onClose: () => void; onCreated: (response: CreateApiTokenResponse) => void }
export const API_TOKEN_NAME_MAX_LENGTH = 64

export function useApiTokenCreate({ isOpen, scope, teamId, onClose, onCreated }: Options) {
  const [name, setName] = useState('')
  const [permissions, setPermissions] = useState<string[]>([])
  const [expiryDays, setExpiryDays] = useState<number | null>(30)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const reset = useCallback(() => { setName(''); setPermissions([]); setExpiryDays(30); setError(null) }, [])
  useEffect(() => { if (isOpen) reset() }, [isOpen, reset])
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape' && isOpen && !isSubmitting) onClose() }
    if (isOpen) document.addEventListener('keydown', close)
    return () => document.removeEventListener('keydown', close)
  }, [isOpen, isSubmitting, onClose])
  const togglePermission = (permission: ApiTokenPermission) => setPermissions((current) => current.includes(permission) ? current.filter((item) => item !== permission) : [...current, permission])
  const trimmedName = name.trim()
  const valid = trimmedName.length >= 1 && trimmedName.length <= API_TOKEN_NAME_MAX_LENGTH
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!valid) { setError('トークン名は1〜64文字で入力してください'); return }
    if (scope === 'team' && !teamId) { setError('チームが選択されていません'); return }
    let expiresAt: string | null = null
    if (expiryDays !== null) {
      const expiry = new Date()
      expiry.setDate(expiry.getDate() + expiryDays)
      expiresAt = expiry.toISOString()
    }
    const payload: CreateApiTokenRequest = { name: trimmedName, scope, team_id: scope === 'team' ? teamId : undefined, permissions: permissions.length ? permissions : undefined, expires_at: expiresAt }
    try {
      setIsSubmitting(true); setError(null)
      const response = await createAgentAPIProxyClientFromStorage().createApiToken(payload)
      reset(); onCreated(response)
    } catch (err) { setError(err instanceof Error ? err.message : 'トークンの作成に失敗しました') }
    finally { setIsSubmitting(false) }
  }
  return { name, setName, permissions, togglePermission, expiryDays, setExpiryDays, isSubmitting, error, trimmedName, canSubmit: valid && !isSubmitting, submit }
}
export type ApiTokenCreateState = ReturnType<typeof useApiTokenCreate>

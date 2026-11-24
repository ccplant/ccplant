'use client'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { SettingsData, ModelConnection, prepareSettingsForSave } from '@/types/settings'
import { AgentAPIProxyError, CredentialsMetadata, createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { useToast } from '@/contexts/ToastContext'
import { SettingsScopeKind } from './navConfig'
import type { SettingsScopeValue, SettingsUpdate } from './SettingsScopeContext'

const collectDirtyFields = (
  current: SettingsData,
  original: SettingsData
): (keyof SettingsData)[] => {
  const keys = new Set<keyof SettingsData>([
    ...(Object.keys(current) as (keyof SettingsData)[]),
    ...(Object.keys(original) as (keyof SettingsData)[]),
  ])
  return Array.from(keys).filter(
    (key) => JSON.stringify(current[key]) !== JSON.stringify(original[key])
  )
}

export function useSettingsScopeValue(scopeKind: SettingsScopeKind, teamId?: string): SettingsScopeValue {
  const [settings, setSettings] = useState<SettingsData>({})
  const [originalSettings, setOriginalSettings] = useState<SettingsData>({})
  const [principalId, setPrincipalId] = useState('')
  const [userName, setUserName] = useState('')
  const [userTeams, setUserTeams] = useState<string[]>([])
  const [userTeamNames, setUserTeamNames] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [ready, setReady] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isAuthError, setIsAuthError] = useState(false)
  const [credentialsMetadata, setCredentialsMetadata] = useState<CredentialsMetadata | null>(null)
  const [revealedTokens, setRevealedTokens] = useState<Record<string, string>>({})
  const [regeneratingEsmId, setRegeneratingEsmId] = useState<string | null>(null)
  const { showToast } = useToast()

  const scopeId = scopeKind === 'personal' ? principalId : (teamId ?? '')
  const setUserTeamName = useCallback((id: string, name: string) => {
    setUserTeamNames((current) => ({ ...current, [id]: name }))
  }, [])

  const dirtyFields = useMemo(
    () => collectDirtyFields(settings, originalSettings),
    [settings, originalSettings]
  )
  const dirty = dirtyFields.length > 0

  // ページ離脱時の警告
  const dirtyRef = useRef(false)
  useEffect(() => {
    dirtyRef.current = dirty
  }, [dirty])

  useEffect(() => {
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      if (dirtyRef.current) {
        e.preventDefault()
        e.returnValue = ''
      }
    }
    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => window.removeEventListener('beforeunload', handleBeforeUnload)
  }, [])

  // ユーザー情報の取得
  useEffect(() => {
    let cancelled = false
    const loadUserInfo = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const info = await client.getUserInfo()
        if (cancelled) return
        if (info?.principal_id && info?.username) {
          setPrincipalId(info.principal_id)
          setUserName(info.username)
          setUserTeams(info.teams || [])
          setUserTeamNames(Object.fromEntries(
            (info.team_principals || []).map((team) => [team.team_id, team.name || team.team_id])
          ))
        } else {
          setError('principal ID を含むユーザー情報の取得に失敗しました')
          setLoading(false)
        }
      } catch (err) {
        if (cancelled) return
        console.error('Failed to get user info:', err)
        if (err instanceof AgentAPIProxyError && err.status === 401) {
          setIsAuthError(true)
          setError('認証が必要です。ログアウトして再度ログインしてください。')
        } else {
          setError('ユーザー情報の取得に失敗しました')
        }
        setLoading(false)
      }
    }
    loadUserInfo()
    return () => {
      cancelled = true
    }
  }, [])

  // スコープの設定を読み込む
  useEffect(() => {
    if (!scopeId) return
    let cancelled = false

    const fetchSettings = async () => {
      setLoading(true)
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const data = await client.getSettings(scopeId)
        if (cancelled) return
        setSettings(data)
        setOriginalSettings(data)
        setReady(true)

        try {
          const meta = await client.getCredentials(scopeId)
          if (!cancelled) setCredentialsMetadata(meta)
        } catch (credentialsError) {
          console.debug('Credentials metadata is unavailable for this settings scope:', credentialsError)
        }
      } catch (err) {
        if (cancelled) return
        console.error('Failed to load settings:', err)
        if (err instanceof AgentAPIProxyError && err.status === 401) {
          setIsAuthError(true)
          setError('認証が必要です。ログアウトして再度ログインしてください。')
        } else if (scopeKind === 'team') {
          setError('チーム設定を読み込めませんでした。チームが存在しないか、権限がない可能性があります。')
        } else {
          setError('設定の読み込みに失敗しました')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    fetchSettings()
    return () => {
      cancelled = true
    }
  }, [scopeId, scopeKind])

  const update = useCallback((partial: SettingsUpdate) => {
    setSettings((prev) => ({
      ...prev,
      ...(typeof partial === 'function' ? partial(prev) : partial),
    }))
  }, [])

  const discard = useCallback(() => {
    setSettings(originalSettings)
  }, [originalSettings])

  const save = useCallback(async () => {
    if (!scopeId) return
    setSaving(true)
    setError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const saved = await client.saveSettings(scopeId, prepareSettingsForSave(settings))
      setSettings(saved)
      setOriginalSettings(saved)

      // 保存レスポンスに一度だけ含まれる接続トークンを取りこぼさない
      const tokens: Record<string, string> = {}
      for (const manager of saved?.external_session_managers ?? []) {
        if (manager.id && manager.connection_token) {
          tokens[manager.id] = manager.connection_token
        }
      }
      if (Object.keys(tokens).length > 0) {
        setRevealedTokens((prev) => ({ ...prev, ...tokens }))
        showToast('接続トークンを発行しました。必要な値をコピーしてください。', 'success')
      } else {
        showToast('設定を保存しました', 'success')
      }
    } catch (err) {
      console.error('Failed to save settings:', err)
      setError('設定の保存に失敗しました')
      showToast('設定の保存に失敗しました', 'error')
    } finally {
      setSaving(false)
    }
  }, [scopeId, settings, showToast])

  const saveConnection = useCallback(async (agent: 'codex' | 'claude', connection: ModelConnection) => {
    if (!scopeId || !ready) throw new Error('設定の読み込みが完了していません')
    if (saving) throw new Error('設定の保存が完了するまでお待ちください')
    setSaving(true)
    try {
      const field = agent === 'codex' ? 'codex_connection' : 'claude_connection'
      const client = createAgentAPIProxyClientFromStorage()
      const saved = await client.saveSettings(scopeId, { [field]: connection })
      const patch: Partial<SettingsData> = { [field]: saved[field], ...(agent === 'claude' ? { auth_mode: saved.auth_mode } : {}) }
      setSettings(prev => ({ ...prev, ...patch }))
      setOriginalSettings(prev => ({ ...prev, ...patch }))
    } finally {
      setSaving(false)
    }
  }, [scopeId, ready, saving])

  const reloadCredentials = useCallback(async () => {
    if (!scopeId) return
    try {
      const client = createAgentAPIProxyClientFromStorage()
      setCredentialsMetadata(await client.getCredentials(scopeId))
    } catch (credentialsError) {
      console.debug('Failed to reload credentials metadata; preserving current display:', credentialsError)
    }
  }, [scopeId])

  const clearCredentialsMetadata = useCallback(() => {
    setCredentialsMetadata(null)
  }, [])

  const regenerateEsmToken = useCallback(
    async (esmId: string) => {
      setRegeneratingEsmId(esmId)
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const rotated = await client.rotateExternalSessionManagerToken(esmId)
        setSettings((prev) => ({
          ...prev,
          external_session_managers: (prev.external_session_managers ?? []).map((manager) =>
            manager.id === esmId ? { ...manager, has_connection_token: true } : manager
          ),
        }))
        setOriginalSettings((prev) => ({
          ...prev,
          external_session_managers: (prev.external_session_managers ?? []).map((manager) =>
            manager.id === esmId ? { ...manager, has_connection_token: true } : manager
          ),
        }))
        setRevealedTokens((prev) => ({ ...prev, [esmId]: rotated.connection_token || '' }))
        showToast('接続トークンを再発行しました', 'success')
      } catch (err) {
        console.error('Failed to regenerate connection token:', err)
        showToast('接続トークンの再発行に失敗しました', 'error')
      } finally {
        setRegeneratingEsmId(null)
      }
    },
    [showToast]
  )

  const value: SettingsScopeValue = {
    scopeKind,
    scopeId,
    principalId,
    userName,
    userTeams,
    userTeamNames,
    setUserTeamName,
    settings,
    update,
    save,
    discard,
    dirty,
    saveConnection,
    dirtyFields,
    loading,
    ready,
    saving,
    error,
    isAuthError,
    credentialsMetadata,
    reloadCredentials,
    clearCredentialsMetadata,
    revealedTokens,
    regenerateEsmToken,
    regeneratingEsmId,
  }


  return value
}

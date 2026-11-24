'use client'

import { createContext, useContext } from 'react'
import { SettingsData, ModelConnection } from '@/types/settings'
import { CredentialsMetadata } from '@/lib/agentapi-proxy-client'
import { SettingsScopeKind } from './navConfig'
import { useSettingsScopeValue } from './useSettingsScopeValue'

/** 直前の状態から差分を組み立てたい場合は関数を渡す */
export type SettingsUpdate = Partial<SettingsData> | ((prev: SettingsData) => Partial<SettingsData>)

export interface SettingsScopeValue {
  scopeKind: SettingsScopeKind
  /** 設定 API に渡す所有者ID。personal は principal ID、team はチーム名 */
  scopeId: string
  /** ログイン中のユーザーの不変 principal ID */
  principalId: string
  /** ログイン中のユーザー名。scopeKind に関わらず常に自分自身 */
  userName: string
  userTeams: string[]
  userTeamNames: Record<string, string>
  setUserTeamName: (teamId: string, name: string) => void

  settings: SettingsData
  update: (partial: SettingsUpdate) => void
  save: () => Promise<void>
 saveConnection: (agent: 'codex' | 'claude', connection: ModelConnection) => Promise<void>
  discard: () => void
  dirty: boolean
  dirtyFields: (keyof SettingsData)[]

  loading: boolean
  /** 設定の読み込みに成功して編集できる状態か */
  ready: boolean
  saving: boolean
  error: string | null
  isAuthError: boolean

  /** Codex 認証ファイルのメタデータ。専用 API で即時反映される */
  credentialsMetadata: CredentialsMetadata | null
  reloadCredentials: () => Promise<void>
  clearCredentialsMetadata: () => void

  /**
   * 保存レスポンスに一度だけ含まれる External Session Manager の接続トークン。
   * ページを移動しても失われないよう Provider が保持する。
   */
  revealedTokens: Record<string, string>
  regenerateEsmToken: (esmId: string) => Promise<void>
  regeneratingEsmId: string | null
}

const SettingsScopeContext = createContext<SettingsScopeValue | null>(null)

export const useSettingsScope = (): SettingsScopeValue => {
  const context = useContext(SettingsScopeContext)
  if (!context) throw new Error('useSettingsScope must be used within a SettingsScopeProvider')
  return context
}

interface SettingsScopeProviderProps {
  scopeKind: SettingsScopeKind
  teamId?: string
  children: React.ReactNode
}

export function SettingsScopeProvider({ scopeKind, teamId, children }: SettingsScopeProviderProps) {
  const value = useSettingsScopeValue(scopeKind, teamId)
  return <SettingsScopeContext.Provider value={value}>{children}</SettingsScopeContext.Provider>
}

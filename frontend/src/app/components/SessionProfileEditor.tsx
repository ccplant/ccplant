'use client'

import {
  ArrowLeft,
  PanelLeft,
  X,
  Settings,
  KeyRound,
  Bot,
  Server,
  Terminal,
  Tags,
  Shield,
  Container,
  Clock,
  Files,
  LockKeyhole,
  Sparkles,
} from 'lucide-react'
import { SideNav } from '@/components/settings/ui/SideNav'
import { SettingsPageHeader } from '@/components/settings/ui/SettingsPageHeader'

import type { SessionProfile } from '../../types/session_profile'

export interface SessionProfileEditorProps {
  onClose: () => void
  onSuccess: (savedProfile?: SessionProfile) => void
  editingProfile?: SessionProfile | null
  section?: string
  createScope?: { scope: 'user' | 'team'; team_id?: string }
}

const profileSections = [
  { slug: 'basic', label: '基本情報', icon: Settings, group: '' },
  { slug: 'inheritance', label: 'ベース設定', icon: Settings, group: '' },
  { slug: 'authentication', label: '認証・API 接続', icon: KeyRound, group: 'AI とエージェント' },
  { slug: 'agent', label: 'エージェント', icon: Bot, group: 'AI とエージェント' },
  { slug: 'system-prompt', label: 'システムプロンプト', icon: Bot, group: 'AI とエージェント' },
  { slug: 'models', label: 'モデル', icon: Bot, group: 'AI とエージェント' },
  { slug: 'mcp', label: 'MCP サーバー', icon: Server, group: 'セッション環境' },
  { slug: 'skills', label: 'Skills', icon: Sparkles, group: 'セッション環境' },
  { slug: 'environment', label: '環境変数', icon: Terminal, group: 'セッション環境' },
  { slug: 'secrets', label: 'シークレット', icon: LockKeyhole, group: 'セッション環境' },
  { slug: 'files', label: 'セッションファイル', icon: Files, group: 'セッション環境' },
  { slug: 'storages', label: 'Storage', icon: Files, group: 'セッション環境' },
  { slug: 'tags', label: 'タグ', icon: Tags, group: 'セッション環境' },
  { slug: 'pool', label: 'プール', icon: Server, group: '実行基盤' },
  { slug: 'runtime', label: 'ランタイム', icon: Terminal, group: '実行基盤' },
  { slug: 'sandbox', label: 'ネットワーク制限', icon: Shield, group: '実行基盤' },
  { slug: 'docker', label: 'Docker', icon: Container, group: '実行基盤' },
  { slug: 'lifecycle', label: '自動削除', icon: Clock, group: '実行基盤' },
]

import { ProfileBasicSection } from './ProfileBasicSection'
import { ProfileStoragesSection } from './ProfileStoragesSection'
import { ProfileInheritanceSection } from './ProfileInheritanceSection'
import { ProfileAuthenticationSection } from './ProfileAuthenticationSection'
import { ProfileAgentSection } from './ProfileAgentSection'
import { ProfileModelsSection } from './ProfileModelsSection'
import { ProfileMcpSection } from './ProfileMcpSection'
import { ProfileSkillsSection } from './ProfileSkillsSection'
import { ProfileEnvironmentSection } from './ProfileEnvironmentSection'
import { ProfileSystemPromptSection } from './ProfileSystemPromptSection'
import { ProfileSecretsSection } from './ProfileSecretsSection'
import { ProfileFilesSection } from './ProfileFilesSection'
import { ProfileTagsSection } from './ProfileTagsSection'
import { ProfilePoolSection } from './ProfilePoolSection'
import { ProfileRuntimeSection } from './ProfileRuntimeSection'
import { ProfileSandboxSection } from './ProfileSandboxSection'
import { ProfileDockerSection } from './ProfileDockerSection'
import { ProfileLifecycleSection } from './ProfileLifecycleSection'
import { useSessionProfileEditor } from './useSessionProfileEditor'
export default function SessionProfileEditor({
  onClose,
  onSuccess,
  editingProfile,
  section = 'basic',
  createScope,
}: SessionProfileEditorProps) {
  const state = useSessionProfileEditor({ onClose, onSuccess, editingProfile, section, createScope })
  const {
    pathname,
    searchParams,
    name,
    isEditing,
    scope,
    teamId,
    dirty,
    drawerOpen,
    setDrawerOpen,
    handleSubmit,
    setDirty,
    settingsTeamId,
    error,
    isSubmitting,
    setResetKey,
  } = state

  const active = profileSections.find((item) => item.slug === section) ?? profileSections[0]
  const hrefFor = (slug: string) => {
    const query = new URLSearchParams(searchParams.toString())
    query.set('section', slug)
    return `${pathname}?${query}`
  }
  const groups = [...new Set(profileSections.map((item) => item.group))].map((title) => ({
    title,
    items: profileSections
      .filter((item) => item.group === title)
      .map((item) => ({ ...item, href: hrefFor(item.slug) })),
  }))
  const leave = () => {
    if (!dirty || window.confirm('未保存の変更があります。破棄して移動しますか？')) onClose()
  }
  const nav = (
    <SideNav
      groups={groups}
      activeHref={hrefFor(active.slug)}
      ariaLabel="セッションプロファイルの設定"
      header={
        <div className="px-3 py-2">
          <p className="text-xs text-gray-500">
            {isEditing ? 'セッションプロファイル' : '新しいプロファイル'} · {scope === 'team' ? teamId : '個人'}
          </p>
          <p className="truncate font-semibold">{name || '名前未設定'}</p>
        </div>
      }
    />
  )
  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950 text-gray-900 dark:text-white">
      <div className="container mx-auto max-w-6xl px-4 py-6">
        <div className="mb-6 flex items-center gap-3">
          <button
            type="button"
            className="md:hidden p-2"
            aria-label="設定項目を開く"
            onClick={() => setDrawerOpen(true)}
          >
            <PanelLeft className="h-5 w-5" />
          </button>
          <button
            type="button"
            onClick={leave}
            className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-900 dark:hover:text-white"
          >
            <ArrowLeft className="h-4 w-4" />
            プロファイル一覧に戻る
          </button>
        </div>
        <div className="flex gap-8">
          <aside className="hidden md:block">{nav}</aside>
          <form onSubmit={handleSubmit} onChange={() => setDirty(true)} className="min-w-0 flex-1">
            <SettingsPageHeader
              title={active.label}
              description="ベースの設定に、このプロファイルで指定した内容を上書きします。各項目の変更はまとめて保存されます。"
            />
            <p className="mb-5 text-sm text-gray-500">
              ベース: {settingsTeamId ? `チーム ${settingsTeamId}` : '既定の個人・チーム設定'}
              。未指定の認証方法・モデル・環境変数・MCP はベースの設定を使用します。
            </p>
            {error && (
              <p
                role="alert"
                className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400"
              >
                {error}
              </p>
            )}
            <fieldset disabled={isSubmitting} className="space-y-5">
              {active.slug === 'basic' && <ProfileBasicSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'storages' && <ProfileStoragesSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'inheritance' && (
                <ProfileInheritanceSection {...{ ...state, editingProfile, createScope }} />
              )}
              {active.slug === 'authentication' && (
                <ProfileAuthenticationSection {...{ ...state, editingProfile, createScope }} />
              )}
              {active.slug === 'agent' && <ProfileAgentSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'models' && <ProfileModelsSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'mcp' && <ProfileMcpSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'skills' && <ProfileSkillsSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'environment' && (
                <ProfileEnvironmentSection {...{ ...state, editingProfile, createScope }} />
              )}
              {active.slug === 'system-prompt' && (
                <ProfileSystemPromptSection {...{ ...state, editingProfile, createScope }} />
              )}
              {active.slug === 'secrets' && <ProfileSecretsSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'files' && <ProfileFilesSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'tags' && <ProfileTagsSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'pool' && <ProfilePoolSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'runtime' && <ProfileRuntimeSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'sandbox' && <ProfileSandboxSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'docker' && <ProfileDockerSection {...{ ...state, editingProfile, createScope }} />}
              {active.slug === 'lifecycle' && (
                <ProfileLifecycleSection {...{ ...state, editingProfile, createScope }} />
              )}
            </fieldset>
            <div className="sticky bottom-0 z-30 -mx-4 mt-8 border-t border-gray-200 bg-white/90 px-4 py-3 backdrop-blur dark:border-gray-700 dark:bg-gray-900/90">
              <div className="flex flex-wrap items-center justify-end gap-3">
                {dirty && (
                  <span role="status" className="mr-auto text-sm text-amber-600 dark:text-amber-400">
                    未保存の変更があります
                  </span>
                )}
                <button
                  type="button"
                  disabled={isSubmitting || !dirty}
                  onClick={() => setResetKey((key) => key + 1)}
                  className="rounded-md border px-4 py-1.5 text-sm disabled:opacity-50"
                >
                  破棄
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="rounded-md bg-blue-600 px-5 py-1.5 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50"
                >
                  {isSubmitting ? '保存中...' : isEditing ? '保存' : '作成'}
                </button>
              </div>
            </div>
          </form>
        </div>
      </div>
      {drawerOpen && (
        <div className="fixed inset-0 z-50 md:hidden" role="dialog" aria-modal="true" aria-label="設定項目">
          <button
            type="button"
            className="absolute inset-0 bg-black/40"
            aria-label="設定項目を閉じる"
            onClick={() => setDrawerOpen(false)}
          />
          <div className="absolute inset-y-0 left-0 w-72 overflow-y-auto bg-white p-4 dark:bg-gray-900">
            <button
              type="button"
              autoFocus
              aria-label="閉じる"
              className="mb-2 p-2"
              onClick={() => setDrawerOpen(false)}
            >
              <X className="h-5 w-5" />
            </button>
            {nav}
          </div>
        </div>
      )}
    </main>
  )
}

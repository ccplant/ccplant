'use client'

import { ArrowLeft, PanelLeft, X, Settings, KeyRound, Bot, Server, Terminal, Tags, Shield, Container, Clock, Files } from 'lucide-react'
import { SideNav } from '@/components/settings/ui/SideNav'
import { SettingsPageHeader } from '@/components/settings/ui/SettingsPageHeader'
import { usePathname, useSearchParams } from 'next/navigation'

import { useState, useEffect, useRef } from 'react'
import {
  SessionProfile,
  CreateSessionProfileRequest,
  UpdateSessionProfileRequest,
  CredentialSource,
  SessionProfileParams,
} from '../../types/session_profile'
import { SandboxPolicy } from '../../types/sandbox_policy'
import { LogicalSessionPool } from '../../types/session_pool'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import ProfileConnectionFields from './ProfileConnectionFields'
import type { ModelConnection } from '../../types/settings'
import type { APIMCPServerConfig } from '../../types/settings'
import { MCPServerSettings } from '../../components/settings/MCPServerSettings'

interface SessionProfileEditorProps {
  onClose: () => void
  onSuccess: () => void
  editingProfile?: SessionProfile | null
  section?: string
  createScope?: { scope: 'user' | 'team'; team_id?: string }
}

const profileSections = [
  { slug: 'basic', label: '基本情報', icon: Settings, group: '' },
  { slug: 'inheritance', label: '設定の継承', icon: Settings, group: '' },
  { slug: 'authentication', label: '認証・API 接続', icon: KeyRound, group: 'AI とエージェント' },
  { slug: 'agent', label: 'エージェント', icon: Bot, group: 'AI とエージェント' },
  { slug: 'models', label: 'モデル', icon: Bot, group: 'AI とエージェント' },
  { slug: 'mcp', label: 'MCP サーバー', icon: Server, group: 'セッション環境' },
  { slug: 'environment', label: '環境変数', icon: Terminal, group: 'セッション環境' },
  { slug: 'files', label: 'セッションファイル', icon: Files, group: 'セッション環境' },
  { slug: 'tags', label: 'タグ', icon: Tags, group: 'セッション環境' },
  { slug: 'pool', label: 'プール', icon: Server, group: '実行基盤' },
  { slug: 'sandbox', label: 'ネットワーク制限', icon: Shield, group: '実行基盤' },
  { slug: 'docker', label: 'Docker', icon: Container, group: '実行基盤' },
  { slug: 'lifecycle', label: '自動削除', icon: Clock, group: '実行基盤' },
]

type KeyValuePair = { key: string; value: string }
const SUPPORTED_AGENT_TYPES = new Set(['auto', 'claude-legacy', 'claude-acp', 'codex-acp', 'pi-ollama', 'cursor'])

const normalizeAgentType = (value?: string): string => {
  return value && SUPPORTED_AGENT_TYPES.has(value) ? value : ''
}

export default function SessionProfileEditor({
  onClose,
  onSuccess,
  editingProfile,
  section = 'basic',
  createScope,
}: SessionProfileEditorProps) {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const { getScopeParams, availableTeams = [] } = useTeamScope()

  // Basic fields
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [isDefault, setIsDefault] = useState(false)

  // Config fields
  const [envPairs, setEnvPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [tagPairs, setTagPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [pool, setPool] = useState('')
  const [availablePools, setAvailablePools] = useState<LogicalSessionPool[]>([])
  const [agentType, setAgentType] = useState('')
  const [mcpServers, setMcpServers] = useState<Record<string, APIMCPServerConfig>>({})

  // Docker / DinD fields
  const [dockerEnabled, setDockerEnabled] = useState(false)
  const [dockerRegistries, setDockerRegistries] = useState<Array<{ server: string; username: string; password: string; secretName: string; insecure: boolean }>>([])

  // Network sandbox fields. Profiles always run with sandbox enabled; no selected
  // policy means count mode so traffic is recorded but not blocked.
  const [sandboxPolicyId, setSandboxPolicyId] = useState('')
  const [sandboxPolicies, setSandboxPolicies] = useState<SandboxPolicy[]>([])

  // Session TTL
  const [sessionTTL, setSessionTTL] = useState('')
  const [unsyncedFilePaths, setUnsyncedFilePaths] = useState('')
  const [settingsTeamId, setSettingsTeamId] = useState('')
  const [codexConnection, setCodexConnection] = useState<ModelConnection | null>(null)
  const [claudeConnection, setClaudeConnection] = useState<ModelConnection | null>(null)
  const [codexAuthMode, setCodexAuthMode] = useState<SessionProfileParams['codex_auth_mode'] | ''>('')
  const [claudeAuthMode, setClaudeAuthMode] = useState<SessionProfileParams['claude_auth_mode'] | ''>('')
  const [credentialSource, setCredentialSource] = useState<CredentialSource | ''>('')

  const addDockerRegistry = () => setDockerRegistries(prev => [...prev, { server: '', username: '', password: '', secretName: '', insecure: false }])
  const removeDockerRegistry = (index: number) => { setDirty(true); setDockerRegistries(prev => prev.filter((_, i) => i !== index)) }
  const updateDockerRegistry = (index: number, field: string, value: string | boolean) =>
    setDockerRegistries(prev => prev.map((r, i) => i === index ? { ...r, [field]: value } : r))

  // UI state
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [resetKey, setResetKey] = useState(0)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const dirtyRef = useRef(false)
  dirtyRef.current = dirty

  const isEditing = !!editingProfile
  const scope = editingProfile?.scope ?? createScope?.scope
  const teamId = editingProfile?.team_id ?? createScope?.team_id

  useEffect(() => {
    const fetchConfigOptions = async () => {
      const client = createAgentAPIProxyClientFromStorage()
      try {
        const response = await client.getSandboxPolicies(scope ? { scope, ...(teamId ? { team_id: teamId } : {}) } : getScopeParams())
        setSandboxPolicies(response.sandbox_policies || [])
      } catch {
        setSandboxPolicies([])
      }
      try {
        setAvailablePools(await client.getAvailableSessionPools())
      } catch {
        setAvailablePools([])
      }
    }
    fetchConfigOptions()
  }, [getScopeParams, scope, teamId])

  // Initialize form when editing
  useEffect(() => {
    if (editingProfile) {
      setName(editingProfile.name)
      setDescription(editingProfile.description ?? '')
      setIsDefault(editingProfile.is_default ?? false)

      const cfg = editingProfile.config
      setPool(cfg?.pool ?? '')
      setAgentType(normalizeAgentType(cfg?.params?.agent_type))
      setMcpServers(cfg?.mcp_servers ?? {})

      if (cfg?.environment && Object.keys(cfg.environment).length > 0) {
        setEnvPairs(Object.entries(cfg.environment).map(([key, value]) => ({ key, value })))
      } else {
        setEnvPairs([{ key: '', value: '' }])
      }

      if (cfg?.tags && Object.keys(cfg.tags).length > 0) {
        setTagPairs(Object.entries(cfg.tags).map(([key, value]) => ({ key, value })))
      } else {
        setTagPairs([{ key: '', value: '' }])
      }


      // Initialize docker from profile params
      const docker = cfg?.params?.docker
      if (docker) {
        setDockerEnabled(docker.enabled)
        setDockerRegistries((docker.registries ?? []).map(r => ({
          server: r.server ?? '',
          username: r.username ?? '',
          password: r.password ?? '',
          secretName: r.secret_name ?? '',
          insecure: r.insecure ?? false,
        })))
      } else {
        setDockerEnabled(false)
        setDockerRegistries([])
      }

      // Initialize network sandbox from profile params and legacy top-level policy ID
      const sandbox = cfg?.params?.sandbox
      const policyId = sandbox?.policy_id ?? cfg?.sandbox_policy_id ?? ''
      setSandboxPolicyId(policyId)

      // Initialize session_ttl from profile config
      const ttl = cfg?.session_ttl ?? cfg?.params?.session_ttl ?? ''
      setSessionTTL(ttl)

      const paths = cfg?.unsynced_file_paths ?? cfg?.params?.unsynced_file_paths ?? []
      setUnsyncedFilePaths(paths.join('\n'))

      const source = cfg?.params?.credential_source ?? ''
      setCredentialSource(source)
      setSettingsTeamId(cfg?.settings_team_id ?? '')
      setCodexConnection(cfg?.codex_connection ?? null)
      setClaudeConnection(cfg?.claude_connection ?? null)
      setCodexAuthMode(cfg?.params?.codex_auth_mode ?? '')
      setClaudeAuthMode(cfg?.params?.claude_auth_mode ?? '')
    } else {
      // Reset form
      setName('')
      setDescription('')
      setIsDefault(false)
      setEnvPairs([{ key: '', value: '' }])
      setTagPairs([{ key: '', value: '' }])
      setPool('')
      setAgentType('')
      setMcpServers({})
      setDockerEnabled(false)
      setDockerRegistries([])
      setSandboxPolicyId('')
      setSessionTTL('')
      setUnsyncedFilePaths('')
      setCredentialSource('')
      setSettingsTeamId('')
      setCodexConnection(null)
      setClaudeConnection(null)
      setCodexAuthMode('')
      setClaudeAuthMode('')
    }
    setError(null)
    setDirty(false)
  }, [editingProfile, resetKey])

  useEffect(() => { setDrawerOpen(false) }, [section])
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (dirtyRef.current) { event.preventDefault(); event.returnValue = '' }
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [])

  // Key-value pair helpers
  const updatePair = (
    pairs: KeyValuePair[],
    setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>,
    index: number,
    field: 'key' | 'value',
    val: string
  ) => {
    const next = [...pairs]
    next[index] = { ...next[index], [field]: val }
    setDirty(true)
    setPairs(next)
  }

  const addPair = (setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>) => {
    setDirty(true)
    setPairs((prev) => [...prev, { key: '', value: '' }])
  }

  const removePair = (
    pairs: KeyValuePair[],
    setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>,
    index: number
  ) => {
    setDirty(true)
    if (pairs.length === 1) {
      setPairs([{ key: '', value: '' }])
    } else {
      setDirty(true)
    setPairs((prev) => prev.filter((_, i) => i !== index))
    }
  }

  const pairsToRecord = (pairs: KeyValuePair[]): Record<string, string> => {
    const record: Record<string, string> = {}
    pairs.forEach(({ key, value }) => {
      if (key.trim()) record[key.trim()] = value
    })
    return record
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    if (!name.trim()) {
      setError('基本情報で名前を入力してください')
      return
    }

    setIsSubmitting(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()

      const environment = pairsToRecord(envPairs)
      const tags = pairsToRecord(tagPairs)
      const parsedUnsyncedFilePaths = unsyncedFilePaths
        .split('\n')
        .map(path => path.trim())
        .filter(Boolean)

      // Build docker config if enabled
      let dockerConfig: { enabled: boolean; registries?: { server?: string; username?: string; password?: string; secret_name?: string; insecure?: boolean }[] } | undefined
      if (dockerEnabled) {
        const regs = dockerRegistries
          .filter(r => r.server || r.username || r.secretName)
          .map(r => ({
            ...(r.server ? { server: r.server } : {}),
            ...(r.secretName ? { secret_name: r.secretName } : {}),
            ...(r.username && !r.secretName ? { username: r.username, password: r.password } : {}),
            ...(r.insecure ? { insecure: true } : {}),
          }))
        dockerConfig = { enabled: true, ...(regs.length > 0 ? { registries: regs } : {}) }
      }

      const sandboxConfig = {
        enabled: true,
        ...(sandboxPolicyId ? { policy_id: sandboxPolicyId } : {}),
        ...(!sandboxPolicyId ? { count_mode: true } : {}),
      }

      // Build params if any param is set
      // Preserve fields managed through the API that are not exposed in this editor.
      const extraParams = { ...editingProfile?.config?.params }
      for (const key of ['agent_type', 'sandbox', 'docker', 'codex_auth_mode', 'claude_auth_mode', 'credential_source', 'session_ttl', 'unsynced_file_paths'] as const) delete extraParams[key]
      const params = {
        ...extraParams,
        ...(agentType.trim() ? { agent_type: agentType.trim() } : {}),
        sandbox: sandboxConfig,
        ...(dockerConfig ? { docker: dockerConfig } : {}),
        ...(codexAuthMode ? { codex_auth_mode: codexAuthMode } : {}),
        ...(claudeAuthMode ? { claude_auth_mode: claudeAuthMode } : {}),
        ...(credentialSource ? { credential_source: credentialSource } : {}),
      }

      const connectionPayload = (connection: ModelConnection | null) => {
        if (!connection) return null
        const payload = { ...connection }
        delete payload.has_api_key
        if (!payload.api_key) delete payload.api_key
        return payload
      }
      const extraConfig = { ...editingProfile?.config }
      for (const key of ['settings_team_id', 'codex_connection', 'claude_connection', 'environment', 'tags', 'pool', 'mcp_servers', 'params', 'sandbox_policy_id', 'session_ttl', 'unsynced_file_paths'] as const) delete extraConfig[key]
      const config = {
        ...extraConfig,
        ...(settingsTeamId ? { settings_team_id: settingsTeamId } : {}),
        codex_connection: connectionPayload(codexConnection),
        claude_connection: connectionPayload(claudeConnection),
        ...(Object.keys(environment).length > 0 ? { environment } : {}),
        ...(Object.keys(tags).length > 0 ? { tags } : {}),
        ...(pool.trim() ? { pool: pool.trim() } : {}),
        ...(Object.keys(mcpServers).length > 0 ? { mcp_servers: mcpServers } : {}),
        params,
        ...(sandboxPolicyId ? { sandbox_policy_id: sandboxPolicyId } : {}),
        ...(sessionTTL.trim() ? { session_ttl: sessionTTL.trim() } : {}),
        ...(parsedUnsyncedFilePaths.length > 0 ? { unsynced_file_paths: parsedUnsyncedFilePaths } : {}),
      }

      if (isEditing && editingProfile) {
        const updateData: UpdateSessionProfileRequest = {
          name: name.trim(),
          description: description.trim(),
          is_default: isDefault,
          config: Object.keys(config).length > 0 ? config : undefined,
        }
        await client.updateSessionProfile(editingProfile.id, updateData)
      } else {
        const scopeParams = createScope ?? getScopeParams()
        const createData: CreateSessionProfileRequest = {
          name: name.trim(),
          description: description.trim(),
          is_default: isDefault,
          ...(Object.keys(config).length > 0 ? { config } : {}),
          ...scopeParams,
        }
        await client.createSessionProfile(createData)
      }

      setDirty(false)
      dirtyRef.current = false
      onSuccess()
    } catch (err) {
      console.error('Failed to save session profile:', err)
      setError(err instanceof Error ? err.message : 'セッションプロファイルの保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  const active = profileSections.find(item => item.slug === section) ?? profileSections[0]
  const hrefFor = (slug: string) => {
    const query = new URLSearchParams(searchParams.toString())
    query.set('section', slug)
    return `${pathname}?${query}`
  }
  const groups = [...new Set(profileSections.map(item => item.group))].map(title => ({
    title, items: profileSections.filter(item => item.group === title).map(item => ({ ...item, href: hrefFor(item.slug) })),
  }))
  const leave = () => {
    if (!dirty || window.confirm('未保存の変更があります。破棄して移動しますか？')) onClose()
  }
  const nav = <SideNav groups={groups} activeHref={hrefFor(active.slug)} ariaLabel="セッションプロファイルの設定" header={<div className="px-3 py-2"><p className="text-xs text-gray-500">{isEditing ? 'セッションプロファイル' : '新しいプロファイル'} · {scope === 'team' ? teamId : '個人'}</p><p className="truncate font-semibold">{name || '名前未設定'}</p></div>} />
  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950 text-gray-900 dark:text-white">
      <div className="container mx-auto max-w-6xl px-4 py-6">
        <div className="mb-6 flex items-center gap-3">
          <button type="button" className="md:hidden p-2" aria-label="設定項目を開く" onClick={() => setDrawerOpen(true)}><PanelLeft className="h-5 w-5" /></button>
          <button type="button" onClick={leave} className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-900 dark:hover:text-white"><ArrowLeft className="h-4 w-4" />プロファイル一覧に戻る</button>
        </div>
        <div className="flex gap-8">
          <aside className="hidden md:block">{nav}</aside>
          <form onSubmit={handleSubmit} onChange={() => setDirty(true)} className="min-w-0 flex-1">
            <SettingsPageHeader title={active.label} description="このプロファイルで使用する設定を編集します。各項目の変更はまとめて保存されます。" />
            {error && <p role="alert" className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{error}</p>}
            <fieldset disabled={isSubmitting} className="space-y-5">
            {active.slug === 'basic' && <div className="space-y-5">
{/* Name */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                名前 <span className="text-red-500">*</span>
              </label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例: my-profile"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400"
                required
              />
            </div>

            {/* Description */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                説明
              </label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="このプロファイルの用途を説明してください"
                rows={2}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
              />
            </div>

{/* is_default */}
            <div>
              <label className="flex items-start gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={isDefault}
                  onChange={(e) => setIsDefault(e.target.checked)}
                  className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
                />
                <div>
                  <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                    デフォルトプロファイルに設定する
                  </span>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                    セッション作成時にこのプロファイルをデフォルトとして使用します。
                  </p>
                </div>
              </label>
            </div>

            
            </div>}
            {active.slug === 'inheritance' && <div className="space-y-5">
            <div>
              <label htmlFor="profile-settings-team" className="block text-sm font-medium mb-1">設定の継承元</label>
              <select id="profile-settings-team" value={settingsTeamId} onChange={e => setSettingsTeamId(e.target.value)} className="w-full px-3 py-2 text-sm border rounded-lg dark:bg-gray-700 dark:text-white">
                <option value="">既定の設定を引き継ぐ</option>
                {[...new Set([...availableTeams, ...(settingsTeamId ? [settingsTeamId] : [])])].filter(team => {
                  const scope = editingProfile ?? createScope ?? getScopeParams()
                  return scope.scope !== 'team' || scope.team_id === team
                }).map(team => <option key={team} value={team}>チーム: {team}</option>)}
              </select>
              <p className="text-xs text-gray-500 mt-2">チームを指定すると、そのチームの認証・モデル・環境変数・MCP などの設定を引き継ぎます。プロファイルの専用接続やモデル指定が優先されます。</p>
            </div>

            </div>}
            {active.slug === 'authentication' && <div className="space-y-5">
            <div>
              <label htmlFor="profile-codex-auth" className="block text-sm font-medium mb-1">Codex の認証方法</label>
              <select id="profile-codex-auth" value={codexAuthMode} onChange={e => setCodexAuthMode(e.target.value as typeof codexAuthMode)} className="w-full px-3 py-2 text-sm border rounded-lg dark:bg-gray-700 dark:text-white">
                <option value="">認証設定を引き継ぐ</option>
                <option value="auth_json">ChatGPT / auth.json</option>
                <option value="openai_compatible">OpenAI 互換 API</option>
              </select>
            </div>
            {codexAuthMode === 'openai_compatible' && <ProfileConnectionFields agent="codex" value={codexConnection} onChange={value => { setCodexConnection(value); setDirty(true) }} />}
            <div>
              <label htmlFor="profile-claude-auth" className="block text-sm font-medium mb-1">Claude Code の認証方法</label>
              <select id="profile-claude-auth" value={claudeAuthMode} onChange={e => setClaudeAuthMode(e.target.value as typeof claudeAuthMode)} className="w-full px-3 py-2 text-sm border rounded-lg dark:bg-gray-700 dark:text-white">
                <option value="">認証設定を引き継ぐ</option>
                <option value="oauth">Claude OAuth</option>
                <option value="bedrock">AWS Bedrock</option>
                <option value="anthropic_compatible">Anthropic 互換 API</option>
              </select>
              <p className="text-xs text-gray-500 mt-2">このプロファイルで使う認証方法を指定します。互換 API の接続先・API キーはプロファイル専用に設定できます。</p>
            </div>

            {claudeAuthMode === 'anthropic_compatible' && <ProfileConnectionFields agent="claude" value={claudeConnection} onChange={value => { setClaudeConnection(value); setDirty(true) }} />}

            {/* Credential source */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                認証情報の配布元
              </label>
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                {settingsTeamId ? `認証情報も ${settingsTeamId} から読み込みます。` : null}
                Codex の <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">auth.json</code> など、セッションへ配布する認証情報を選択します。
              </p>
              <select
                value={settingsTeamId ? 'team' : credentialSource}
                disabled={!!settingsTeamId}
                onChange={(e) => setCredentialSource(e.target.value as CredentialSource | '')}
                className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
              >
                <option value="">デフォルト（個人: 作成者 / チーム: 配布なし）</option>
                <option value="session_user">セッションを作成したユーザー</option>
                <option value="team">セッションのチーム</option>
                <option value="none">配布しない</option>
              </select>
            </div>

            
            </div>}
            {active.slug === 'agent' && <div className="space-y-5">
{/* Agent Type */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      エージェントタイプ
                    </label>
                    <div className="space-y-2">
                      {([
                        { value: '', label: 'デフォルト', description: '継承元のエージェント設定を使用します' },
                        { value: 'auto', label: '自動選択', description: 'Codex の互換 API または auth.json があれば Codex ACP、それ以外は Claude ACP' },
                        { value: 'claude-legacy', label: 'Claude Legacy', description: 'Claude の従来の実行方式' },
                        { value: 'claude-acp', label: 'Claude ACP', description: 'Claude Code を使用します' },
                        { value: 'codex-acp', label: 'Codex ACP', description: 'Codex を使用します' },
                        { value: 'pi-ollama', label: 'Pi Ollama', description: 'Ollama に接続する Pi を使用します' },
                        { value: 'cursor', label: 'Cursor ACP', description: 'Cursor を使用します' },
                      ]).map(({ value: v, label, description }) => (
                        <label key={v} className="flex items-start cursor-pointer group">
                          <input
                            type="radio"
                            name="profile-agent-type"
                            value={v}
                            checked={agentType === v}
                            onChange={() => setAgentType(v)}
                            className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500"
                          />
                          <span className="ml-2">
                            <span className="block text-xs font-medium text-gray-600 dark:text-gray-400 group-hover:text-gray-800 dark:group-hover:text-gray-200">
                              {label}
                            </span>
                            <span className="block text-xs text-gray-400 dark:text-gray-500 mt-0.5">
                              {description}
                            </span>
                          </span>
                        </label>
                      ))}
                    </div>
                  </div>

                  
            </div>}
            {active.slug === 'models' && <div className="space-y-5">
                  <div className="space-y-3">
                    <p className="text-sm font-medium">モデルの上書き</p>
                    <p className="text-xs text-gray-500">空欄なら認証設定のデフォルトモデルを使用します。接続先・認証情報は変更しません。</p>
                    {([{ key: 'CODEX_MODEL', label: 'Codex モデル ID' }, { key: 'ANTHROPIC_MODEL', label: 'Claude Code モデル ID' }]).map(({ key, label }) => (
                      <label key={key} className="block text-sm">{label}
                        <input aria-label={label} value={envPairs.find(pair => pair.key === key)?.value || (key === 'CODEX_MODEL' ? envPairs.find(pair => pair.key === 'OPENAI_MODEL')?.value : '') || ''}
                          onChange={e => setEnvPairs(prev => [...prev.filter(pair => pair.key !== key && !(key === 'CODEX_MODEL' && pair.key === 'OPENAI_MODEL')), ...(e.target.value ? [{ key, value: e.target.value }] : [])])}
                          placeholder="デフォルトを継承" className="w-full rounded-md border bg-white p-2 dark:bg-gray-800" />
                      </label>
                    ))}
                  </div>

                  
            </div>}
            {active.slug === 'mcp' && <div className="space-y-5">
{/* MCP Servers */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      MCP サーバー
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
                      個人・チーム設定の MCP サーバーを継承します。同じ名前のサーバーは、このプロファイルの設定で上書きされます。
                    </p>
                    <MCPServerSettings servers={mcpServers} onChange={value => { setMcpServers(value); setDirty(true) }} />
                  </div>

                  
            </div>}
            {active.slug === 'environment' && <div className="space-y-5">
{/* Environment Variables */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      環境変数
                    </label>
                    <div className="space-y-2">
                      {envPairs.map((pair, idx) => (
                        <div key={idx} className="rounded border border-gray-200 dark:border-gray-600 p-2 space-y-1.5 bg-gray-50 dark:bg-gray-900/40">
                          <input
                            type="text"
                            value={pair.key}
                            onChange={(e) => updatePair(envPairs, setEnvPairs, idx, 'key', e.target.value)}
                            placeholder="KEY"
                            className="w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                          <div className="flex items-center gap-2">
                            <input
                              type="text"
                              value={pair.value}
                              onChange={(e) => updatePair(envPairs, setEnvPairs, idx, 'value', e.target.value)}
                              placeholder="value"
                              className="flex-1 min-w-0 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                            />
                            <button
                              type="button"
                              onClick={() => removePair(envPairs, setEnvPairs, idx)}
                              className="shrink-0 p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                            >
                              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                              </svg>
                            </button>
                          </div>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => addPair(setEnvPairs)}
                        className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
                      >
                        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                        </svg>
                        追加
                      </button>
                    </div>
                  </div>

                  
            </div>}
            {active.slug === 'files' && <div className="space-y-5">
{/* 同期しないファイルパス */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      同期しないファイルパス
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                      変更を保存先へ同期しない管理ファイルのパスを 1 行に 1 つ指定します。
                    </p>
                    <textarea
                      value={unsyncedFilePaths}
                      onChange={e => setUnsyncedFilePaths(e.target.value)}
                      placeholder="/home/agentapi/.codex/auth.json"
                      rows={3}
                      className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white font-mono resize-y"
                    />
                  </div>

            </div>}
            {active.slug === 'tags' && <div className="space-y-5">
{/* Tags */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      タグ
                    </label>
                    <div className="space-y-2">
                      {tagPairs.map((pair, idx) => (
                        <div key={idx} className="rounded border border-gray-200 dark:border-gray-600 p-2 space-y-1.5 bg-gray-50 dark:bg-gray-900/40">
                          <input
                            type="text"
                            value={pair.key}
                            onChange={(e) => updatePair(tagPairs, setTagPairs, idx, 'key', e.target.value)}
                            placeholder="key"
                            className="w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                          <div className="flex items-center gap-2">
                            <input
                              type="text"
                              value={pair.value}
                              onChange={(e) => updatePair(tagPairs, setTagPairs, idx, 'value', e.target.value)}
                              placeholder="value"
                              className="flex-1 min-w-0 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                            />
                            <button
                              type="button"
                              onClick={() => removePair(tagPairs, setTagPairs, idx)}
                              className="shrink-0 p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                            >
                              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                              </svg>
                            </button>
                          </div>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => addPair(setTagPairs)}
                        className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
                      >
                        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                        </svg>
                        追加
                      </button>
                    </div>
                  </div>


            </div>}
            {active.slug === 'pool' && <div className="space-y-5">
{/* Session Runner Pool */}
                  <div>
                    <label htmlFor="session-profile-pool" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      Session Runner Pool
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                      このプロファイルで作成するセッションの実行先Poolを指定します。空欄の場合は自動選択されます。
                    </p>
                    <select
                      id="session-profile-pool"
                      value={pool}
                      onChange={(e) => setPool(e.target.value)}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400"
                    >
                      <option value="">自動選択</option>
                      {pool && !availablePools.some((availablePool) => availablePool.name === pool) && (
                        <option value={pool}>{pool}（現在の設定）</option>
                      )}
                      {availablePools.map((availablePool) => (
                        <option key={availablePool.name} value={availablePool.name}>
                          {availablePool.name}
                        </option>
                      ))}
                    </select>
                    {availablePools.length === 0 && (
                      <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
                        選択可能なPool候補がありません。
                      </p>
                    )}
                  </div>

                  
            </div>}
            {active.slug === 'sandbox' && <div className="space-y-5">
{/* Network Sandbox */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      sandbox policy
                    </label>
                    <select
                      value={sandboxPolicyId}
                      onChange={(e) => setSandboxPolicyId(e.target.value)}
                      className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                    >
                      <option value="">ポリシーなし（count mode）</option>
                      {sandboxPolicies.map((policy) => (
                        <option key={policy.id} value={policy.id}>
                          {policy.name}{policy.scope === 'team' ? ' [チーム]' : ''}
                        </option>
                      ))}
                    </select>
                    {sandboxPolicies.length === 0 && (
                      <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
                        適用できる sandbox policy がありません。count mode で起動します。
                      </p>
                    )}
                  </div>

                  
            </div>}
            {active.slug === 'docker' && <div className="space-y-5">
{/* Docker in Docker (DinD) */}
                  <div>
                    <label className="flex items-start gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={dockerEnabled}
                        onChange={(e) => setDockerEnabled(e.target.checked)}
                        className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
                      />
                      <div>
                        <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                          Docker in Docker (DinD) を有効にする
                        </span>
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                          セッション Pod に docker:dind サイドカーを追加し、<code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded text-xs">DOCKER_HOST</code> を自動設定します。
                        </p>
                      </div>
                    </label>

                    {dockerEnabled && (
                      <div className="mt-3 ml-6 space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-xs font-medium text-gray-600 dark:text-gray-400">認証済みレジストリ（任意）</span>
                          <button
                            type="button"
                            onClick={addDockerRegistry}
                            className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700"
                          >
                            + 追加
                          </button>
                        </div>
                        {dockerRegistries.length === 0 && (
                          <p className="text-xs text-gray-400 dark:text-gray-500">
                            認証不要な場合は追加不要です。
                          </p>
                        )}
                        {dockerRegistries.map((registry, index) => (
                          <div key={index} className="border border-gray-200 dark:border-gray-600 rounded-lg p-3 space-y-2">
                            <div className="flex items-center justify-between">
                              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">レジストリ #{index + 1}</span>
                              <button
                                type="button"
                                onClick={() => removeDockerRegistry(index)}
                                className="text-xs text-red-500 hover:text-red-600"
                              >
                                削除
                              </button>
                            </div>
                            <div>
                              <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">サーバー（空 = Docker Hub）</label>
                              <input
                                type="text"
                                value={registry.server}
                                onChange={e => updateDockerRegistry(index, 'server', e.target.value)}
                                placeholder="ghcr.io"
                                className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                              />
                            </div>
                            <div className="flex items-center gap-2">
                              <input
                                type="checkbox"
                                id={`insecure-${index}`}
                                checked={registry.insecure}
                                onChange={e => updateDockerRegistry(index, 'insecure', e.target.checked)}
                                className="w-3 h-3 rounded"
                              />
                              <label htmlFor={`insecure-${index}`} className="text-xs text-gray-500 dark:text-gray-400">HTTP（insecure）レジストリ</label>
                            </div>
                            <div>
                              <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">K8s Secret 名（docker config JSON）</label>
                              <input
                                type="text"
                                value={registry.secretName}
                                onChange={e => updateDockerRegistry(index, 'secretName', e.target.value)}
                                placeholder="my-registry-secret"
                                className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                              />
                            </div>
                            {!registry.secretName && (
                              <>
                                <div>
                                  <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">ユーザー名</label>
                                  <input
                                    type="text"
                                    value={registry.username}
                                    onChange={e => updateDockerRegistry(index, 'username', e.target.value)}
                                    placeholder="myuser"
                                    className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                                  />
                                </div>
                                <div>
                                  <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">パスワード / アクセストークン</label>
                                  <input
                                    type="password"
                                    value={registry.password}
                                    onChange={e => updateDockerRegistry(index, 'password', e.target.value)}
                                    placeholder="••••••••"
                                    className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                                  />
                                </div>
                              </>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  
            </div>}
            {active.slug === 'lifecycle' && <div className="space-y-5">
{/* セッション TTL */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      セッション自動削除 TTL
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                      最後のメッセージからこの時間が経過するとセッションを自動削除します。例: <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">24h</code>、<code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">168h</code>（空欄 = 自動削除なし / グローバル設定に従う）
                    </p>
                    <input
                      type="text"
                      value={sessionTTL}
                      onChange={e => setSessionTTL(e.target.value)}
                      placeholder="例: 24h、72h、168h"
                      className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                    />
                  </div>

                  
            </div>}
            </fieldset>
            <div className="sticky bottom-0 z-30 -mx-4 mt-8 border-t border-gray-200 bg-white/90 px-4 py-3 backdrop-blur dark:border-gray-700 dark:bg-gray-900/90">
              <div className="flex flex-wrap items-center justify-end gap-3">
                {dirty && <span role="status" className="mr-auto text-sm text-amber-600 dark:text-amber-400">未保存の変更があります</span>}
                <button type="button" disabled={isSubmitting || !dirty} onClick={() => setResetKey(key => key + 1)} className="rounded-md border px-4 py-1.5 text-sm disabled:opacity-50">破棄</button>
                <button type="submit" disabled={isSubmitting} className="rounded-md bg-blue-600 px-5 py-1.5 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50">{isSubmitting ? '保存中...' : isEditing ? '保存' : '作成'}</button>
              </div>
            </div>
          </form>
        </div>
      </div>
      {drawerOpen && <div className="fixed inset-0 z-50 md:hidden" role="dialog" aria-modal="true" aria-label="設定項目">
        <button type="button" className="absolute inset-0 bg-black/40" aria-label="設定項目を閉じる" onClick={() => setDrawerOpen(false)} />
        <div className="absolute inset-y-0 left-0 w-72 overflow-y-auto bg-white p-4 dark:bg-gray-900">
          <button type="button" autoFocus aria-label="閉じる" className="mb-2 p-2" onClick={() => setDrawerOpen(false)}><X className="h-5 w-5" /></button>{nav}
        </div>
      </div>}
    </main>
  )
}

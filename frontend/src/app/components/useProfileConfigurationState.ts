'use client'

import { useState } from 'react'
import {
  ProfileFile,
  ProfileRepository,
  SessionProfile,
  SessionProfileParams,
  SessionStateConfig,
} from '../../types/session_profile'

import { LogicalSessionPool } from '../../types/session_pool'

import type { APIMCPServerConfig, SettingsSecret } from '../../types/settings'

import type { GitHubIdentity } from '../../types/github-connection'
import type { StorageAttachment } from '../../types/storage'

type KeyValuePair = { key: string; value: string }
type DefaultModelTarget =
  | 'codex:all'
  | 'codex:auth_json'
  | 'codex:openai_compatible'
  | 'claude:all'
  | 'claude:oauth'
  | 'claude:bedrock'
  | 'claude:anthropic_compatible'

export function useProfileConfigurationState() {
  // Config fields
  const [envPairs, setEnvPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [tagPairs, setTagPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [pool, setPool] = useState('')
  const [availablePools, setAvailablePools] = useState<LogicalSessionPool[]>([])
  const [agentType, setAgentType] = useState('')
  const [codexModel, setCodexModel] = useState('')
  const [claudeModel, setClaudeModel] = useState('')
  const [codexDefaultModels, setCodexDefaultModels] = useState<
    NonNullable<SessionProfileParams['codex_default_models']>
  >({})
  const [claudeDefaultModels, setClaudeDefaultModels] = useState<
    NonNullable<SessionProfileParams['claude_default_models']>
  >({})
  const [defaultModelTarget, setDefaultModelTarget] = useState<DefaultModelTarget>('codex:all')
  const [modelOptions, setModelOptions] = useState('')
  const [profileFiles, setProfileFiles] = useState<ProfileFile[]>([])
  const [profileRepositories, setProfileRepositories] = useState<ProfileRepository[]>([])
  const [storages, setStorages] = useState<StorageAttachment[]>([])
  const [sessionState, setSessionState] = useState<SessionStateConfig>({
    enabled: false,
    components: ['agent_state'],
    agents: ['claude'],
  })
  const [mcpServers, setMcpServers] = useState<Record<string, APIMCPServerConfig>>({})
  const [sourceProfileId, setSourceProfileId] = useState('')
  const [availableProfiles, setAvailableProfiles] = useState<SessionProfile[]>([])
  const [availableSecrets, setAvailableSecrets] = useState<SettingsSecret[]>([])
  const [selectedSecretIds, setSelectedSecretIds] = useState<string[]>([])
  const [skills, setSkills] = useState<string[]>([])
  const [piPackages, setPiPackages] = useState('')
  const [commandWrapperTemplate, setCommandWrapperTemplate] = useState('')
  const [systemPrompt, setSystemPrompt] = useState('')
  const [systemPromptMode, setSystemPromptMode] = useState<'override' | 'append'>('override')
  const [githubRepositories, setGitHubRepositories] = useState('')
  const [linkedConnections, setLinkedConnections] = useState<GitHubIdentity[]>([])
  const [connectionId, setConnectionId] = useState('')

  return {
    envPairs,
    setEnvPairs,
    tagPairs,
    setTagPairs,
    pool,
    setPool,
    availablePools,
    setAvailablePools,
    agentType,
    setAgentType,
    codexModel,
    setCodexModel,
    claudeModel,
    setClaudeModel,
    codexDefaultModels,
    setCodexDefaultModels,
    claudeDefaultModels,
    setClaudeDefaultModels,
    defaultModelTarget,
    setDefaultModelTarget,
    modelOptions,
    setModelOptions,
    profileFiles,
    setProfileFiles,
    profileRepositories,
    setProfileRepositories,
    storages,
    setStorages,
    sessionState,
    setSessionState,
    mcpServers,
    setMcpServers,
    sourceProfileId,
    setSourceProfileId,
    availableProfiles,
    setAvailableProfiles,
    availableSecrets,
    setAvailableSecrets,
    selectedSecretIds,
    setSelectedSecretIds,
    skills,
    setSkills,
    piPackages,
    setPiPackages,
    commandWrapperTemplate,
    setCommandWrapperTemplate,
    systemPrompt,
    setSystemPrompt,
    systemPromptMode,
    setSystemPromptMode,
    githubRepositories,
    setGitHubRepositories,
    linkedConnections,
    setLinkedConnections,
    connectionId,
    setConnectionId,
  }
}

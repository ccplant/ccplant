'use client'

import { useState } from 'react'
import { SandboxPolicy } from '../../types/sandbox_policy'
import { SessionProfileParams } from '../../types/session_profile'

import type { ModelConnection } from '../../types/settings'

export function useProfileRuntimeState() {
  // Docker / DinD fields
  const [dockerEnabled, setDockerEnabled] = useState(false)
  const [dockerRegistries, setDockerRegistries] = useState<
    Array<{ server: string; username: string; password: string; secretName: string; insecure: boolean }>
  >([])

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
  const [piConnection, setPiConnection] = useState<ModelConnection | null>(null)
  const [codexAuthMode, setCodexAuthMode] = useState<SessionProfileParams['codex_auth_mode'] | ''>('')
  const [claudeAuthMode, setClaudeAuthMode] = useState<SessionProfileParams['claude_auth_mode'] | ''>('')

  return {
    dockerEnabled,
    setDockerEnabled,
    dockerRegistries,
    setDockerRegistries,
    sandboxPolicyId,
    setSandboxPolicyId,
    sandboxPolicies,
    setSandboxPolicies,
    sessionTTL,
    setSessionTTL,
    unsyncedFilePaths,
    setUnsyncedFilePaths,
    settingsTeamId,
    setSettingsTeamId,
    codexConnection,
    setCodexConnection,
    claudeConnection,
    setClaudeConnection,
    piConnection,
    setPiConnection,
    codexAuthMode,
    setCodexAuthMode,
    claudeAuthMode,
    setClaudeAuthMode,
  }
}

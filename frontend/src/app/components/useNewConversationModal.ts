'use client'

import { useCallback, useEffect, useState, type ChangeEvent, type FormEvent } from 'react'
import { AgentAPIProxyError, createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { getFilterValuesForSessionCreation, type SessionFilter } from '../../lib/filter-utils'
import { addRepositoryToHistory } from '../../types/settings'
import { OrganizationHistory } from '../../utils/organizationHistory'
import type { EnvironmentVariable, NewConversationModalViewProps } from './NewConversationModalView'

const EMPTY_VARIABLE: EnvironmentVariable = { key: '', value: '' }
const TAG_FIELDS = ['project_type', 'technology', 'priority', 'status', 'category']

function useVariableList() {
  const [values, setValues] = useState<EnvironmentVariable[]>([EMPTY_VARIABLE])
  const add = useCallback(() => setValues(current => [...current, EMPTY_VARIABLE]), [])
  const remove = useCallback((index: number) => setValues(current => current.length > 1 ? current.filter((_, position) => position !== index) : current), [])
  const update = useCallback((index: number, field: 'key' | 'value', value: string) => {
    setValues(current => current.map((entry, position) => position === index ? { ...entry, [field]: value } : entry))
  }, [])
  const reset = useCallback(() => setValues([EMPTY_VARIABLE]), [])
  return { values, setValues, add, remove, update, reset }
}

function useRepositoryField() {
  const [repository, setRepository] = useState('')
  const [suggestions, setSuggestions] = useState<string[]>([])
  const [showSuggestions, setShowSuggestions] = useState(false)
  const showMatches = useCallback((query?: string) => {
    const matches = OrganizationHistory.getRepositorySuggestions(query)
    setSuggestions(matches)
    setShowSuggestions(matches.length > 0)
  }, [])
  const onChange = useCallback((event: ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value
    setRepository(value)
    if (value.trim()) showMatches(value); else setShowSuggestions(false)
  }, [showMatches])
  const onSelect = useCallback((value: string) => { setRepository(value); setShowSuggestions(false) }, [])
  const onBlur = useCallback(() => { setTimeout(() => setShowSuggestions(false), 150) }, [])
  const reset = useCallback(() => { setRepository(''); setShowSuggestions(false) }, [])
  return { repository, setRepository, suggestions, showSuggestions, onChange, onFocus: () => showMatches(), onBlur, onSelect, reset }
}

function variablesToRecord(values: EnvironmentVariable[]): Record<string, string> {
  return values.reduce<Record<string, string>>((result, entry) => {
    if (entry.key.trim() && entry.value.trim()) result[entry.key] = entry.value
    return result
  }, {})
}

export function buildNewConversationSessionData(description: string, repository: string, envVars: EnvironmentVariable[], metadataVars: EnvironmentVariable[]) {
  const environment = variablesToRecord(envVars)
  const metadata: Record<string, unknown> = variablesToRecord(metadataVars)
  const tags: Record<string, string> = {}
  metadata.description = description.trim()
  if (repository.trim()) tags.repository = repository.trim()
  for (const key of TAG_FIELDS) {
    if (typeof metadata[key] !== 'string' || !metadata[key]) continue
    tags[key] = String(metadata[key])
    delete metadata[key]
  }
  return {
    environment: Object.keys(environment).length > 0 ? environment : undefined,
    metadata: Object.keys(metadata).length > 0 ? metadata : undefined,
    tags: Object.keys(tags).length > 0 ? tags : undefined,
  }
}

function rememberRepository(repository: string): void {
  const name = repository.trim()
  if (!name) return
  addRepositoryToHistory(name)
  if (!name.includes('/')) return
  const [organization] = name.split('/', 2)
  if (organization) OrganizationHistory.addRepositoryToOrganization(organization, name)
}

async function startConversation(input: {
  description: string
  repository: string
  envVars: EnvironmentVariable[]
  metadataVars: EnvironmentVariable[]
  oneshot: boolean
  selectedTeam: string | null
}): Promise<void> {
  const sessionData = buildNewConversationSessionData(input.description, input.repository, input.envVars, input.metadataVars)
  const scope = input.selectedTeam ? { scope: 'team' as const, team_id: input.selectedTeam } : { scope: 'user' as const }
  await createAgentAPIProxyClientFromStorage().start({
    ...sessionData,
    params: { message: input.description.trim(), ...(input.oneshot ? { oneshot: true } : {}) },
    ...scope,
  })
  rememberRepository(input.repository)
}

function errorMessage(error: unknown): string {
  if (error instanceof AgentAPIProxyError) return `Failed to create session: ${error.message}`
  return error instanceof Error ? error.message : 'Failed to create session'
}

interface ModelOptions {
  isOpen: boolean
  selectedTeam: string | null
  currentFilters?: SessionFilter
  initialRepository?: string
  onClose: () => void
  onSuccess: () => void
}

export function useNewConversationModal(options: ModelOptions): NewConversationModalViewProps {
	const { isOpen, selectedTeam, currentFilters, initialRepository, onClose, onSuccess } = options
  const [description, setDescription] = useState('')
  const [userId, setUserId] = useState('current-user')
  const [oneshot, setOneshot] = useState(false)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const environment = useVariableList()
  const metadata = useVariableList()
  const repository = useRepositoryField()
	const { values: envVars, setValues: setEnvVars, add: addEnv, remove: removeEnv, update: updateEnv, reset: resetEnv } = environment
	const { values: metadataVars, setValues: setMetadataVars, add: addMetadata, remove: removeMetadata, update: updateMetadata, reset: resetMetadata } = metadata
	const { repository: repositoryName, setRepository, suggestions, showSuggestions, onChange, onFocus, onBlur, onSelect, reset: resetRepository } = repository

  const reset = useCallback(() => {
    setDescription(''); setUserId('current-user'); setOneshot(false); setError(null)
    resetEnv(); resetMetadata(); resetRepository()
  }, [resetEnv, resetMetadata, resetRepository])
  const close = useCallback(() => { reset(); onClose() }, [onClose, reset])

  useEffect(() => {
    if (!isOpen) return
    if (currentFilters) {
      const values = getFilterValuesForSessionCreation(currentFilters)
      const envEntries = Object.entries(values.environment).map(([key, value]) => ({ key, value }))
      const metadataEntries = [...Object.entries(values.tags), ...Object.entries(values.metadata)].map(([key, value]) => ({ key, value: String(value) }))
      if (envEntries.length) setEnvVars(envEntries)
      if (metadataEntries.length) setMetadataVars(metadataEntries)
      if (values.tags.repository) setRepository(values.tags.repository)
    }
    if (initialRepository) setRepository(initialRepository)
  }, [isOpen, currentFilters, initialRepository, setEnvVars, setMetadataVars, setRepository])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape' && isOpen) close() }
    if (isOpen) document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [close, isOpen])

  const submit = useCallback(async (event: FormEvent) => {
    event.preventDefault()
    if (!description.trim()) { setError('Description is required'); return }
    setCreating(true); setError(null)
    try {
      await startConversation({ description, repository: repositoryName, envVars, metadataVars, oneshot, selectedTeam })
      onSuccess(); close()
    } catch (submitError) {
      setError(errorMessage(submitError))
    } finally {
      setCreating(false)
    }
  }, [close, description, envVars, metadataVars, oneshot, onSuccess, repositoryName, selectedTeam])

  return {
    userId, description, repository: repositoryName, oneshot,
    envVars, metadataVars,
    suggestions, showSuggestions,
    disabled: creating, error,
    onUserIdChange: setUserId, onDescriptionChange: setDescription, onOneshotChange: setOneshot,
    onChange, onFocus, onBlur, onSelect,
    onAddEnv: addEnv, onRemoveEnv: removeEnv, onUpdateEnv: updateEnv,
    onAddMetadata: addMetadata, onRemoveMetadata: removeMetadata, onUpdateMetadata: updateMetadata,
    onSubmit: submit, onClose: close,
  }
}

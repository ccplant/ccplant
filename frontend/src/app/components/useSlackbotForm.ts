'use client'

import { useState, useEffect, useCallback, useMemo, type Dispatch, type FormEvent, type KeyboardEvent, type SetStateAction } from 'react'
import { type SlackBot, type CreateSlackBotRequest, type SlackBotSecretRef, type SlackBotSessionConfig, type UpdateSlackBotRequest } from '../../types/slackbot'
import type { ResourceScope } from '../../types/agentapi'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import type { SettingsSecret } from '../../types/settings'

export type KeyValuePair = { key: string; value: string }

function useChipInput() {
  const [values, setValues] = useState<string[]>([])
  const [input, setInput] = useState('')
  const add = useCallback(() => {
    const trimmed = input.trim()
    if (trimmed) setValues(current => current.includes(trimmed) ? current : [...current, trimmed])
    setInput('')
  }, [input])
  const onKeyDown = useCallback((event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== 'Enter' && event.key !== ',') return
    event.preventDefault()
    add()
  }, [add])
  const remove = useCallback((value: string) => setValues(current => current.filter(item => item !== value)), [])
  return { values, setValues, input, setInput, add, onKeyDown, remove }
}

export function pairsToRecord(pairs: KeyValuePair[]): Record<string, string> {
  return pairs.reduce<Record<string, string>>((record, { key, value }) => {
    if (key.trim()) record[key.trim()] = value
    return record
  }, {})
}

function updatePairList(pairs: KeyValuePair[], index: number, field: 'key' | 'value', value: string): KeyValuePair[] {
  return pairs.map((pair, position) => position === index ? { ...pair, [field]: value } : pair)
}

function removePairFromList(pairs: KeyValuePair[], index: number): KeyValuePair[] {
  return pairs.length === 1 ? [{ key: '', value: '' }] : pairs.filter((_, position) => position !== index)
}

const pairActions = {
  updatePair(pairs: KeyValuePair[], setPairs: Setter<KeyValuePair[]>, index: number, field: 'key' | 'value', value: string) {
    setPairs(updatePairList(pairs, index, field, value))
  },
  addPair(setPairs: Setter<KeyValuePair[]>) {
    setPairs(previous => [...previous, { key: '', value: '' }])
  },
  removePair(pairs: KeyValuePair[], setPairs: Setter<KeyValuePair[]>, index: number) {
    setPairs(removePairFromList(pairs, index))
  },
}

interface UseSlackbotFormOptions {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingSlackbot?: SlackBot | null
}

type Setter<T> = Dispatch<SetStateAction<T>>

interface SlackbotFormSetters {
  setName: Setter<string>; setBotToken: Setter<string>; setAppToken: Setter<string>
  setMaxSessions: Setter<number>; setNotifyOnSessionCreated: Setter<boolean>; setAllowBotMessages: Setter<boolean>
  setAllowedChannelNames: Setter<string[]>; setChannelNameInput: Setter<string>
  setAllowedUserIDs: Setter<string[]>; setUserIDInput: Setter<string>
  setInitialMessageTemplate: Setter<string>; setReuseMessageTemplate: Setter<string>
  setEnvPairs: Setter<KeyValuePair[]>; setTagPairs: Setter<KeyValuePair[]>
  setShowBotTokenSection: Setter<boolean>; setShowAdvanced: Setter<boolean>
  setRepoFullName: Setter<string>; setSessionProfileId: Setter<string>; setError: Setter<string | null>
}

function resetSlackbotForm(setters: SlackbotFormSetters): void {
  setters.setName(''); setters.setBotToken(''); setters.setAppToken('')
  setters.setMaxSessions(10); setters.setNotifyOnSessionCreated(true); setters.setAllowBotMessages(false)
  setters.setAllowedChannelNames([]); setters.setChannelNameInput('')
  setters.setAllowedUserIDs([]); setters.setUserIDInput('')
  setters.setInitialMessageTemplate('{{ .event.text }}'); setters.setReuseMessageTemplate('{{ .event.text }}')
  setters.setEnvPairs([{ key: '', value: '' }]); setters.setTagPairs([{ key: '', value: '' }])
  setters.setShowBotTokenSection(false); setters.setShowAdvanced(false)
  setters.setRepoFullName(''); setters.setSessionProfileId('')
}

function loadSlackbotForm(slackbot: SlackBot, setters: SlackbotFormSetters): void {
  const config = slackbot.session_config
  setters.setName(slackbot.name)
  setters.setMaxSessions(slackbot.max_sessions ?? 10)
  setters.setNotifyOnSessionCreated(slackbot.notify_on_session_created ?? true)
  setters.setAllowBotMessages(slackbot.allow_bot_messages ?? false)
  setters.setAllowedChannelNames(slackbot.allowed_channel_names || [])
  setters.setAllowedUserIDs(slackbot.allowed_user_ids || [])
  setters.setSessionProfileId(config?.session_profile_id || '')
  setters.setInitialMessageTemplate(config?.initial_message_template ?? '{{ .event.text }}')
  setters.setReuseMessageTemplate(config?.reuse_message_template ?? '{{ .event.text }}')
  setters.setEnvPairs(config?.environment && Object.keys(config.environment).length ? Object.entries(config.environment).map(([key, value]) => ({ key, value })) : [{ key: '', value: '' }])
  setters.setTagPairs(config?.tags && Object.keys(config.tags).length ? Object.entries(config.tags).map(([key, value]) => ({ key, value })) : [{ key: '', value: '' }])
  setters.setRepoFullName(config?.params?.repo_full_name || '')
  const expanded = Boolean(config?.environment && Object.keys(config.environment).length) || Boolean(config?.tags && Object.keys(config.tags).length) || Boolean(config?.params?.repo_full_name) || Boolean(slackbot.bot_token_secret_name)
  setters.setShowBotTokenSection(Boolean(slackbot.bot_token_secret_name))
  setters.setShowAdvanced(expanded)
}

function useInitializeSlackbotForm(isOpen: boolean, editingSlackbot: SlackBot | null | undefined, setters: SlackbotFormSetters): void {
  useEffect(() => {
    if (editingSlackbot) loadSlackbotForm(editingSlackbot, setters); else resetSlackbotForm(setters)
    setters.setError(null)
  }, [editingSlackbot, isOpen, setters])
}

interface SlackbotSecretsOptions {
  isOpen: boolean; selectedTeam: string | null; editingSlackbot: SlackBot | null | undefined
  setSecretOwner: Setter<string>; setAvailableSecrets: Setter<SettingsSecret[]>; setSelectedSecretId: Setter<string>
}

function useSlackbotSecrets(options: SlackbotSecretsOptions): void {
  const { isOpen, selectedTeam, editingSlackbot, setSecretOwner, setAvailableSecrets, setSelectedSecretId } = options
  useEffect(() => {
    if (!isOpen) return
    const load = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const owner = selectedTeam || (await client.getUserInfo()).principal_id
        setSecretOwner(owner)
        const secrets = await client.listSettingsSecrets(owner)
        setAvailableSecrets(secrets.filter(secret => secret.keys.includes('bot-token') && secret.keys.includes('app-token')))
        setSelectedSecretId(editingSlackbot?.secret?.secret_id || '')
      } catch { setAvailableSecrets([]) }
    }
    void load()
  }, [editingSlackbot, isOpen, selectedTeam, setAvailableSecrets, setSecretOwner, setSelectedSecretId])
}

function useEscapeClose(isOpen: boolean, onClose: () => void): void {
  useEffect(() => {
    const handleKeyDown = (event: globalThis.KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    if (isOpen) document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])
}

interface SlackbotSubmission {
  name: string; botToken: string; appToken: string; secretOwner: string; selectedSecretId: string
  allowedChannelNames: string[]; allowedUserIDs: string[]; maxSessions: number
  notifyOnSessionCreated: boolean; allowBotMessages: boolean; showBotTokenSection: boolean
  initialMessageTemplate: string; reuseMessageTemplate: string; envPairs: KeyValuePair[]; tagPairs: KeyValuePair[]
  repoFullName: string; sessionProfileId: string; editingSlackbot?: SlackBot | null
  getScopeParams: () => { scope: ResourceScope; team_id?: string }
}

export function buildSlackbotSessionConfig(input: Pick<SlackbotSubmission, 'initialMessageTemplate' | 'reuseMessageTemplate' | 'envPairs' | 'tagPairs' | 'repoFullName' | 'sessionProfileId'>): SlackBotSessionConfig {
  const environment = pairsToRecord(input.envPairs)
  const tags = pairsToRecord(input.tagPairs)
  return {
    ...(input.initialMessageTemplate.trim() ? { initial_message_template: input.initialMessageTemplate.trim() } : {}),
    ...(input.reuseMessageTemplate.trim() ? { reuse_message_template: input.reuseMessageTemplate.trim() } : {}),
    ...(Object.keys(environment).length ? { environment } : {}),
    ...(Object.keys(tags).length ? { tags } : {}),
    ...(input.repoFullName.trim() ? { params: { repo_full_name: input.repoFullName.trim() } } : {}),
    ...(input.sessionProfileId.trim() ? { session_profile_id: input.sessionProfileId.trim() } : {}),
  }
}

async function resolveSlackbotSecret(input: SlackbotSubmission): Promise<SlackBotSecretRef | undefined> {
  if (input.selectedSecretId) return { secret_id: input.selectedSecretId, bot_token_key: 'bot-token', app_token_key: 'app-token' }
  if (!input.botToken.trim() || !input.appToken.trim()) return undefined
  const created = await createAgentAPIProxyClientFromStorage().createSettingsSecret(input.secretOwner, {
    name: `${input.name.trim()} Slack App`,
    values: { 'bot-token': input.botToken.trim(), 'app-token': input.appToken.trim() },
  })
  return { secret_id: created.id, bot_token_key: 'bot-token', app_token_key: 'app-token' }
}

async function saveSlackbot(input: SlackbotSubmission): Promise<void> {
  const client = createAgentAPIProxyClientFromStorage()
  const sessionConfig = buildSlackbotSessionConfig(input)
  const secret = await resolveSlackbotSecret(input)
  const shared = {
    name: input.name.trim(), ...(secret ? { secret } : {}), allowed_channel_names: input.allowedChannelNames,
    max_sessions: input.maxSessions, notify_on_session_created: input.notifyOnSessionCreated,
    allow_bot_messages: input.allowBotMessages, session_config: sessionConfig,
  }
  if (input.editingSlackbot) {
    const update: UpdateSlackBotRequest = {
      ...shared, allowed_user_ids: input.allowedUserIDs,
      ...(!input.showBotTokenSection ? { bot_token_secret_name: '', bot_token_secret_key: '' } : {}),
    }
    await client.updateSlackBot(input.editingSlackbot.id, update)
    return
  }
  const create: CreateSlackBotRequest = {
    ...shared, ...(input.allowedUserIDs.length ? { allowed_user_ids: input.allowedUserIDs } : {}),
    ...(Object.keys(sessionConfig).length ? { session_config: sessionConfig } : {}), ...input.getScopeParams(),
  }
  await client.createSlackBot(create)
}

export function useSlackbotForm({ isOpen, onClose, onSuccess, editingSlackbot }: UseSlackbotFormOptions) {
  const { getScopeParams, selectedTeam } = useTeamScope()

  // Basic fields
  const [name, setName] = useState('')
  const [botToken, setBotToken] = useState('')
  const [appToken, setAppToken] = useState('')
  const [availableSecrets, setAvailableSecrets] = useState<SettingsSecret[]>([])
  const [selectedSecretId, setSelectedSecretId] = useState('')
  const [secretOwner, setSecretOwner] = useState('')
  const [maxSessions, setMaxSessions] = useState<number>(10)
  const [notifyOnSessionCreated, setNotifyOnSessionCreated] = useState<boolean>(true)
  const [allowBotMessages, setAllowBotMessages] = useState<boolean>(false)

  const channels = useChipInput()
  const users = useChipInput()
  const { values: allowedChannelNames, setValues: setAllowedChannelNames, input: channelNameInput, setInput: setChannelNameInput, add: addChannelName, onKeyDown: handleChannelNameKeyDown, remove: removeChannelName } = channels
  const { values: allowedUserIDs, setValues: setAllowedUserIDs, input: userIDInput, setInput: setUserIDInput, add: addUserID, onKeyDown: handleUserIDKeyDown, remove: removeUserID } = users

  // Message templates
  const [initialMessageTemplate, setInitialMessageTemplate] = useState('{{ .event.text }}')
  const [reuseMessageTemplate, setReuseMessageTemplate] = useState('{{ .event.text }}')

  // Session config
  const [envPairs, setEnvPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [tagPairs, setTagPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [repoFullName, setRepoFullName] = useState('')

  const [sessionProfileId, setSessionProfileId] = useState('')

  // UI state
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showBotTokenSection, setShowBotTokenSection] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)

  const isEditing = !!editingSlackbot

  const formSetters = useMemo<SlackbotFormSetters>(() => ({
    setName, setBotToken, setAppToken, setMaxSessions, setNotifyOnSessionCreated, setAllowBotMessages,
    setAllowedChannelNames, setChannelNameInput, setAllowedUserIDs, setUserIDInput,
    setInitialMessageTemplate, setReuseMessageTemplate, setEnvPairs, setTagPairs,
    setShowBotTokenSection, setShowAdvanced, setRepoFullName, setSessionProfileId, setError,
  }), [setAllowedChannelNames, setAllowedUserIDs, setChannelNameInput, setUserIDInput])
  useInitializeSlackbotForm(isOpen, editingSlackbot, formSetters)

  useSlackbotSecrets({ isOpen, selectedTeam, editingSlackbot, setSecretOwner, setAvailableSecrets, setSelectedSecretId })
  useEscapeClose(isOpen, onClose)

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    setError(null)
    if (!name.trim()) { setError('名前は必須です'); return }
    if (!allowedChannelNames.length) { setError('チャンネル名を1つ以上指定してください'); return }
    setIsSubmitting(true)
    try {
      await saveSlackbot({
        name, botToken, appToken, secretOwner, selectedSecretId, allowedChannelNames, allowedUserIDs,
        maxSessions, notifyOnSessionCreated, allowBotMessages, showBotTokenSection,
        initialMessageTemplate, reuseMessageTemplate, envPairs, tagPairs, repoFullName, sessionProfileId,
        editingSlackbot, getScopeParams,
      })
      onSuccess()
    } catch (error) {
      console.error('Failed to save slackbot:', error)
      setError(error instanceof Error ? error.message : 'Slackbotの保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  return {
    name, setName, botToken, setBotToken, appToken, setAppToken,
    availableSecrets, selectedSecretId, setSelectedSecretId,
    maxSessions, setMaxSessions, notifyOnSessionCreated, setNotifyOnSessionCreated,
    allowBotMessages, setAllowBotMessages, allowedChannelNames,
    channelNameInput, setChannelNameInput, allowedUserIDs, userIDInput, setUserIDInput,
    initialMessageTemplate, setInitialMessageTemplate, reuseMessageTemplate, setReuseMessageTemplate,
    envPairs, setEnvPairs, tagPairs, setTagPairs, repoFullName, setRepoFullName,
    sessionProfileId, setSessionProfileId, isSubmitting, error,
    showBotTokenSection, setShowBotTokenSection, showAdvanced, setShowAdvanced, isEditing,
    addChannelName, handleChannelNameKeyDown, removeChannelName,
    addUserID, handleUserIDKeyDown, removeUserID, ...pairActions, handleSubmit,
  }

}

export type SlackbotFormModel = ReturnType<typeof useSlackbotForm>

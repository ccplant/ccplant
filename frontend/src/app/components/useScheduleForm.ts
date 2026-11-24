'use client'

import { useState, useEffect, useCallback, type ChangeEvent, type FormEvent } from 'react'
import { type Schedule, type CreateScheduleRequest, type UpdateScheduleRequest, type ScheduleSessionConfig } from '../../types/schedule'
import type { ResourceScope } from '../../types/agentapi'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { OrganizationHistory } from '../../utils/organizationHistory'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { type KeyValuePair, keyValuePairsToRecord, recordToKeyValuePairs } from './KeyValueInput'

export type ExecutionType = 'recurring' | 'onetime'
interface UseScheduleFormOptions { isOpen: boolean; onClose: () => void; onSuccess: () => void; editingSchedule?: Schedule | null }

function formatDateTimeLocal(isoString: string): string {
  const date = new Date(isoString)
  if (Number.isNaN(date.getTime())) return ''
  const parts = [date.getMonth() + 1, date.getDate(), date.getHours(), date.getMinutes()].map(value => String(value).padStart(2, '0'))
  return `${date.getFullYear()}-${parts[0]}-${parts[1]}T${parts[2]}:${parts[3]}`
}

function useScheduleRepository() {
  const [repository, setRepository] = useState('')
  const [suggestions, setSuggestions] = useState<string[]>([])
  const [showSuggestions, setShowSuggestions] = useState(false)
  const showMatches = useCallback((query?: string) => {
    const matches = OrganizationHistory.getRepositorySuggestions(query)
    setSuggestions(matches)
    setShowSuggestions(matches.length > 0)
  }, [])
  const handleChange = useCallback((event: ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value
    setRepository(value)
    if (value.trim()) showMatches(value); else setShowSuggestions(false)
  }, [showMatches])
  const select = useCallback((value: string) => { setRepository(value); setShowSuggestions(false) }, [])
  const blur = useCallback(() => { setTimeout(() => setShowSuggestions(false), 150) }, [])
  return { repository, setRepository, suggestions, showSuggestions, handleChange, focus: () => showMatches(), blur, select }
}

interface ScheduleSubmission {
  name: string; executionType: ExecutionType; cronExpr: string; scheduledAt: string; timezone: string
  message: string; model: string; repository: string; sessionProfileId: string
  oneshot: boolean; reuseSession: boolean; reuseMessage: string; envVarPairs: KeyValuePair[]
  editingSchedule?: Schedule | null
  getScopeParams: () => { scope: ResourceScope; team_id?: string }
}

export function buildScheduleRequest(input: ScheduleSubmission): CreateScheduleRequest {
  const params: ScheduleSessionConfig['params'] = {
    ...(input.message.trim() ? { message: input.message.trim() } : {}),
    ...(input.model.trim() ? { model: input.model.trim() } : {}),
    ...(input.oneshot ? { oneshot: true } : {}),
  }
  const environment = keyValuePairsToRecord(input.envVarPairs)
  const request: CreateScheduleRequest = {
    name: input.name.trim(),
    timezone: input.timezone,
    session_config: {
      params: Object.keys(params).length ? params : undefined,
      tags: input.repository.trim() ? { repository: input.repository.trim() } : undefined,
      reuse_session: input.reuseSession || undefined,
      reuse_message: input.reuseSession && input.reuseMessage.trim() ? input.reuseMessage.trim() : undefined,
      environment: environment || undefined,
      session_profile_id: input.sessionProfileId.trim() || undefined,
    },
    ...input.getScopeParams(),
  }
  if (input.executionType === 'recurring') request.cron_expr = input.cronExpr.trim()
  else request.scheduled_at = new Date(input.scheduledAt).toISOString()
  return request
}

async function saveSchedule(input: ScheduleSubmission): Promise<void> {
  const client = createAgentAPIProxyClientFromStorage()
  const request = buildScheduleRequest(input)
  if (!input.editingSchedule) { await client.createSchedule(request); return }
  const update: UpdateScheduleRequest = {
    ...request,
    ...(input.editingSchedule.status === 'completed' ? { status: 'active' as const } : {}),
  }
  await client.updateSchedule(input.editingSchedule.id, update)
}

export function useScheduleForm({ isOpen, onClose, onSuccess, editingSchedule }: UseScheduleFormOptions) {
  const { getScopeParams } = useTeamScope()
  const [name, setName] = useState('')
  const [executionType, setExecutionType] = useState<ExecutionType>('recurring')
  const [cronExpr, setCronExpr] = useState('')
  const [scheduledAt, setScheduledAt] = useState('')
  const [timezone, setTimezone] = useState('Asia/Tokyo')
  const [message, setMessage] = useState('')
  const [model, setModel] = useState('')
  const repositoryField = useScheduleRepository()
  const { repository, setRepository, suggestions: repositorySuggestions, showSuggestions: showRepositorySuggestions, handleChange: handleRepositoryChange, focus: handleRepositoryFocus, blur: handleRepositoryBlur, select: selectRepositorySuggestion } = repositoryField

  const [sessionProfileId, setSessionProfileId] = useState('')
  const [oneshot, setOneshot] = useState(false)
  const [reuseSession, setReuseSession] = useState(false)
  const [reuseMessage, setReuseMessage] = useState('')
  const [envVarPairs, setEnvVarPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isEditing = !!editingSchedule
	const resetForm = useCallback(() => {
		setName(''); setExecutionType('recurring'); setCronExpr(''); setScheduledAt('')
		setTimezone('Asia/Tokyo'); setMessage(''); setModel(''); setRepository('')
		setSessionProfileId(''); setOneshot(false); setReuseSession(false); setReuseMessage('')
		setEnvVarPairs([{ key: '', value: '' }]); setError(null)
	}, [setRepository])
	const handleClose = useCallback(() => { resetForm(); onClose() }, [onClose, resetForm])

  // Initialize form when editing
  useEffect(() => {
    if (editingSchedule) {
      setName(editingSchedule.name)
      setTimezone(editingSchedule.timezone || 'Asia/Tokyo')
      setMessage(editingSchedule.session_config?.params?.message || '')
      setModel(editingSchedule.session_config?.params?.model || '')
      setRepository(editingSchedule.session_config?.tags?.repository || '')
      setSessionProfileId(editingSchedule.session_config?.session_profile_id || '')
      setOneshot(editingSchedule.session_config?.params?.oneshot === true)
      setReuseSession(editingSchedule.session_config?.reuse_session === true)
      setReuseMessage(editingSchedule.session_config?.reuse_message || '')
      setEnvVarPairs(recordToKeyValuePairs(editingSchedule.session_config?.environment))

      if (editingSchedule.cron_expr) {
        setExecutionType('recurring')
        setCronExpr(editingSchedule.cron_expr)
        setScheduledAt('')
      } else if (editingSchedule.scheduled_at) {
        setExecutionType('onetime')
        setScheduledAt(formatDateTimeLocal(editingSchedule.scheduled_at))
        setCronExpr('')
      }
    } else {
      resetForm()
    }
	}, [editingSchedule, isOpen, resetForm, setRepository])

  // ESC key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        onClose()
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, onClose])

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    if (!name.trim()) { setError('スケジュール名を入力してください'); return }
    if (executionType === 'recurring' && !cronExpr.trim()) { setError('cron式を入力してください'); return }
    if (executionType === 'onetime' && !scheduledAt) { setError('実行日時を選択してください'); return }
    setIsSubmitting(true); setError(null)
    try {
      await saveSchedule({ name, executionType, cronExpr, scheduledAt, timezone, message, model, repository, sessionProfileId, oneshot, reuseSession, reuseMessage, envVarPairs, editingSchedule, getScopeParams })
      onSuccess(); handleClose()
    } catch (error) {
      console.error('Failed to save schedule:', error)
      setError(error instanceof Error ? error.message : 'スケジュールの保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  return {
    name, setName, executionType, setExecutionType, cronExpr, setCronExpr,
    scheduledAt, setScheduledAt, timezone, setTimezone, message, setMessage, model, setModel,
    repository, repositorySuggestions, showRepositorySuggestions,
    sessionProfileId, setSessionProfileId, oneshot, setOneshot,
    reuseSession, setReuseSession, reuseMessage, setReuseMessage,
    envVarPairs, setEnvVarPairs, isSubmitting, error, isEditing, handleClose,
    handleRepositoryChange, handleRepositoryFocus, handleRepositoryBlur, selectRepositorySuggestion, handleSubmit,
  }
}

export type ScheduleFormModel = ReturnType<typeof useScheduleForm>

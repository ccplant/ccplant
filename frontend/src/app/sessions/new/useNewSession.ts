'use client'
import { useRouter } from 'next/navigation'
import { useEffect, useState } from 'react'
import { useTeamScope } from '../../../contexts/TeamScopeContext'
import { MessageTemplate } from '../../../types/messageTemplate'
import { SessionCreationProgress, SessionCreationStatus } from '../../../types/sessionProgress'
import { submitNewSession } from './newSessionSubmission'
import { useNewSessionOptions } from './useNewSessionOptions'
import { useNewSessionPreferences } from './useNewSessionPreferences'
import { useNewSessionRepository } from './useNewSessionRepository'

export function useNewSession() {
  const { selectedTeam } = useTeamScope()
  const router = useRouter()
  const [initialMessage, setInitialMessage] = useState('')
  const {
    freeFormRepository,
    checkoutTarget,
    checkoutBranch,
    setCheckoutBranch,
    checkoutPrNumber,
    setCheckoutPrNumber,
    previewTags,
    repositoryHistory,
    repositoryList,
    showFreeFormRepositorySuggestions,
    handleFreeFormRepositoryChange,
    handleCheckoutTargetChange,
    handleFreeFormRepositoryFocus,
    handleFreeFormRepositoryBlur,
    selectFreeFormRepositorySuggestion,
  } = useNewSessionRepository()

  const [isCreating, setIsCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [statusMessage, setStatusMessage] = useState('')
  const { templates, recentMessages, availablePools, linkedConnections } = useNewSessionOptions()

  const [showTemplateModal, setShowTemplateModal] = useState(false)

  const [showProgressModal, setShowProgressModal] = useState(false)
  const [creationProgress, setCreationProgress] = useState<SessionCreationProgress | null>(null)
  const [showAdvancedSettings, setShowAdvancedSettings] = useState(false)
  const {
    selectedAgentType,
    setSelectedAgentType,
    selectedManagerId,
    setSelectedManagerId,
    cycleEnabled,
    setCycleEnabled,
    cycleMessage,
    setCycleMessage,
    cycleMaxCount,
    setCycleMaxCount,
    sessionProfileId,
    setSessionProfileId,
    dockerEnabled,
    setDockerEnabled,
    dockerRegistries,
    sessionTTL,
    setSessionTTL,
    connectionId,
    setConnectionId,
    addDockerRegistry,
    removeDockerRegistry,
    updateDockerRegistry,
  } = useNewSessionPreferences()

  // ESCキーで戻る
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (showTemplateModal) {
          setShowTemplateModal(false)
        } else {
          router.back()
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [showTemplateModal, router])

  // 進捗状態を更新するヘルパー関数
  const updateProgress = (status: SessionCreationStatus, errorMessage?: string) => {
    setCreationProgress((prev) => {
      if (!prev) return null
      return {
        ...prev,
        status,
        errorMessage,
      }
    })
  }

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    await submitNewSession({
      draft: {
        initialMessage,
        freeFormRepository,
        checkoutTarget,
        checkoutBranch,
        checkoutPrNumber,
        selectedTeam,
        selectedAgentType,
        selectedManagerId,
        connectionId,
        cycleEnabled,
        cycleMessage,
        cycleMaxCount,
        dockerEnabled,
        dockerRegistries,
        sessionTTL,
        sessionProfileId,
      },
      router,
      setError,
      setIsCreating,
      setStatusMessage,
      setCreationProgress,
      setShowProgressModal,
      updateProgress,
    })
  }

  const handleCloseProgressModal = () => {
    setShowProgressModal(false)
    setCreationProgress(null)
    setIsCreating(false)
  }

  const handleRetry = () => {
    setShowProgressModal(false)
    setCreationProgress(null)
    setIsCreating(false)
  }

  const handleCancel = () => {
    router.back()
  }

  const selectTemplate = (template: MessageTemplate) => {
    setInitialMessage(template.content)
    setShowTemplateModal(false)
  }

  return {
    initialMessage,
    setInitialMessage,
    freeFormRepository,
    checkoutTarget,
    checkoutBranch,
    setCheckoutBranch,
    checkoutPrNumber,
    setCheckoutPrNumber,
    previewTags,
    isCreating,
    error,
    statusMessage,
    templates,
    recentMessages,
    showTemplateModal,
    setShowTemplateModal,
    repositoryHistory,
    repositoryList,
    showFreeFormRepositorySuggestions,
    showProgressModal,
    creationProgress,
    showAdvancedSettings,
    setShowAdvancedSettings,
    selectedAgentType,
    setSelectedAgentType,
    availablePools,
    selectedManagerId,
    setSelectedManagerId,
    cycleEnabled,
    setCycleEnabled,
    cycleMessage,
    setCycleMessage,
    cycleMaxCount,
    setCycleMaxCount,
    sessionProfileId,
    setSessionProfileId,
    dockerEnabled,
    setDockerEnabled,
    dockerRegistries,
    sessionTTL,
    setSessionTTL,
    linkedConnections,
    connectionId,
    setConnectionId,
    addDockerRegistry,
    removeDockerRegistry,
    updateDockerRegistry,
    handleSubmit,
    handleCloseProgressModal,
    handleRetry,
    handleCancel,
    handleFreeFormRepositoryChange,
    handleCheckoutTargetChange,
    handleFreeFormRepositoryFocus,
    handleFreeFormRepositoryBlur,
    selectFreeFormRepositorySuggestion,
    selectTemplate,
  }
}
export type NewSessionFormState = ReturnType<typeof useNewSession>

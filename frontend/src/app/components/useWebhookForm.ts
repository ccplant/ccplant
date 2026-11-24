'use client'

import { createWebhookTriggerActions } from './webhookTriggerActions'

import { useState, useEffect, useCallback } from 'react'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { WebhookSignatureType, WebhookType } from '../../types/webhook'

import { emptyTrigger, type TriggerFormData, type WebhookFormModalProps } from './webhookFormTypes'
import { useWebhookRepositorySuggestions } from './useWebhookRepositorySuggestions'
import { saveWebhook, validateWebhookSubmit, webhookSubmitError } from './webhookFormSubmit'
import { useWebhookFormInitialization } from './useWebhookFormInitialization'

export function useWebhookForm({ isOpen, onClose, onSuccess, editingWebhook }: WebhookFormModalProps) {
  const { getScopeParams } = useTeamScope()
  const [name, setName] = useState('')
  const [webhookType, setWebhookType] = useState<WebhookType>('github')
  const [secret, setSecret] = useState('')
  const [signatureHeader, setSignatureHeader] = useState('X-Signature')
  const [signatureType, setSignatureType] = useState<WebhookSignatureType>('hmac')
  const [signaturePrefix, setSignaturePrefix] = useState('')
  const [allowedEvents, setAllowedEvents] = useState<string[]>([])
  const [triggers, setTriggers] = useState<TriggerFormData[]>([{ ...emptyTrigger }])
  const {
    allowedRepositories, handleRepositoryBlur, handleRepositoryChange,
    handleRepositoryFocus, repositorySuggestions, selectRepositorySuggestion,
    setAllowedRepositories, setRepositorySuggestions, setShowRepositorySuggestions,
    showRepositorySuggestions,
  } = useWebhookRepositorySuggestions()

  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [expandedTrigger, setExpandedTrigger] = useState<number | null>(0)

  const isEditing = !!editingWebhook

  const resetForm = useCallback(() => {
    setName('')
    setWebhookType('github')
    setSecret('')
    setSignatureHeader('X-Signature')
    setSignatureType('hmac')
    setSignaturePrefix('')
    setAllowedEvents([])
    setAllowedRepositories('')
    setTriggers([{ ...emptyTrigger }])
    setError(null)
    setExpandedTrigger(0)
  }, [setAllowedRepositories])

  const handleClose = useCallback(() => {
    resetForm()
    onClose()
  }, [resetForm, onClose])

  useWebhookFormInitialization({
    editingWebhook, isOpen, resetForm, setAllowedEvents, setAllowedRepositories,
    setName, setSecret, setSignatureHeader, setSignaturePrefix, setSignatureType,
    setTriggers, setWebhookType,
  })

  // ESC key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        handleClose()
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, handleClose])

  const toggleAllowedEvent = (event: string) => {
    setAllowedEvents((prev) => (prev.includes(event) ? prev.filter((e) => e !== event) : [...prev, event]))
  }

  const {
    addTrigger,
    removeTrigger,
    updateTrigger,
    updateCredentialSource,
    toggleTriggerEvent,
    toggleTriggerAction,
    getAvailableActions,
    addKeyValuePair,
    updateKeyValuePair,
    removeKeyValuePair,
  } = createWebhookTriggerActions({ triggers, expandedTrigger, webhookType, setTriggers, setExpandedTrigger })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const input = {
      allowedEvents, allowedRepositories, editingWebhook, getScopeParams, name, secret,
      signatureHeader, signaturePrefix, signatureType, triggers, webhookType,
    }
    const validationError = validateWebhookSubmit(input)
    if (validationError) return setError(validationError)

    try {
      setIsSubmitting(true)
      setError(null)
      await saveWebhook(input)
      onSuccess()
      handleClose()
    } catch (err) {
      console.error('Failed to save webhook:', err)
      setError(webhookSubmitError(err))
    } finally {
      setIsSubmitting(false)
    }
  }

  return {
    getScopeParams,
    name,
    setName,
    webhookType,
    setWebhookType,
    secret,
    setSecret,
    signatureHeader,
    setSignatureHeader,
    signatureType,
    setSignatureType,
    signaturePrefix,
    setSignaturePrefix,
    allowedEvents,
    setAllowedEvents,
    allowedRepositories,
    setAllowedRepositories,
    triggers,
    setTriggers,
    repositorySuggestions,
    setRepositorySuggestions,
    showRepositorySuggestions,
    setShowRepositorySuggestions,
    isSubmitting,
    setIsSubmitting,
    error,
    setError,
    expandedTrigger,
    setExpandedTrigger,
    isEditing,
    resetForm,
    handleClose,
    handleRepositoryChange,
    handleRepositoryFocus,
    handleRepositoryBlur,
    selectRepositorySuggestion,
    toggleAllowedEvent,
    addTrigger,
    removeTrigger,
    updateTrigger,
    updateCredentialSource,
    toggleTriggerEvent,
    toggleTriggerAction,
    getAvailableActions,
    addKeyValuePair,
    updateKeyValuePair,
    removeKeyValuePair,
    handleSubmit,
  }
}

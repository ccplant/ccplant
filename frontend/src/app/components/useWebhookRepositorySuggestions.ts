'use client'

import { useState } from 'react'

import { OrganizationHistory } from '../../utils/organizationHistory'

export function useWebhookRepositorySuggestions() {
  const [allowedRepositories, setAllowedRepositories] = useState('')
  const [repositorySuggestions, setRepositorySuggestions] = useState<string[]>([])
  const [showRepositorySuggestions, setShowRepositorySuggestions] = useState(false)

  const handleRepositoryChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value
    setAllowedRepositories(value)
    if (!value.trim()) {
      setShowRepositorySuggestions(false)
      return
    }
    const suggestions = OrganizationHistory.getRepositorySuggestions(value)
    setRepositorySuggestions(suggestions)
    setShowRepositorySuggestions(suggestions.length > 0)
  }

  const handleRepositoryFocus = () => {
    const suggestions = OrganizationHistory.getRepositorySuggestions()
    setRepositorySuggestions(suggestions)
    setShowRepositorySuggestions(suggestions.length > 0)
  }

  const handleRepositoryBlur = () => {
    setTimeout(() => setShowRepositorySuggestions(false), 150)
  }

  const selectRepositorySuggestion = (suggestion: string) => {
    const current = allowedRepositories.trim()
    setAllowedRepositories(current ? `${current}, ${suggestion}` : suggestion)
    setShowRepositorySuggestions(false)
  }

  return {
    allowedRepositories, handleRepositoryBlur, handleRepositoryChange,
    handleRepositoryFocus, repositorySuggestions, selectRepositorySuggestion,
    setAllowedRepositories, setRepositorySuggestions, setShowRepositorySuggestions,
    showRepositorySuggestions,
  }
}

'use client'
import { useMemo, useState } from 'react'
import { OrganizationHistory } from '../../../utils/organizationHistory'
import { buildRepositoryTags, type CheckoutTarget } from './newSessionPresentation'

export function useNewSessionRepository() {
  const [freeFormRepository, setFreeFormRepository] = useState('')
  const [checkoutTarget, setCheckoutTarget] = useState<CheckoutTarget>('')
  const [checkoutBranch, setCheckoutBranch] = useState('')
  const [checkoutPrNumber, setCheckoutPrNumber] = useState('')
  const previewTags = useMemo(
    () => buildRepositoryTags(freeFormRepository, checkoutTarget, checkoutBranch, checkoutPrNumber),
    [freeFormRepository, checkoutTarget, checkoutBranch, checkoutPrNumber]
  )
  const [repositoryHistory, setRepositoryHistory] = useState<string[]>([])
  const [repositoryList, setRepositoryList] = useState<string[]>([])
  const [showFreeFormRepositorySuggestions, setShowFreeFormRepositorySuggestions] = useState(false)
  const handleFreeFormRepositoryChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value
    setFreeFormRepository(value)

    if (value.trim()) {
      const { history, repositories } = OrganizationHistory.getRepositorySuggestionsGrouped(value)
      setRepositoryHistory(history)
      setRepositoryList(repositories)
      setShowFreeFormRepositorySuggestions(history.length > 0 || repositories.length > 0)
    } else {
      setCheckoutTarget('')
      setCheckoutBranch('')
      setCheckoutPrNumber('')
      setShowFreeFormRepositorySuggestions(false)
    }
  }
  const handleCheckoutTargetChange = (target: Exclude<CheckoutTarget, ''>) => {
    setCheckoutTarget(target)
    if (target === 'branch') {
      setCheckoutPrNumber('')
    } else {
      setCheckoutBranch('')
    }
  }
  const handleFreeFormRepositoryFocus = () => {
    const { history, repositories } = OrganizationHistory.getRepositorySuggestionsGrouped()
    setRepositoryHistory(history)
    setRepositoryList(repositories)
    setShowFreeFormRepositorySuggestions(history.length > 0 || repositories.length > 0)
  }
  const handleFreeFormRepositoryBlur = () => {
    setTimeout(() => setShowFreeFormRepositorySuggestions(false), 150)
  }
  const selectFreeFormRepositorySuggestion = (suggestion: string) => {
    setFreeFormRepository(suggestion)
    setShowFreeFormRepositorySuggestions(false)
  }
  return {
    freeFormRepository,
    setFreeFormRepository,
    checkoutTarget,
    setCheckoutTarget,
    checkoutBranch,
    setCheckoutBranch,
    checkoutPrNumber,
    setCheckoutPrNumber,
    previewTags,
    repositoryHistory,
    setRepositoryHistory,
    repositoryList,
    setRepositoryList,
    showFreeFormRepositorySuggestions,
    setShowFreeFormRepositorySuggestions,
    handleFreeFormRepositoryChange,
    handleCheckoutTargetChange,
    handleFreeFormRepositoryFocus,
    handleFreeFormRepositoryBlur,
    selectFreeFormRepositorySuggestion,
  }
}

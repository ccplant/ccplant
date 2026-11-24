'use client'

import { useState } from 'react'
import {} from '../../types/session_profile'

export function useProfileInterfaceState() {
  // UI state
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [resetKey, setResetKey] = useState(0)
  const [drawerOpen, setDrawerOpen] = useState(false)
  return {
    isSubmitting,
    setIsSubmitting,
    error,
    setError,
    dirty,
    setDirty,
    resetKey,
    setResetKey,
    drawerOpen,
    setDrawerOpen,
  }
}

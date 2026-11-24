'use client'

import { useState } from 'react'
import {} from '../../types/session_profile'

export function useProfileBasicState() {
  // Basic fields
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [isDefault, setIsDefault] = useState(false)
  const [repositorySelector, setRepositorySelector] = useState('')
  const [otherSelectorTags, setOtherSelectorTags] = useState<Record<string, string>>({})

  return {
    name,
    setName,
    description,
    setDescription,
    isDefault,
    setIsDefault,
    repositorySelector,
    setRepositorySelector,
    otherSelectorTags,
    setOtherSelectorTags,
  }
}

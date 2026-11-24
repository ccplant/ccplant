'use client'

import { ProfileFile, ProfileRepository } from '../../types/session_profile'

import type { ProfileEditorState } from './profileEditorState'
type KeyValuePair = { key: string; value: string }
export function createProfileDraftActions(state: ProfileEditorState) {
  const { setProfileFiles, setProfileRepositories, setDockerRegistries, setDirty } = state
  const addDockerRegistry = () =>
    setDockerRegistries((prev) => [
      ...prev,
      { server: '', username: '', password: '', secretName: '', insecure: false },
    ])
  const removeDockerRegistry = (index: number) => {
    setDirty(true)
    setDockerRegistries((prev) => prev.filter((_, i) => i !== index))
  }
  const updateDockerRegistry = (index: number, field: string, value: string | boolean) =>
    setDockerRegistries((prev) => prev.map((r, i) => (i === index ? { ...r, [field]: value } : r)))

  // Key-value pair helpers
  const updatePair = (
    pairs: KeyValuePair[],
    setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>,
    index: number,
    field: 'key' | 'value',
    val: string
  ) => {
    const next = [...pairs]
    next[index] = { ...next[index], [field]: val }
    setDirty(true)
    setPairs(next)
  }

  const addPair = (setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>) => {
    setDirty(true)
    setPairs((prev) => [...prev, { key: '', value: '' }])
  }

  const removePair = (
    pairs: KeyValuePair[],
    setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>,
    index: number
  ) => {
    setDirty(true)
    if (pairs.length === 1) {
      setPairs([{ key: '', value: '' }])
    } else {
      setDirty(true)
      setPairs((prev) => prev.filter((_, i) => i !== index))
    }
  }

  const pairsToRecord = (pairs: KeyValuePair[]): Record<string, string> => {
    const record: Record<string, string> = {}
    pairs.forEach(({ key, value }) => {
      if (key.trim()) record[key.trim()] = value
    })
    return record
  }

  const updateProfileFile = (index: number, field: keyof ProfileFile, value: string) => {
    setDirty(true)
    setProfileFiles((prev) => prev.map((file, i) => (i === index ? { ...file, [field]: value } : file)))
  }

  const addProfileFile = () => {
    setDirty(true)
    setProfileFiles((prev) => [...prev, { path: '', content: '', permissions: '0600' }])
  }

  const removeProfileFile = (index: number) => {
    setDirty(true)
    setProfileFiles((prev) => prev.filter((_, i) => i !== index))
  }

  const updateProfileRepository = (index: number, field: keyof ProfileRepository, value: string) => {
    setDirty(true)
    setProfileRepositories((current) =>
      current.map((repository, i) => (i === index ? { ...repository, [field]: value } : repository))
    )
  }

  const addProfileRepository = () => {
    setDirty(true)
    setProfileRepositories((current) => [...current, { repository: '', path: '', branch: '' }])
  }

  const removeProfileRepository = (index: number) => {
    setDirty(true)
    setProfileRepositories((current) => current.filter((_, i) => i !== index))
  }

  return {
    addDockerRegistry,
    removeDockerRegistry,
    updateDockerRegistry,
    updatePair,
    addPair,
    removePair,
    pairsToRecord,
    updateProfileFile,
    addProfileFile,
    removeProfileFile,
    updateProfileRepository,
    addProfileRepository,
    removeProfileRepository,
  }
}

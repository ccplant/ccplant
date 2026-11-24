'use client'

import { useEffect, useState } from 'react'

export interface EnvVarEntry {
  key: string
  value: string
  isNew: boolean
}

export function useEnvVarsSettings(envVarKeys: string[] | undefined, onChange: (envVars: Record<string, string>) => void) {
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingEntry, setEditingEntry] = useState<EnvVarEntry | null>(null)
  const [editKey, setEditKey] = useState('')
  const [editValue, setEditValue] = useState('')
  const [keyError, setKeyError] = useState('')
  const [existingKeys, setExistingKeys] = useState<string[]>(envVarKeys || [])

  useEffect(() => setExistingKeys(envVarKeys || []), [envVarKeys])

  const openEditor = (entry: EnvVarEntry) => {
    setEditingEntry(entry)
    setEditKey(entry.key)
    setEditValue('')
    setKeyError('')
    setIsModalOpen(true)
  }

  const handleDelete = (key: string) => {
    if (!confirm(`環境変数 "${key}" を削除してもよろしいですか？`)) return
    setExistingKeys((keys) => keys.filter((candidate) => candidate !== key))
    onChange({ [key]: '' })
  }

  const validateKey = () => {
    if (!editKey.trim()) return '環境変数名を入力してください'
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(editKey)) {
      return '環境変数名は英字またはアンダースコアで始まり、英数字とアンダースコアのみを含む必要があります'
    }
    if (editingEntry?.isNew && existingKeys.includes(editKey)) return 'この環境変数名は既に存在します'
    return ''
  }

  const handleSave = () => {
    const validationError = validateKey()
    if (validationError) return setKeyError(validationError)
    if (!editValue.trim()) return alert('環境変数の値を入力してください')
    const updates: Record<string, string> = {}
    if (editingEntry && !editingEntry.isNew && editingEntry.key !== editKey) {
      setExistingKeys((keys) => keys.filter((key) => key !== editingEntry.key))
      updates[editingEntry.key] = ''
    }
    if (editingEntry?.isNew) setExistingKeys((keys) => [...keys, editKey].sort())
    updates[editKey] = editValue
    onChange(updates)
    closeEditor()
  }

  const closeEditor = () => {
    setIsModalOpen(false)
    setEditingEntry(null)
    setKeyError('')
  }

  return {
    closeEditor, editKey, editValue, editingEntry, existingKeys, handleDelete,
    handleSave, isModalOpen, keyError, openEditor, setEditKey, setEditValue, setKeyError,
  }
}

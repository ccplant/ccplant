'use client'

import { useCallback, useEffect, useState } from 'react'

import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { CreateFileRequest, UpdateFileRequest, UserFile } from '../../types/user_file'

export type FileEditingState =
  | { kind: 'none' }
  | { kind: 'new' }
  | { kind: 'edit'; file: UserFile }

const emptyForm = (): CreateFileRequest => ({
  name: '',
  path: '',
  content: '',
  permissions: '0600',
})

export function useFileSettings(userName: string) {
  const [files, setFiles] = useState<UserFile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<FileEditingState>({ kind: 'none' })
  const [form, setForm] = useState<CreateFileRequest>(emptyForm())
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())
  const [showContent, setShowContent] = useState<Set<string>>(new Set())

  const fetchFiles = useCallback(async () => {
    if (!userName) return
    setLoading(true)
    setError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const response = await client.listFiles()
      setFiles(response.files || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'ファイルの取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }, [userName])

  useEffect(() => {
    fetchFiles()
  }, [fetchFiles])

  const handleNew = () => {
    setForm(emptyForm())
    setSubmitError(null)
    setEditing({ kind: 'new' })
  }

  const handleEdit = (file: UserFile) => {
    setForm({
      name: file.name,
      path: file.path,
      content: file.content,
      permissions: file.permissions || '0600',
    })
    setSubmitError(null)
    setEditing({ kind: 'edit', file })
  }

  const handleCancel = () => {
    setEditing({ kind: 'none' })
    setSubmitError(null)
  }

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!form.path.trim()) return
    setSubmitting(true)
    setSubmitError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      if (editing.kind === 'new') {
        const created = await client.createFile({
          name: form.name?.trim() || undefined,
          path: form.path.trim(),
          content: form.content,
          permissions: form.permissions?.trim() || undefined,
        })
        setFiles((previous) => [...previous, created])
      } else if (editing.kind === 'edit') {
        const update: UpdateFileRequest = {
          name: form.name?.trim(),
          path: form.path.trim(),
          content: form.content,
          permissions: form.permissions?.trim(),
        }
        const updated = await client.updateFile(editing.file.id, update)
        setFiles((previous) => previous.map((file) => (file.id === updated.id ? updated : file)))
      }
      setEditing({ kind: 'none' })
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : '保存に失敗しました')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = async (file: UserFile) => {
    if (!confirm(`「${file.name || file.path}」を削除しますか？\nセッション起動時にこのファイルは配置されなくなります。`)) return
    setDeletingIds((previous) => new Set(previous).add(file.id))
    try {
      await createAgentAPIProxyClientFromStorage().deleteFile(file.id)
      setFiles((previous) => previous.filter((candidate) => candidate.id !== file.id))
    } catch (err) {
      alert(err instanceof Error ? err.message : '削除に失敗しました')
    } finally {
      setDeletingIds((previous) => {
        const next = new Set(previous)
        next.delete(file.id)
        return next
      })
    }
  }

  const toggleContent = (id: string) => {
    setShowContent((previous) => {
      const next = new Set(previous)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return {
    deletingIds, editing, error, fetchFiles, files, form, handleCancel, handleDelete,
    handleEdit, handleNew, handleSubmit, loading, setForm, showContent, submitError,
    submitting, toggleContent,
  }
}

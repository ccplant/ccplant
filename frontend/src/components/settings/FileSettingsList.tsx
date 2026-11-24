'use client'

import { CreateFileRequest, UserFile } from '../../types/user_file'
import { FileSettingsRow } from './FileSettingsRow'
import { FileEditingState } from './useFileSettings'

interface FileSettingsListProps {
  deletingIds: Set<string>
  editing: FileEditingState
  error: string | null
  files: UserFile[]
  form: CreateFileRequest
  loading: boolean
  onCancel: () => void
  onChange: (form: CreateFileRequest) => void
  onDelete: (file: UserFile) => void
  onEdit: (file: UserFile) => void
  onSubmit: (event: React.FormEvent) => void
  onToggleContent: (id: string) => void
  showContent: Set<string>
  submitting: boolean
}

export function FileSettingsList(props: FileSettingsListProps) {
  const {
    deletingIds, editing, error, files, form, loading, onCancel, onChange,
    onDelete, onEdit, onSubmit, onToggleContent, showContent, submitting,
  } = props
  const isFormOpen = editing.kind !== 'none'

  return (
    <>
      {!loading && files.length > 0 && (
        <div className="space-y-2">
          {files.map((file) => (
            <FileSettingsRow
              key={file.id}
              contentVisible={showContent.has(file.id)}
              error={error}
              file={file}
              form={form}
              isDeleting={deletingIds.has(file.id)}
              isEditing={editing.kind === 'edit' && editing.file.id === file.id}
              isFormOpen={isFormOpen}
              onCancel={onCancel}
              onChange={onChange}
              onDelete={onDelete}
              onEdit={onEdit}
              onSubmit={onSubmit}
              onToggleContent={onToggleContent}
              submitting={submitting}
            />
          ))}
        </div>
      )}
    </>
  )
}

'use client'

import type { ChangeEvent, FormEvent } from 'react'

export interface EnvironmentVariable {
  key: string
  value: string
}

interface RepositoryFieldProps {
  repository: string
  suggestions: string[]
  showSuggestions: boolean
  disabled: boolean
  onChange: (event: ChangeEvent<HTMLInputElement>) => void
  onFocus: () => void
  onBlur: () => void
  onSelect: (repository: string) => void
}

function RepositoryField(props: RepositoryFieldProps) {
  return (
    <div className="relative">
      <label htmlFor="repository" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">Repository</label>
      <input
        type="text"
        id="repository"
        value={props.repository}
        onChange={props.onChange}
        onFocus={props.onFocus}
        onBlur={props.onBlur}
        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
        placeholder="e.g., my-org/my-repo"
        disabled={props.disabled}
      />
      {props.showSuggestions && props.suggestions.length > 0 && (
        <div className="absolute z-50 w-full mt-1 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-md shadow-lg max-h-40 overflow-y-auto">
          {props.suggestions.map(suggestion => (
            <button key={suggestion} type="button" onClick={() => props.onSelect(suggestion)} className="w-full text-left px-3 py-2 hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-900 dark:text-white first:rounded-t-md last:rounded-b-md">
              {suggestion}
            </button>
          ))}
        </div>
      )}
      <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">Specify a repository context for this conversation session.</p>
    </div>
  )
}

interface VariableEditorProps {
  title: string
  keyPlaceholder: string
  valuePlaceholder: string
  help: string
  addLabel: string
  values: EnvironmentVariable[]
  disabled: boolean
  onAdd: () => void
  onRemove: (index: number) => void
  onUpdate: (index: number, field: 'key' | 'value', value: string) => void
}

function VariableEditor(props: VariableEditorProps) {
  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">{props.title}</label>
        <button type="button" onClick={props.onAdd} className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 font-medium" disabled={props.disabled}>{props.addLabel}</button>
      </div>
      <div className="space-y-3">
        {props.values.map((entry, index) => (
          <div key={index} className="flex flex-col sm:flex-row gap-3 items-start sm:items-center">
            <input type="text" value={entry.key} onChange={event => props.onUpdate(index, 'key', event.target.value)} className="w-full sm:flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white" placeholder={props.keyPlaceholder} disabled={props.disabled} />
            <input type="text" value={entry.value} onChange={event => props.onUpdate(index, 'value', event.target.value)} className="w-full sm:flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white" placeholder={props.valuePlaceholder} disabled={props.disabled} />
            {props.values.length > 1 && <RemoveButton disabled={props.disabled} onClick={() => props.onRemove(index)} />}
          </div>
        ))}
      </div>
      <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">{props.help}</p>
    </div>
  )
}

function RemoveButton({ disabled, onClick }: { disabled: boolean; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="text-red-600 dark:text-red-400 hover:text-red-800 dark:hover:text-red-300 self-start sm:self-center" disabled={disabled} aria-label="Remove variable">
      <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
    </button>
  )
}

interface BasicFieldsProps extends RepositoryFieldProps {
  userId: string
  description: string
  oneshot: boolean
  onUserIdChange: (value: string) => void
  onDescriptionChange: (value: string) => void
  onOneshotChange: (value: boolean) => void
}

function BasicFields(props: BasicFieldsProps) {
  return (
    <>
      <div>
        <label htmlFor="userId" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">User ID *</label>
        <input type="text" id="userId" value={props.userId} onChange={event => props.onUserIdChange(event.target.value)} className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white" placeholder="Enter your user ID" disabled={props.disabled} />
      </div>
      <RepositoryField {...props} />
      <div>
        <label htmlFor="description" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">Description *</label>
        <textarea id="description" value={props.description} onChange={event => props.onDescriptionChange(event.target.value)} rows={3} className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white" placeholder="Describe what this session is about" disabled={props.disabled} />
      </div>
      <div className="flex items-start gap-3">
        <input type="checkbox" id="oneshot" checked={props.oneshot} onChange={event => props.onOneshotChange(event.target.checked)} className="mt-1 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500" disabled={props.disabled} />
        <div><label htmlFor="oneshot" className="block text-sm font-medium text-gray-700 dark:text-gray-300 cursor-pointer">エージェントが終了したら自動的に削除する</label><p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">セッションが停止した後、自動的にセッションを削除します。</p></div>
      </div>
    </>
  )
}

function ModalHeader({ onClose }: { onClose: () => void }) {
  return (
    <div className="p-6 border-b border-gray-200 dark:border-gray-700"><div className="flex items-center justify-between">
      <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Start New Session</h2>
      <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" /></svg></button>
    </div></div>
  )
}

function FormActions({ creating, valid, onClose }: { creating: boolean; valid: boolean; onClose: () => void }) {
  return (
    <div className="flex justify-end gap-3 pt-4 border-t border-gray-200 dark:border-gray-700">
      <button type="button" onClick={onClose} className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md hover:bg-gray-50 dark:hover:bg-gray-600 disabled:opacity-50" disabled={creating}>Cancel</button>
      <button type="submit" className="px-4 py-2 text-sm font-medium text-white bg-blue-600 border border-transparent rounded-md hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed" disabled={creating || !valid}>{creating ? 'Creating...' : 'Start Session'}</button>
    </div>
  )
}

export interface NewConversationModalViewProps extends BasicFieldsProps {
  envVars: EnvironmentVariable[]
  metadataVars: EnvironmentVariable[]
  error: string | null
  onSubmit: (event: FormEvent) => void
  onClose: () => void
  onAddEnv: () => void
  onRemoveEnv: (index: number) => void
  onUpdateEnv: (index: number, field: 'key' | 'value', value: string) => void
  onAddMetadata: () => void
  onRemoveMetadata: (index: number) => void
  onUpdateMetadata: (index: number, field: 'key' | 'value', value: string) => void
}

export function NewConversationModalView(props: NewConversationModalViewProps) {
  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center p-4 z-50" onClick={event => { if (event.target === event.currentTarget) props.onClose() }}>
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-2xl w-full max-h-[90vh] overflow-y-auto">
        <ModalHeader onClose={props.onClose} />
        <form onSubmit={props.onSubmit} className="p-6 space-y-6">
          <BasicFields {...props} />
          <VariableEditor title="Environment Variables (Optional)" addLabel="+ Add Variable" keyPlaceholder="Variable name (e.g., NODE_ENV)" valuePlaceholder="Variable value" help="Environment variables will be available as context during the session." values={props.envVars} disabled={props.disabled} onAdd={props.onAddEnv} onRemove={props.onRemoveEnv} onUpdate={props.onUpdateEnv} />
          <VariableEditor title="Metadata (Optional)" addLabel="+ Add Metadata" keyPlaceholder="Metadata key (e.g., project_type)" valuePlaceholder="Metadata value" help="Metadata fields for categorizing and filtering sessions." values={props.metadataVars} disabled={props.disabled} onAdd={props.onAddMetadata} onRemove={props.onRemoveMetadata} onUpdate={props.onUpdateMetadata} />
          {props.error && <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-3"><span className="text-red-600 dark:text-red-400 text-sm">{props.error}</span></div>}
          <FormActions creating={props.disabled} valid={Boolean(props.description.trim())} onClose={props.onClose} />
        </form>
      </div>
    </div>
  )
}

'use client'

import { ApiTokenScope, CreateApiTokenResponse } from '../../types/api_token'
import { ApiTokenCreateFields, ApiTokenCreateFooter } from './ApiTokenCreateFields'
import { useApiTokenCreate } from './useApiTokenCreate'

interface Props { isOpen: boolean; scope: ApiTokenScope; teamId?: string; onClose: () => void; onCreated: (response: CreateApiTokenResponse) => void }

export default function ApiTokenCreateModal(props: Props) {
  const state = useApiTokenCreate(props)
  if (!props.isOpen) return null
  return <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"><div className="bg-white dark:bg-gray-800 rounded-lg w-full max-w-lg shadow-xl overflow-hidden">
    <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700"><h2 className="text-lg font-semibold text-gray-900 dark:text-white">{props.scope === 'team' ? 'チーム API トークンを作成' : 'API トークンを作成'}</h2><button type="button" onClick={props.onClose} disabled={state.isSubmitting} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 disabled:opacity-50" aria-label="閉じる">×</button></div>
    <form id="api-token-create-form" onSubmit={state.submit} className="px-6 py-4 space-y-4 max-h-[75vh] overflow-y-auto"><ApiTokenCreateFields state={state} scope={props.scope} teamId={props.teamId} /></form>
    <ApiTokenCreateFooter state={state} onClose={props.onClose} />
  </div></div>
}

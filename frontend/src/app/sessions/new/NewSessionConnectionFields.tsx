'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionConnectionFields(context: NewSessionFormState) {
  const { isCreating, linkedConnections, connectionId, setConnectionId } = context
  return (
    <div>
      <label htmlFor="connection-id" className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">
        アカウント接続
      </label>
      <select
        id="connection-id"
        value={connectionId}
        onChange={(event) => setConnectionId(event.target.value)}
        disabled={isCreating}
        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm"
      >
        <option value="">自動選択</option>
        {linkedConnections.map((identity) => (
          <option key={identity.id} value={identity.connection_id}>
            {identity.connection_name} ({identity.login})
          </option>
        ))}
      </select>
      <p className="text-xs text-gray-400 dark:text-gray-500 mt-1.5">
        通常はrepository organizationに応じて自動選択されます
      </p>
    </div>
  )
}

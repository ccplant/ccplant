'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'
import { MCPServerSettings } from '../../components/settings/MCPServerSettings'

type Props = Pick<ReturnType<typeof useSessionProfileEditor>, 'mcpServers' | 'setMcpServers' | 'setDirty'> &
  Pick<SessionProfileEditorProps, never>

export function ProfileMcpSection({ mcpServers, setMcpServers, setDirty }: Props) {
  return (
    <div className="space-y-5">
      {/* MCP Servers */}
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">MCP サーバー</label>
        <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
          個人・チーム設定の MCP サーバーを継承します。同じ名前のサーバーは、このプロファイルの設定で上書きされます。
        </p>
        <MCPServerSettings
          servers={mcpServers}
          onChange={(value) => {
            setMcpServers(value)
            setDirty(true)
          }}
        />
      </div>
    </div>
  )
}

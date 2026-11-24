'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  'sourceProfileId' | 'systemPrompt' | 'setSystemPrompt' | 'systemPromptMode' | 'setSystemPromptMode'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileSystemPromptSection({
  sourceProfileId,
  systemPrompt,
  setSystemPrompt,
  systemPromptMode,
  setSystemPromptMode,
}: Props) {
  return (
    <div className="space-y-5">
      <div>
        <label
          htmlFor="session-profile-system-prompt"
          className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
        >
          エージェントへの指示
        </label>
        <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
          セッション起動時に、選択されたエージェントが読み込む指示ファイルとして配置します。Claude では
          CLAUDE.md、それ以外の対応エージェントでは AGENTS.md が使用されます。
        </p>
        <textarea
          id="session-profile-system-prompt"
          value={systemPrompt}
          onChange={(event) => setSystemPrompt(event.target.value)}
          rows={18}
          placeholder={'# Instructions\n\nこのセッションでエージェントが従う指示を入力してください。'}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <label
          htmlFor="session-profile-system-prompt-mode"
          className="mt-4 block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
        >
          ベースプロファイルの指示
        </label>
        <select
          id="session-profile-system-prompt-mode"
          value={systemPromptMode}
          onChange={(event) => setSystemPromptMode(event.target.value as 'override' | 'append')}
          disabled={!sourceProfileId}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white disabled:opacity-60 focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="override">上書きする</option>
          <option value="append">後ろに追記する</option>
        </select>
        <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {sourceProfileId
            ? '追記では、ベースプロファイルの指示の後ろにこの内容を追加します。'
            : '空欄の場合は、エージェントイメージに含まれる既定の指示を使用します。'}
        </p>
      </div>
    </div>
  )
}

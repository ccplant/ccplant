'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  'commandWrapperTemplate' | 'setCommandWrapperTemplate' | 'setDirty'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileRuntimeSection({ commandWrapperTemplate, setCommandWrapperTemplate, setDirty }: Props) {
  return (
    <div className="space-y-5">
      <div>
        <div className="mb-1 flex items-center justify-between gap-2">
          <label
            htmlFor="session-profile-command-wrapper"
            className="block text-sm font-medium text-gray-700 dark:text-gray-300"
          >
            コマンドラッパー
          </label>
          <button
            type="button"
            onClick={() => {
              setCommandWrapperTemplate(
                (value) => `${value}${value && !value.endsWith('\n') ? '\n' : ''}exec {{ .Command }}`
              )
              setDirty(true)
            }}
            className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400"
          >
            {'{{ .Command }}'} を挿入
          </button>
        </div>
        <textarea
          id="session-profile-command-wrapper"
          value={commandWrapperTemplate}
          onChange={(e) => {
            setCommandWrapperTemplate(e.target.value)
            setDirty(true)
          }}
          placeholder={'#!/bin/sh\nset -eu\nexec env PROFILE=development {{ .Command }}'}
          rows={7}
          className="w-full rounded-md border border-gray-300 bg-white p-2 font-mono text-xs text-gray-900 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-white"
        />
        <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
          Go template として保存され、shell quote 済みの既定コマンドを <code>{'{{ .Command }}'}</code>{' '}
          に1回挿入します。signal を正しく転送するため <code>exec</code> の使用を推奨します。
        </p>
      </div>
    </div>
  )
}
